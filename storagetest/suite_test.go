package storagetest_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"
)

func TestRun_Fake(t *testing.T) {
	storagetest.Run(t, func(*testing.T) storage.Client {
		return storagetest.NewFake()
	})
}

func TestRun_FakeWithoutContainer(t *testing.T) {
	storagetest.Run(t, func(*testing.T) storage.Client {
		return storagetest.NewFake(storagetest.WithoutContainer())
	})
}

func TestRun_Store(t *testing.T) {
	storagetest.Run(t, func(t *testing.T) storage.Client {
		cfg := storage.Config{Container: "conformance"}
		if err := cfg.Finalize(""); err != nil {
			t.Fatalf("finalize config: %v", err)
		}
		s := storage.New(storagetest.NewFake(storagetest.WithoutContainer()), cfg)
		if err := s.Start(t.Context()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		return s
	})
}

func TestRunMissingContainer_Fake(t *testing.T) {
	storagetest.RunMissingContainer(t, func(*testing.T) storage.Client {
		return storagetest.NewFake(storagetest.WithoutContainer())
	})
}

// A started Store whose container was dropped behind it passes the
// provider's classification through.
func TestRunMissingContainer_Store(t *testing.T) {
	storagetest.RunMissingContainer(t, func(t *testing.T) storage.Client {
		cfg := storage.Config{Container: "gone"}
		if err := cfg.Finalize(""); err != nil {
			t.Fatalf("finalize config: %v", err)
		}
		f := storagetest.NewFake()
		s := storage.New(f, cfg)
		if err := s.Start(t.Context()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		f.DropContainer()
		return s
	})
}

// childEnv names the broken client a test runs the suite over when it runs
// as the child process its parent starts. A suite that catches a broken
// client fails the test it runs in, so the parent runs it in a child and
// reads the child's verdict from its exit status and output.
const childEnv = "STORAGETEST_BROKEN_CLIENT"

// runChild reruns the test binary with childEnv set to client, running only
// the tests run matches, and returns the child's output and whether it
// failed.
func runChild(t *testing.T, run, client string) (string, bool) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.v", "-test.run="+run)
	cmd.Env = append(os.Environ(), childEnv+"="+client)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("run child: %v", err)
	}
	return string(out), err != nil
}

// notFoundOnDelete reports ErrNotFound from a Delete of a missing key.
type notFoundOnDelete struct{ *storagetest.Fake }

func (c *notFoundOnDelete) Delete(ctx context.Context, key string) error {
	if _, err := c.Stat(ctx, key); err != nil {
		return err
	}
	return c.Fake.Delete(ctx, key)
}

// keepsContentType leaves the stored ContentType as it was when it replaces
// an object, the way a provider does when a replace omits the header.
type keepsContentType struct{ *storagetest.Fake }

func (c *keepsContentType) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if old, err := c.Stat(ctx, key); err == nil {
		opts.ContentType = old.ContentType
	}
	return c.Fake.Put(ctx, key, body, opts)
}

// seekOnly refuses a body it cannot seek.
type seekOnly struct{ *storagetest.Fake }

func (c *seekOnly) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if _, ok := body.(io.Seeker); !ok {
		return storage.Object{}, errors.New("body must implement io.Seeker")
	}
	return c.Fake.Put(ctx, key, body, opts)
}

// truncatingPut stores at most the first 1 MiB of a body.
type truncatingPut struct{ *storagetest.Fake }

func (c *truncatingPut) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	return c.Fake.Put(ctx, key, io.LimitReader(body, 1<<20), opts)
}

// emptyETag reports no ETag from Put.
type emptyETag struct{ *storagetest.Fake }

func (c *emptyETag) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	obj, err := c.Fake.Put(ctx, key, body, opts)
	obj.ETag = ""
	return obj, err
}

// onePageList never returns a continuation token.
type onePageList struct{ *storagetest.Fake }

func (c *onePageList) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	page, err := c.Fake.List(ctx, opts)
	page.Next = ""
	return page, err
}

// ignoresPrefix lists the whole container whatever the Prefix.
type ignoresPrefix struct{ *storagetest.Fake }

func (c *ignoresPrefix) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	opts.Prefix = ""
	return c.Fake.List(ctx, opts)
}

// ensureOnce fails EnsureContainer when the container already exists.
type ensureOnce struct{ *storagetest.Fake }

func (c *ensureOnce) EnsureContainer(ctx context.Context) error {
	if c.HasContainer() {
		return errors.New("container already exists")
	}
	return c.Fake.EnsureContainer(ctx)
}

// permissiveKeys accepts every key, the empty one included.
type permissiveKeys struct{ *storagetest.Fake }

func (c *permissiveKeys) Capabilities() storage.Capabilities {
	return storage.Capabilities{MaxKeyLength: 1, ValidateKey: func(string) error { return nil }}
}

// unquotedListETag strips the quotes from every ETag a listing reports, the
// way a provider does when it copies a listing's XML value verbatim.
type unquotedListETag struct{ *storagetest.Fake }

func (c *unquotedListETag) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	page, err := c.Fake.List(ctx, opts)
	for i := range page.Objects {
		page.Objects[i].ETag = strings.Trim(page.Objects[i].ETag, `"`)
	}
	return page, err
}

// statETagDiffers reports an ETag from Stat other than the one Put reported.
type statETagDiffers struct{ *storagetest.Fake }

func (c *statETagDiffers) Stat(ctx context.Context, key string) (storage.Object, error) {
	obj, err := c.Fake.Stat(ctx, key)
	obj.ETag = `"stat-` + strings.Trim(obj.ETag, `"`) + `"`
	return obj, err
}

// commitsPartialBody stores the bytes it read before the body failed and
// then returns the body's error.
type commitsPartialBody struct{ *storagetest.Fake }

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
type ignoresSize struct{ *storagetest.Fake }

func (c *ignoresSize) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	opts.Size = 0
	return c.Fake.Put(ctx, key, body, opts)
}

// truncatesToSize stores the first Size bytes of a longer body.
type truncatesToSize struct{ *storagetest.Fake }

func (c *truncatesToSize) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if opts.Size > 0 {
		body = io.LimitReader(body, opts.Size)
		opts.Size = 0
	}
	return c.Fake.Put(ctx, key, body, opts)
}

// overwritesBeforeFailing removes the existing object before it reads the
// body, so a body that fails leaves the key empty.
type overwritesBeforeFailing struct{ *storagetest.Fake }

func (c *overwritesBeforeFailing) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if err := c.Delete(ctx, key); err != nil {
		return storage.Object{}, err
	}
	return c.Fake.Put(ctx, key, body, opts)
}

// alsoClassifies wraps a missing key's error under a second sentinel as
// well, so it matches ErrNotFound and also.
type alsoClassifies struct {
	*storagetest.Fake
	also error
}

func (c *alsoClassifies) Get(ctx context.Context, key string, opts storage.GetOptions) (storage.Blob, error) {
	blob, err := c.Fake.Get(ctx, key, opts)
	return blob, c.wrap(err)
}

func (c *alsoClassifies) Stat(ctx context.Context, key string) (storage.Object, error) {
	obj, err := c.Fake.Stat(ctx, key)
	return obj, c.wrap(err)
}

func (c *alsoClassifies) wrap(err error) error {
	if errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("%w: %w", c.also, err)
	}
	return err
}

// noDefaultContentType reports an empty ContentType for a Put without one,
// where the contract asks for application/octet-stream.
type noDefaultContentType struct{ *storagetest.Fake }

func (c *noDefaultContentType) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	obj, err := c.Fake.Put(ctx, key, body, opts)
	if opts.ContentType == "" {
		obj.ContentType = ""
	}
	return obj, err
}

// nonUTCModifiedAt reports Put's, Get's, and Stat's ModifiedAt in a fixed
// GMT zone, the way a provider does that keeps the zone its SDK parsed an
// RFC 1123 header in. The instant is unchanged, so only the zone check
// catches it.
type nonUTCModifiedAt struct{ *storagetest.Fake }

var gmt = time.FixedZone("GMT", 0)

func (c *nonUTCModifiedAt) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	obj, err := c.Fake.Put(ctx, key, body, opts)
	obj.ModifiedAt = obj.ModifiedAt.In(gmt)
	return obj, err
}

func (c *nonUTCModifiedAt) Get(ctx context.Context, key string, opts storage.GetOptions) (storage.Blob, error) {
	blob, err := c.Fake.Get(ctx, key, opts)
	blob.ModifiedAt = blob.ModifiedAt.In(gmt)
	return blob, err
}

func (c *nonUTCModifiedAt) Stat(ctx context.Context, key string) (storage.Object, error) {
	obj, err := c.Fake.Stat(ctx, key)
	obj.ModifiedAt = obj.ModifiedAt.In(gmt)
	return obj, err
}

// nonUTCListModifiedAt reports List's ModifiedAt in a fixed GMT zone.
type nonUTCListModifiedAt struct{ *storagetest.Fake }

func (c *nonUTCListModifiedAt) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	page, err := c.Fake.List(ctx, opts)
	for i := range page.Objects {
		page.Objects[i].ModifiedAt = page.Objects[i].ModifiedAt.In(gmt)
	}
	return page, err
}

// brokenClients are the clients TestRun_CatchesBrokenClients runs the suite
// over, each named for the contract it breaks, with the suite case that
// must fail it.
var brokenClients = []struct {
	name    string
	client  func() storage.Client
	failing string
}{
	{"Delete reports a missing key", func() storage.Client { return &notFoundOnDelete{storagetest.NewFake()} }, "Delete"},
	{"Put keeps the old ContentType on replace", func() storage.Client { return &keepsContentType{storagetest.NewFake()} }, "Replace"},
	{"Put requires a seekable body", func() storage.Client { return &seekOnly{storagetest.NewFake()} }, "PutNonSeekableBody"},
	{"Put requires a seekable body under one-byte reads", func() storage.Client { return &seekOnly{storagetest.NewFake()} }, "PutOneByteReads"},
	{"Put truncates a large body", func() storage.Client { return &truncatingPut{storagetest.NewFake()} }, "LargeBody"},
	{"Put reports no ETag", func() storage.Client { return &emptyETag{storagetest.NewFake()} }, "RoundTrip"},
	{"List never continues past its first page", func() storage.Client { return &onePageList{storagetest.NewFake()} }, "List"},
	{"List ignores the prefix", func() storage.Client { return &ignoresPrefix{storagetest.NewFake()} }, "List"},
	{"EnsureContainer fails over an existing container", func() storage.Client {
		return &ensureOnce{storagetest.NewFake(storagetest.WithoutContainer())}
	}, "EnsureContainer"},
	{"ValidateKey accepts every key", func() storage.Client { return &permissiveKeys{storagetest.NewFake()} }, "Capabilities"},
	{"List reports an unquoted ETag", func() storage.Client { return &unquotedListETag{storagetest.NewFake()} }, "List"},
	{"Stat reports an ETag other than Put's", func() storage.Client { return &statETagDiffers{storagetest.NewFake()} }, "RoundTrip"},
	{"Put commits the bytes read before the body failed", func() storage.Client { return &commitsPartialBody{storagetest.NewFake()} }, "PutBodyFailsMidway"},
	{"Put overwrites the existing object before failing", func() storage.Client {
		return &overwritesBeforeFailing{storagetest.NewFake()}
	}, "PutBodyFailsMidway"},
	{"Put stores the body ignoring Size", func() storage.Client { return &ignoresSize{storagetest.NewFake()} }, "PutSizeMismatch"},
	{"Put truncates the body to Size", func() storage.Client { return &truncatesToSize{storagetest.NewFake()} }, "PutSizeMismatch"},
	{"a missing key also matches ErrContainerNotFound", func() storage.Client {
		return &alsoClassifies{storagetest.NewFake(), storage.ErrContainerNotFound}
	}, "MissingKey"},
	{"a missing key also matches ErrUnavailable", func() storage.Client {
		return &alsoClassifies{storagetest.NewFake(), storage.ErrUnavailable}
	}, "MissingKey"},
	{"Put reports no ContentType when none was given", func() storage.Client { return &noDefaultContentType{storagetest.NewFake()} }, "PutUnknownSize"},
	{"Put, Get, and Stat report ModifiedAt outside UTC", func() storage.Client { return &nonUTCModifiedAt{storagetest.NewFake()} }, "RoundTrip"},
	{"List reports ModifiedAt outside UTC", func() storage.Client { return &nonUTCListModifiedAt{storagetest.NewFake()} }, "List"},
}

// Run fails the case that proves the contract a broken client breaks. As
// the child, the test runs Run over the client childEnv names.
func TestRun_CatchesBrokenClients(t *testing.T) {
	if name := os.Getenv(childEnv); name != "" {
		for _, bc := range brokenClients {
			if bc.name == name {
				storagetest.Run(t, func(*testing.T) storage.Client { return bc.client() })
				return
			}
		}
		t.Fatalf("no broken client named %q", name)
	}

	for _, bc := range brokenClients {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			out, failed := runChild(t, "^TestRun_CatchesBrokenClients$/^"+bc.failing+"$", bc.name)
			if want := "--- FAIL: TestRun_CatchesBrokenClients/" + bc.failing + " "; !failed || !strings.Contains(out, want) {
				t.Errorf("case %s passed over a client where %s:\n%s", bc.failing, bc.name, out)
			}
		})
	}
}

// notFoundContainer reports a missing container the way v0.1.0 providers
// did, as a missing object, which the missing-container check must reject.
type notFoundContainer struct{ *storagetest.Fake }

func (c notFoundContainer) Probe(context.Context) error {
	return fmt.Errorf("%w: container gone", storage.ErrNotFound)
}

// swallowsContainer treats a missing container on Delete as the idempotent
// success of a missing key, the likeliest regression.
type swallowsContainer struct{ *storagetest.Fake }

func (c swallowsContainer) Delete(context.Context, string) error { return nil }

// createsContainer creates its container on a Put, as a provider that
// provisions on demand would.
type createsContainer struct{ *storagetest.Fake }

func (c createsContainer) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if err := c.EnsureContainer(ctx); err != nil {
		return storage.Object{}, err
	}
	return c.Fake.Put(ctx, key, body, opts)
}

// missingContainerOps are the operations RunMissingContainer checks, by the
// name its failures give them.
var missingContainerOps = []string{"Probe", "Put", "Get", "Stat", "Delete", "List"}

// RunMissingContainer fails each operation a broken client misclassifies
// and no other. As the child, the test runs RunMissingContainer over the
// client childEnv names.
func TestRunMissingContainer_CatchesBrokenClients(t *testing.T) {
	tests := []struct {
		name   string
		client func() storage.Client
		op     string
	}{
		{"a Probe that reports ErrNotFound", func() storage.Client {
			return notFoundContainer{storagetest.NewFake(storagetest.WithoutContainer())}
		}, "Probe"},
		{"a Delete that swallows the missing container", func() storage.Client {
			return swallowsContainer{storagetest.NewFake(storagetest.WithoutContainer())}
		}, "Delete"},
	}
	if name := os.Getenv(childEnv); name != "" {
		for _, tc := range tests {
			if tc.name == name {
				storagetest.RunMissingContainer(t, func(*testing.T) storage.Client { return tc.client() })
				return
			}
		}
		t.Fatalf("no broken client named %q", name)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, failed := runChild(t, "^TestRunMissingContainer_CatchesBrokenClients$", tc.name)
			if !failed {
				t.Fatalf("RunMissingContainer passed %s, want a failure:\n%s", tc.name, out)
			}
			for _, op := range missingContainerOps {
				if got, want := strings.Contains(out, op+" without a container"), op == tc.op; got != want {
					t.Errorf("RunMissingContainer over %s: failure of %s = %t, want %t:\n%s", tc.name, op, got, want, out)
				}
			}
		})
	}
}

// A client whose Put creates the container fails RunMissingContainer, and
// the object it stored is deleted when the check's test ends. As the child,
// the test runs the check in a subtest and then reports what the store
// holds.
func TestRunMissingContainer_CleansUpAStrayPut(t *testing.T) {
	const client = "createsContainer"
	if os.Getenv(childEnv) == client {
		f := storagetest.NewFake(storagetest.WithoutContainer())
		t.Run("check", func(t *testing.T) {
			storagetest.RunMissingContainer(t, func(*testing.T) storage.Client { return createsContainer{f} })
		})
		page, err := f.List(context.Background(), storage.ListOptions{})
		t.Logf("after the check the store holds %d objects (%v)", len(page.Objects), err)
		return
	}

	out, failed := runChild(t, "^TestRunMissingContainer_CleansUpAStrayPut$", client)
	if !failed || !strings.Contains(out, "Put without a container") {
		t.Errorf("RunMissingContainer passed a Put that created the container, want a failure:\n%s", out)
	}
	if !strings.Contains(out, "after the check the store holds 0 objects (<nil>)") {
		t.Errorf("RunMissingContainer left the stray object behind:\n%s", out)
	}
}
