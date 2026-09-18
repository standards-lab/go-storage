package storagetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-storage"
)

// recorder is a testing.TB that records failures instead of failing the
// enclosing test, so a case can run over a deliberately broken client and
// the test can assert that the case caught it. Cleanup, Context, and the
// rest forward to the enclosing test.
type recorder struct {
	testing.TB

	mu       sync.Mutex
	failures []string
}

func (r *recorder) record(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = append(r.failures, msg)
}

func (r *recorder) Error(args ...any)                 { r.record(fmt.Sprint(args...)) }
func (r *recorder) Errorf(format string, args ...any) { r.record(fmt.Sprintf(format, args...)) }
func (r *recorder) Fail()                             { r.record("Fail") }
func (r *recorder) FailNow()                          { r.Fail(); runtime.Goexit() }
func (r *recorder) Fatal(args ...any)                 { r.Error(args...); runtime.Goexit() }
func (r *recorder) Fatalf(format string, args ...any) { r.Errorf(format, args...); runtime.Goexit() }

func (r *recorder) Failed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.failures) > 0
}

func (r *recorder) Failures() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.failures, "\n")
}

// runCase runs the named case over c through a recorder on its own
// goroutine, so a Fatal in the case ends that goroutine and not the test,
// and returns the recorder.
func runCase(t *testing.T, name string, c storage.Client) *recorder {
	t.Helper()
	var tc *testCase
	for i := range cases {
		if cases[i].name == name {
			tc = &cases[i]
		}
	}
	if tc == nil {
		t.Fatalf("no case named %q", name)
	}
	rec := &recorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		tc.run(rec, c)
	}()
	<-done
	return rec
}

func TestCases_PassOverFake(t *testing.T) {
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := runCase(t, tc.name, NewFake()); rec.Failed() {
				t.Errorf("case %s failed over a conforming Fake:\n%s", tc.name, rec.Failures())
			}
		})
	}
}

// notFoundOnDelete reports ErrNotFound from a Delete of a missing key.
type notFoundOnDelete struct{ *Fake }

func (c *notFoundOnDelete) Delete(ctx context.Context, key string) error {
	if _, err := c.Stat(ctx, key); err != nil {
		return err
	}
	return c.Fake.Delete(ctx, key)
}

// keepsContentType leaves the stored ContentType as it was when it replaces
// an object, the way a provider does when a replace omits the header.
type keepsContentType struct{ *Fake }

func (c *keepsContentType) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if old, err := c.Stat(ctx, key); err == nil {
		opts.ContentType = old.ContentType
	}
	return c.Fake.Put(ctx, key, body, opts)
}

// seekOnly refuses a body it cannot seek.
type seekOnly struct{ *Fake }

func (c *seekOnly) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if _, ok := body.(io.Seeker); !ok {
		return storage.Object{}, errors.New("body must implement io.Seeker")
	}
	return c.Fake.Put(ctx, key, body, opts)
}

// truncatingPut stores at most the first 1 MiB of a body.
type truncatingPut struct{ *Fake }

func (c *truncatingPut) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	return c.Fake.Put(ctx, key, io.LimitReader(body, 1<<20), opts)
}

// emptyETag reports no ETag from Put.
type emptyETag struct{ *Fake }

func (c *emptyETag) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	obj, err := c.Fake.Put(ctx, key, body, opts)
	obj.ETag = ""
	return obj, err
}

// onePageList never returns a continuation token.
type onePageList struct{ *Fake }

func (c *onePageList) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	page, err := c.Fake.List(ctx, opts)
	page.Next = ""
	return page, err
}

// ignoresPrefix lists the whole container whatever the Prefix.
type ignoresPrefix struct{ *Fake }

func (c *ignoresPrefix) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	opts.Prefix = ""
	return c.Fake.List(ctx, opts)
}

// ensureOnce fails EnsureContainer when the container already exists.
type ensureOnce struct{ *Fake }

func (c *ensureOnce) EnsureContainer(ctx context.Context) error {
	if c.HasContainer() {
		return errors.New("container already exists")
	}
	return c.Fake.EnsureContainer(ctx)
}

// permissiveKeys accepts every key, the empty one included.
type permissiveKeys struct{ *Fake }

func (c *permissiveKeys) Capabilities() storage.Capabilities {
	return storage.Capabilities{MaxKeyLength: 1, ValidateKey: func(string) error { return nil }}
}

// unquotedListETag strips the quotes from every ETag a listing reports, the
// way a provider does when it copies a listing's XML value verbatim.
type unquotedListETag struct{ *Fake }

func (c *unquotedListETag) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	page, err := c.Fake.List(ctx, opts)
	for i := range page.Objects {
		page.Objects[i].ETag = strings.Trim(page.Objects[i].ETag, `"`)
	}
	return page, err
}

// statETagDiffers reports an ETag from Stat other than the one Put reported.
type statETagDiffers struct{ *Fake }

func (c *statETagDiffers) Stat(ctx context.Context, key string) (storage.Object, error) {
	obj, err := c.Fake.Stat(ctx, key)
	obj.ETag = `"stat-` + strings.Trim(obj.ETag, `"`) + `"`
	return obj, err
}

// commitsPartialBody stores the bytes it read before the body failed and
// then returns the body's error.
type commitsPartialBody struct{ *Fake }

func (c *commitsPartialBody) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	data, readErr := io.ReadAll(body)
	opts.Size = 0
	obj, err := c.Fake.Put(ctx, key, bytes.NewReader(data), opts)
	if readErr != nil {
		return storage.Object{}, readErr
	}
	return obj, err
}

// ignoresSize stores the whole body whatever Size says.
type ignoresSize struct{ *Fake }

func (c *ignoresSize) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	opts.Size = 0
	return c.Fake.Put(ctx, key, body, opts)
}

// truncatesToSize stores the first Size bytes of a longer body.
type truncatesToSize struct{ *Fake }

func (c *truncatesToSize) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if opts.Size > 0 {
		body = io.LimitReader(body, opts.Size)
		opts.Size = 0
	}
	return c.Fake.Put(ctx, key, body, opts)
}

// overwritesBeforeFailing removes the existing object before it reads the
// body, so a body that fails leaves the key empty.
type overwritesBeforeFailing struct{ *Fake }

func (c *overwritesBeforeFailing) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if err := c.Delete(ctx, key); err != nil {
		return storage.Object{}, err
	}
	return c.Fake.Put(ctx, key, body, opts)
}

func TestCases_CatchBrokenClients(t *testing.T) {
	tests := []struct {
		name   string
		client storage.Client
		// failing is the case the broken client must fail.
		failing string
	}{
		{"Delete reports a missing key", &notFoundOnDelete{NewFake()}, "Delete"},
		{"Put keeps the old ContentType on replace", &keepsContentType{NewFake()}, "Replace"},
		{"Put requires a seekable body", &seekOnly{NewFake()}, "PutNonSeekableBody"},
		{"Put requires a seekable body under one-byte reads", &seekOnly{NewFake()}, "PutOneByteReads"},
		{"Put truncates a large body", &truncatingPut{NewFake()}, "LargeBody"},
		{"Put reports no ETag", &emptyETag{NewFake()}, "RoundTrip"},
		{"List never continues past its first page", &onePageList{NewFake()}, "List"},
		{"List ignores the prefix", &ignoresPrefix{NewFake()}, "List"},
		{"EnsureContainer fails over an existing container", &ensureOnce{NewFake(WithoutContainer())}, "EnsureContainer"},
		{"ValidateKey accepts every key", &permissiveKeys{NewFake()}, "Capabilities"},
		{"List reports an unquoted ETag", &unquotedListETag{NewFake()}, "List"},
		{"Stat reports an ETag other than Put's", &statETagDiffers{NewFake()}, "RoundTrip"},
		{"Put commits the bytes read before the body failed", &commitsPartialBody{NewFake()}, "PutBodyFailsMidway"},
		{"Put overwrites the existing object before failing", &overwritesBeforeFailing{NewFake()}, "PutBodyFailsMidway"},
		{"Put stores the body ignoring Size", &ignoresSize{NewFake()}, "PutSizeMismatch"},
		{"Put truncates the body to Size", &truncatesToSize{NewFake()}, "PutSizeMismatch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := runCase(t, tc.failing, tc.client)
			if !rec.Failed() {
				t.Errorf("case %s passed over a client that %s", tc.failing, tc.name)
			}
		})
	}
}
