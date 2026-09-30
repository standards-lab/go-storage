package azureblob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync/atomic"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/standards-lab/go-storage"
)

// defaultContentType is what Put sends and reports when opts.ContentType is
// empty, the type the service stores for a blob written without one.
const defaultContentType = "application/octet-stream"

// Put uploads body through the SDK's streaming upload, which sends the Put
// Blob or the Put Block List only after the body ends. A failure of body, a
// Size mismatch included, is returned wrapped and unclassified.
func (c *Client) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	tracked := &trackedReader{r: body, size: opts.Size}
	contentType := opts.ContentType
	if contentType == "" {
		contentType = defaultContentType
	}
	uploadOpts := &blockblob.UploadStreamOptions{
		BlockSize:   c.blockSize,
		Concurrency: c.concurrency,
		HTTPHeaders: &blob.HTTPHeaders{BlobContentType: &contentType},
	}

	resp, err := c.container.NewBlockBlobClient(key).UploadStream(ctx, tracked, uploadOpts)
	if err != nil {
		if readErr := tracked.readError(); readErr != nil {
			return storage.Object{}, fmt.Errorf("azureblob: read body: %w", readErr)
		}
		return storage.Object{}, classify(err)
	}
	return storage.Object{
		Key:         key,
		Size:        tracked.n.Load(),
		ContentType: contentType,
		ETag:        entityTag(resp.ETag),
		ModifiedAt:  deref(resp.LastModified),
	}, nil
}

// Get opens the blob at key with one Get Blob request and returns the
// response body as the stream.
func (c *Client) Get(ctx context.Context, key string, _ storage.GetOptions) (storage.Blob, error) {
	resp, err := c.container.NewBlobClient(key).DownloadStream(ctx, nil)
	if err != nil {
		return storage.Blob{}, classify(err)
	}
	if resp.Body == nil {
		// The SDK returns a body-less response only for a 304, which needs an
		// access condition this method never sends; guard it all the same.
		return storage.Blob{}, errors.New("azureblob: get blob returned no body")
	}
	return storage.Blob{
		Object: storage.Object{
			Key:         key,
			Size:        deref(resp.ContentLength),
			ContentType: deref(resp.ContentType),
			ETag:        entityTag(resp.ETag),
			ModifiedAt:  deref(resp.LastModified),
		},
		// A try's deadline covers the part of the body read within it: when
		// a read fails, the body resumes from its offset with a ranged GET
		// conditioned on the ETag, so a transfer outlasts try_timeout while
		// a try that stalls is retried, readRetries times per read.
		Body: resp.NewRetryReader(ctx, &blob.RetryReaderOptions{MaxRetries: c.readRetries}),
	}, nil
}

// Stat reads the blob's properties with one Get Blob Properties request.
func (c *Client) Stat(ctx context.Context, key string) (storage.Object, error) {
	resp, err := c.container.NewBlobClient(key).GetProperties(ctx, nil)
	if err != nil {
		return storage.Object{}, classify(err)
	}
	return storage.Object{
		Key:         key,
		Size:        deref(resp.ContentLength),
		ContentType: deref(resp.ContentType),
		ETag:        entityTag(resp.ETag),
		ModifiedAt:  deref(resp.LastModified),
	}, nil
}

// Delete removes the blob at key and treats the service's BlobNotFound
// answer as success.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.container.NewBlobClient(key).Delete(ctx, nil)
	if bloberror.HasCode(err, bloberror.BlobNotFound) {
		return nil
	}
	return classify(err)
}

// List fetches one page of the container's flat blob listing. opts.Prefix,
// opts.Token, and opts.Limit map to the request's prefix, marker, and
// maxresults; a Limit past the int32 range is clamped.
func (c *Client) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	listOpts := &container.ListBlobsFlatOptions{}
	if opts.Prefix != "" {
		listOpts.Prefix = &opts.Prefix
	}
	if opts.Token != "" {
		listOpts.Marker = &opts.Token
	}
	if opts.Limit > 0 {
		limit := int32(min(opts.Limit, math.MaxInt32))
		listOpts.MaxResults = &limit
	}

	resp, err := c.container.NewListBlobsFlatPager(listOpts).NextPage(ctx)
	if err != nil {
		return storage.Page{}, classify(err)
	}

	page := storage.Page{Next: deref(resp.NextMarker)}
	if resp.Segment == nil {
		return page, nil
	}
	page.Objects = make([]storage.Object, 0, len(resp.Segment.BlobItems))
	for _, item := range resp.Segment.BlobItems {
		if item == nil || item.Name == nil {
			continue
		}
		obj := storage.Object{Key: *item.Name}
		if p := item.Properties; p != nil {
			obj.Size = deref(p.ContentLength)
			obj.ContentType = deref(p.ContentType)
			obj.ETag = entityTag(p.ETag)
			obj.ModifiedAt = deref(p.LastModified)
		}
		page.Objects = append(page.Objects, obj)
	}
	return page, nil
}

// trackedReader counts the bytes read from a Put body, holds the body to its
// declared size, and records the body's first error other than io.EOF, so Put
// can tell a failure of the body from one of the upload. A recorded error is
// repeated on every later Read, because the SDK drops an error that arrives
// with the bytes completing a block.
type trackedReader struct {
	r    io.Reader
	size int64 // the declared length, or 0 when unknown
	n    atomic.Int64
	err  atomic.Pointer[error]
}

func (t *trackedReader) Read(p []byte) (int, error) {
	if err := t.readError(); err != nil {
		return 0, err
	}
	if t.size > 0 {
		remain := t.size - t.n.Load()
		if remain <= 0 {
			// The declared size is spent. One more byte from the body means
			// it is longer than declared; none means it ended on it.
			var probe [1]byte
			n, err := t.r.Read(probe[:])
			if n > 0 {
				return 0, t.fail(fmt.Errorf("body is longer than the declared size (%d bytes)", t.size))
			}
			if err != nil && !errors.Is(err, io.EOF) {
				return 0, t.fail(err)
			}
			return 0, err
		}
		if int64(len(p)) > remain {
			p = p[:remain]
		}
	}
	n, err := t.r.Read(p)
	read := t.n.Add(int64(n))
	switch {
	case err == nil:
		return n, nil
	case !errors.Is(err, io.EOF):
		return n, t.fail(err)
	case t.size > 0 && read < t.size:
		return n, t.fail(fmt.Errorf("body ended after %d bytes, short of the declared size (%d bytes): %w", read, t.size, io.ErrUnexpectedEOF))
	default:
		return n, err
	}
}

// fail records err as the body's failure unless one is already recorded,
// and returns the recorded one.
func (t *trackedReader) fail(err error) error {
	t.err.CompareAndSwap(nil, &err)
	return t.readError()
}

// readError returns the first non-EOF error the body returned, or nil.
func (t *trackedReader) readError() error {
	if p := t.err.Load(); p != nil {
		return *p
	}
	return nil
}

// entityTag returns the ETag the service sent in HTTP entity-tag form, or ""
// when the response carried none: an unquoted listing value gains its quotes,
// and a quoted or W/"..." value is returned as it is.
func entityTag(e *azcore.ETag) string {
	if e == nil {
		return ""
	}
	s := string(*e)
	if s == "" || strings.HasPrefix(s, `"`) || strings.HasPrefix(s, `W/"`) {
		return s
	}
	return `"` + s + `"`
}

// deref returns *p, or T's zero value when p is nil.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
