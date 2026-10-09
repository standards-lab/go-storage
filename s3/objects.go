package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	smithymiddleware "github.com/aws/smithy-go/middleware"

	"github.com/standards-lab/go-storage"
)

// defaultContentType is what Put sends and reports when opts.ContentType is
// empty, the type S3 stores for an object written without one.
const defaultContentType = "application/octet-stream"

// Put sends a body of at most one part as a single PutObject and a longer
// one as a multipart upload through transfermanager, so the object appears
// whole or not at all. It reads the body through a part and one byte more
// into memory to decide. A body that fits is sent from that buffer, which
// is seekable, as the SDK needs to sign a request over plain HTTP, with its
// length and content type: application/octet-stream when opts gives none.
// A longer body, of declared or unknown size, streams on: transfermanager
// uploads it a part at a time, holding a few parts in memory, never the
// whole body.
//
// A failure of body, or a body shorter or longer than a declared Size,
// fails Put. Before the decision it has sent nothing. During a multipart
// upload, a failure of body, a part, the completion, or ctx aborts the
// upload: transfermanager aborts on a context of its own, and Put sends one
// more AbortMultipartUpload for the same upload ID, because transfermanager
// drops the error of an abort that fails after another failure, and S3
// advises a repeat abort to free parts still in flight. An abort that still
// fails is named in Put's error, since the upload's parts may then remain
// until a lifecycle rule removes them. A failure of body is returned
// wrapped and unclassified; any other failure is classified.
//
// Neither PutObject's answer nor CompleteMultipartUpload's carries a
// Last-Modified, so a HeadObject follows either for the ModifiedAt the
// other operations report. When that HeadObject fails, or reports another
// ETag because a concurrent writer replaced the object, the object is still
// written: Put succeeds and takes ModifiedAt from the write answer's Date
// header instead.
func (c *Client) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if err := validateKey(key); err != nil {
		return storage.Object{}, err
	}
	contentType := opts.ContentType
	if contentType == "" {
		contentType = defaultContentType
	}

	src := &bodyReader{r: body, size: opts.Size}
	var buf bytes.Buffer
	if opts.Size > 0 {
		buf.Grow(int(min(opts.Size, c.partSize+1)))
	}
	// One byte past a part shows whether the body needs more than one.
	n, err := buf.ReadFrom(io.LimitReader(src, c.partSize+1))
	if err != nil {
		return storage.Object{}, fmt.Errorf("s3: read body: %w", err)
	}

	obj := storage.Object{Key: key, ContentType: contentType}
	var meta smithymiddleware.Metadata
	if n <= c.partSize {
		out, err := c.s3.PutObject(ctx, &awss3.PutObjectInput{
			Bucket:        aws.String(c.bucket),
			Key:           aws.String(key),
			Body:          bytes.NewReader(buf.Bytes()),
			ContentLength: aws.Int64(n),
			ContentType:   aws.String(contentType),
		})
		if err != nil {
			return storage.Object{}, classify(err)
		}
		obj.ETag, meta = entityTag(out.ETag), out.ResultMetadata
	} else {
		in := &transfermanager.UploadObjectInput{
			Bucket:      aws.String(c.bucket),
			Key:         aws.String(key),
			Body:        io.MultiReader(&buf, src),
			ContentType: aws.String(contentType),
		}
		if opts.Size > 0 {
			in.ContentLength = aws.Int64(opts.Size)
		}
		out, err := c.uploader.UploadObject(ctx, in)
		if err != nil {
			return storage.Object{}, c.failedUpload(ctx, key, src, err)
		}
		obj.ETag, meta = entityTag(out.ETag), out.ResultMetadata
	}
	obj.Size = src.n

	head, headErr := c.s3.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if headErr == nil && entityTag(head.ETag) == obj.ETag {
		obj.ModifiedAt = aws.ToTime(head.LastModified)
	} else if date, ok := awsmiddleware.GetServerTime(meta); ok {
		obj.ModifiedAt = date
	}
	return obj, nil
}

// failedUpload returns the error Put reports for a failed multipart upload,
// after it repeats the abort of the upload transfermanager started. The
// body's own failure is returned unclassified, as it is before any request;
// any other is classified. A repeat abort that fails is named in the error
// but never classifies it, since the upload's failure is what Put reports.
func (c *Client) failedUpload(ctx context.Context, key string, src *bodyReader, err error) error {
	var abortErr error
	if mpErr, ok := errors.AsType[transfermanager.MultipartUploadError](err); ok && mpErr.UploadID() != "" {
		abortErr = c.abortUpload(ctx, key, mpErr.UploadID())
	}
	if src.err != nil {
		err = fmt.Errorf("s3: read body: %w", src.err)
	} else {
		err = classify(err)
	}
	if abortErr != nil {
		return fmt.Errorf("%w (abort of multipart upload failed, parts may remain: %v)", err, abortErr)
	}
	return err
}

// abortUpload sends AbortMultipartUpload for uploadID on a context detached
// from the caller's, bounded by abortTimeout, so a cancelled Put still
// aborts. NoSuchUpload, the answer for an upload already aborted, is
// success.
func (c *Client) abortUpload(ctx context.Context, key, uploadID string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abortTimeout)
	defer cancel()
	_, err := c.s3.AbortMultipartUpload(ctx, &awss3.AbortMultipartUploadInput{
		Bucket:   aws.String(c.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil && errorCode(err) != "NoSuchUpload" {
		return classify(err)
	}
	return nil
}

// bodyReader reads a Put body, counting its bytes and holding it to a
// declared size above 0: a body that ends short of the size fails with an
// error wrapping io.ErrUnexpectedEOF, and one that runs past it fails as
// soon as the byte past the size arrives. It keeps the first failure, the
// body's own or a size mismatch, in err, so Put can tell a body failure
// from a request's, and returns it again on every later Read.
type bodyReader struct {
	r    io.Reader
	size int64
	n    int64
	err  error
}

func (b *bodyReader) Read(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if b.size > 0 {
		if b.n >= b.size {
			// At the declared size, one more byte shows a longer body.
			var one [1]byte
			k, err := io.ReadAtLeast(b.r, one[:], 1)
			switch {
			case k > 0:
				b.err = fmt.Errorf("body is longer than the declared size (%d bytes)", b.size)
			case err == io.EOF:
				return 0, io.EOF
			default:
				b.err = err
			}
			return 0, b.err
		}
		if rest := b.size - b.n; int64(len(p)) > rest {
			p = p[:rest]
		}
	}
	k, err := b.r.Read(p)
	b.n += int64(k)
	if err == io.EOF && b.size > 0 && b.n < b.size {
		err = fmt.Errorf("body ended after %d bytes, short of the declared size (%d bytes): %w", b.n, b.size, io.ErrUnexpectedEOF)
	}
	if err != nil && err != io.EOF {
		b.err = err
	}
	return k, err
}

// Get opens the object at key with one GetObject request and returns a
// body that resumes after a failed read, as the try_timeout entry in the
// package documentation describes. NoSuchKey is storage.ErrNotFound and
// NoSuchBucket storage.ErrContainerNotFound, as classify maps them. The
// body classifies a read's failure with classifyRead: a try's deadline or a
// lost connection is storage.ErrUnavailable, and an object deleted or
// replaced before a resumption is storage.ErrNotFound, since the version
// being read is gone.
func (c *Client) Get(ctx context.Context, key string, _ storage.GetOptions) (storage.Blob, error) {
	if err := validateKey(key); err != nil {
		return storage.Blob{}, err
	}
	out, err := c.s3.GetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		return storage.Blob{}, classify(err)
	}
	size := aws.ToInt64(out.ContentLength)
	if out.ContentLength == nil {
		size = -1
	}
	body := &resumingBody{
		c:       c,
		ctx:     ctx,
		key:     key,
		etag:    aws.ToString(out.ETag),
		size:    size,
		retries: c.readRetries,
		body:    out.Body,
	}
	return storage.Blob{
		Key:         key,
		Size:        aws.ToInt64(out.ContentLength),
		ContentType: aws.ToString(out.ContentType),
		ETag:        entityTag(out.ETag),
		ModifiedAt:  aws.ToTime(out.LastModified),
		Body:        classifiedBody{body},
	}, nil
}

// errBodyClosed is a Get body's read failure after Close.
var errBodyClosed = errors.New("s3: read of a closed Get body")

// resumingBody is a Get body that resumes a failed read. When a read of the
// current answer's body fails, for a try's deadline or a lost connection,
// it sends a ranged GetObject from its offset, Range bytes=<offset>-,
// conditioned with If-Match on the first answer's ETag, so the bytes that
// follow come from the version the read began on. One Read resumes at most
// retries times before it returns the failure; a resumption's own
// GetObject is retried by the SDK as any request is.
//
// It does not resume when the caller's context is done, after Close, when the first answer carried no ETag to condition on, or when
// the failure came at or after the last byte, where the only failure left
// is the SDK's checksum check of a complete body. A failure it returns is
// returned again on every later Read.
//
// The SDK validates a checksum only at the end of a whole object's body,
// so a read that resumes is not checked; the package documentation says
// so.
type resumingBody struct {
	c       *Client
	ctx     context.Context
	key     string
	etag    string // the first answer's ETag, as the service sent it
	size    int64  // the object's length, or -1 when the answer gave none
	retries int

	offset  int64 // bytes delivered so far
	lastErr error // the failure that ended the last body
	err     error // the failure every later Read returns
	closed  atomic.Bool

	// mu guards body, which Close may reach from another goroutine to cut
	// off a Read in progress.
	mu   sync.Mutex
	body io.ReadCloser // the current answer's body; nil after a failure
}

func (b *resumingBody) Read(p []byte) (int, error) {
	for resumes := 0; ; {
		if b.closed.Load() {
			return 0, errBodyClosed
		}
		if b.err != nil {
			return 0, b.err
		}
		body := b.current()
		if body == nil {
			if resumes == b.retries || !b.resumable() {
				b.err = b.lastErr
				return 0, b.err
			}
			resumes++
			var err error
			if body, err = b.resume(); err != nil {
				b.err = err
				return 0, err
			}
		}
		n, err := body.Read(p)
		b.offset += int64(n)
		if err == nil || err == io.EOF {
			return n, err
		}
		_ = body.Close()
		b.setCurrent(nil)
		b.lastErr = err
		if n > 0 {
			// Deliver what arrived; the next Read resumes after it.
			return n, nil
		}
	}
}

// current returns the current answer's body, or nil after a failure.
func (b *resumingBody) current() io.ReadCloser {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.body
}

// setCurrent makes body the current answer's body. A body set after Close
// is closed at once.
func (b *resumingBody) setCurrent(body io.ReadCloser) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.body = body
	if body != nil && b.closed.Load() {
		_ = body.Close()
	}
}

// resumable reports whether the failure that ended the last body may be
// resumed after, as the type's documentation lists.
func (b *resumingBody) resumable() bool {
	return b.ctx.Err() == nil && b.etag != "" && (b.size < 0 || b.offset < b.size)
}

// resume sends the ranged, conditioned GetObject that continues the read at
// its offset, and makes its body the current one. An answer that does not
// start at the offset, from a service that ignored the Range, would repeat
// or skip bytes, so it fails the read.
func (b *resumingBody) resume() (io.ReadCloser, error) {
	out, err := b.c.s3.GetObject(b.ctx, &awss3.GetObjectInput{
		Bucket:  aws.String(b.c.bucket),
		Key:     aws.String(b.key),
		Range:   aws.String(fmt.Sprintf("bytes=%d-", b.offset)),
		IfMatch: aws.String(b.etag),
	})
	if err != nil {
		return nil, fmt.Errorf("s3: resume read of %q at byte %d: %w", b.key, b.offset, classifyRead(err))
	}
	if want := fmt.Sprintf("bytes %d-", b.offset); !strings.HasPrefix(aws.ToString(out.ContentRange), want) {
		_ = out.Body.Close()
		return nil, fmt.Errorf("s3: resume read of %q at byte %d: answer's Content-Range is %q, not the range asked for: %w",
			b.key, b.offset, aws.ToString(out.ContentRange), b.lastErr)
	}
	b.setCurrent(out.Body)
	return out.Body, nil
}

// Close closes the current answer's body and stops any later resumption,
// so a Read it cuts off fails rather than resumes.
func (b *resumingBody) Close() error {
	b.closed.Store(true)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.body != nil {
		return b.body.Close()
	}
	return nil
}

// classifiedBody classifies a Get body's read failures with classifyRead
// and passes io.EOF through unchanged.
type classifiedBody struct{ io.ReadCloser }

func (b classifiedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && err != io.EOF {
		err = classifyRead(err)
	}
	return n, err
}

// Stat reads the object's metadata with one HeadObject request. A HEAD
// answer carries no body, so a missing key and a missing bucket both arrive
// as a bare 404; a HeadBucket that follows it tells them apart. A missing
// bucket is storage.ErrContainerNotFound, an existing one makes the 404
// storage.ErrNotFound, and a HeadBucket that fails otherwise returns its
// own classified error, never storage.ErrNotFound.
func (c *Client) Stat(ctx context.Context, key string) (storage.Object, error) {
	if err := validateKey(key); err != nil {
		return storage.Object{}, err
	}
	out, err := c.s3.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		if errorCode(err) != "NotFound" {
			return storage.Object{}, classify(err)
		}
		if probeErr := c.Probe(ctx); probeErr != nil {
			return storage.Object{}, fmt.Errorf("s3: stat %q: object answered 404, bucket check: %w", key, probeErr)
		}
		return storage.Object{}, fmt.Errorf("%w: %w", storage.ErrNotFound, err)
	}
	return storage.Object{
		Key:         key,
		Size:        aws.ToInt64(out.ContentLength),
		ContentType: aws.ToString(out.ContentType),
		ETag:        entityTag(out.ETag),
		ModifiedAt:  aws.ToTime(out.LastModified),
	}, nil
}

// Delete removes the object at key with one DeleteObject request. S3
// answers a delete of a missing key with success; a gateway that answers
// NoSuchKey instead is treated as success too.
func (c *Client) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	_, err := c.s3.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if errorCode(err) == "NoSuchKey" {
		return nil
	}
	return classify(err)
}

// List fetches one page of the bucket's flat listing with one ListObjectsV2
// request. opts.Prefix, opts.Token, and opts.Limit map to its prefix,
// continuation token, and max-keys; a Limit past the int32 range is
// clamped, and S3 itself caps a page at 1,000 keys. Page.Next is the next
// continuation token while the listing is truncated.
func (c *Client) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	in := &awss3.ListObjectsV2Input{Bucket: aws.String(c.bucket)}
	if opts.Prefix != "" {
		in.Prefix = aws.String(opts.Prefix)
	}
	if opts.Token != "" {
		in.ContinuationToken = aws.String(opts.Token)
	}
	if opts.Limit > 0 {
		in.MaxKeys = aws.Int32(int32(min(opts.Limit, math.MaxInt32)))
	}

	out, err := c.s3.ListObjectsV2(ctx, in)
	if err != nil {
		return storage.Page{}, classify(err)
	}

	var page storage.Page
	if aws.ToBool(out.IsTruncated) {
		page.Next = aws.ToString(out.NextContinuationToken)
	}
	page.Objects = make([]storage.Object, 0, len(out.Contents))
	for _, item := range out.Contents {
		if item.Key == nil {
			continue
		}
		page.Objects = append(page.Objects, storage.Object{
			Key:  *item.Key,
			Size: aws.ToInt64(item.Size),
			ETag: entityTag(item.ETag),
			// A listing's time is ISO 8601 and may carry milliseconds, where
			// the Last-Modified header Put, Get, and Stat read has whole
			// seconds; truncating keeps the four in agreement.
			ModifiedAt: aws.ToTime(item.LastModified).UTC().Truncate(time.Second),
		})
	}
	return page, nil
}

// entityTag returns the ETag the service sent in HTTP entity-tag form, or ""
// when the answer carried none: an unquoted value gains its quotes, and a
// quoted or W/"..." value is returned as it is.
func entityTag(e *string) string {
	s := aws.ToString(e)
	if s == "" || strings.HasPrefix(s, `"`) || strings.HasPrefix(s, `W/"`) {
		return s
	}
	return `"` + s + `"`
}
