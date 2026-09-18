package azureblob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/standards-lab/go-storage"
)

// Put uploads body as the block blob at key through the SDK's streaming
// upload, which reads body to EOF and needs neither its length nor a seek.
// A body shorter than one block is sent as one Put Blob request; a longer
// one is staged in blocks of the configured block_size by up to concurrency
// workers and committed with one block list, so every block buffer is
// released before Put returns. Either request commits the blob whole, so
// the object is replaced in one step or not at all. opts.ContentType is sent
// as the blob's Content-Type header when set; a replace without it leaves
// the service's default, application/octet-stream.
//
// When opts.Size is greater than 0 the body must yield exactly that many
// bytes. A body that ends short of Size fails with an error wrapping
// io.ErrUnexpectedEOF, and one that runs past Size fails on the first byte
// beyond it; both are read failures and take the path described below, so
// the blob is never truncated to Size and never holds a body longer than
// it. A Size of 0 asserts nothing.
//
// The returned Object carries the ETag and LastModified the service answered
// with, the number of bytes read from body, and opts.ContentType as given.
//
// A failure of body itself is not a store failure. When body returns an error
// other than io.EOF, Put returns that error wrapped, unclassified, so a
// caller's sentinel in it (storage.Store's size bound is one) stays matchable
// and it never matches storage.ErrUnavailable. No object is committed then,
// because the SDK sends the Put Blob or the Put Block List only after the
// body has ended: a body that fit in one block was never sent, and the
// staged blocks of a longer one are left uncommitted, invisible to Get,
// Stat, and List, for the service to discard.
func (c *Client) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	tracked := &trackedReader{r: body, size: opts.Size}
	uploadOpts := &blockblob.UploadStreamOptions{
		BlockSize:   c.blockSize,
		Concurrency: c.concurrency,
	}
	if opts.ContentType != "" {
		uploadOpts.HTTPHeaders = &blob.HTTPHeaders{BlobContentType: &opts.ContentType}
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
		ContentType: opts.ContentType,
		ETag:        entityTag(resp.ETag),
		ModifiedAt:  timeValue(resp.LastModified),
	}, nil
}

// Get opens the blob at key with one Get Blob request and returns its
// metadata from the response headers with the response body as the stream.
// opts is empty by definition and ignored. A missing blob matches
// storage.ErrNotFound.
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
			Size:        int64Value(resp.ContentLength),
			ContentType: stringValue(resp.ContentType),
			ETag:        entityTag(resp.ETag),
			ModifiedAt:  timeValue(resp.LastModified),
		},
		Body: resp.Body,
	}, nil
}

// Stat reads the blob's properties with one Get Blob Properties request. A
// missing blob matches storage.ErrNotFound.
func (c *Client) Stat(ctx context.Context, key string) (storage.Object, error) {
	resp, err := c.container.NewBlobClient(key).GetProperties(ctx, nil)
	if err != nil {
		return storage.Object{}, classify(err)
	}
	return storage.Object{
		Key:         key,
		Size:        int64Value(resp.ContentLength),
		ContentType: stringValue(resp.ContentType),
		ETag:        entityTag(resp.ETag),
		ModifiedAt:  timeValue(resp.LastModified),
	}, nil
}

// Delete removes the blob at key. The service's BlobNotFound answer is the
// idempotent success the Client contract asks for. A missing container is not
// swallowed: it still matches storage.ErrNotFound, because it says the
// configured target is gone rather than that this key is.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.container.NewBlobClient(key).Delete(ctx, nil)
	if bloberror.HasCode(err, bloberror.BlobNotFound) {
		return nil
	}
	return classify(err)
}

// List fetches one page of the container's flat blob listing. opts.Prefix,
// opts.Token, and opts.Limit map to the request's prefix, marker, and
// maxresults; a Limit of 0 leaves maxresults unset so the service applies its
// own page size, and a Limit past the int32 range is clamped. The service's
// NextMarker is returned as Page.Next, verbatim and never inspected: Azure
// returns an opaque token and Azurite returns the last key of the page, and
// both are passed back on the next call as given.
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

	page := storage.Page{Next: stringValue(resp.NextMarker)}
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
			obj.Size = int64Value(p.ContentLength)
			obj.ContentType = stringValue(p.ContentType)
			obj.ETag = entityTag(p.ETag)
			obj.ModifiedAt = timeValue(p.LastModified)
		}
		page.Objects = append(page.Objects, obj)
	}
	return page, nil
}

// trackedReader counts the bytes the SDK reads from a Put body, holds the
// body to its declared size when one was given, and keeps the first error
// the body returned other than io.EOF, so Put can tell a failure of the body
// from a failure of the upload. Once an error is recorded every later Read
// returns it again, because the SDK drops an error that arrives with the
// bytes that complete a block and reads once more. The SDK reads on the
// calling goroutine, but the fields are atomic so the count and the error
// are safe to read after the upload returns whatever goroutine read them.
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
// when the response carried none. Azure quotes an ETag in a response header
// and leaves it unquoted in a listing's XML, so an unquoted value gains its
// quotes here and an already quoted or W/"..." value is returned as it is.
// Every path that reports an ETag goes through this function, so one version
// of a blob reports one string whichever call produced it.
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

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func int64Value(n *int64) int64 {
	if n == nil {
		return 0
	}
	return *n
}

func timeValue(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
