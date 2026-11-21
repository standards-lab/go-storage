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
		return storagetest.NewFake(storagetest.Options{})
	})
}

func TestRun_FakeNoContainer(t *testing.T) {
	storagetest.Run(t, func(*testing.T) storage.Client {
		return storagetest.NewFake(storagetest.Options{NoContainer: true})
	})
}

func TestRun_Store(t *testing.T) {
	storagetest.Run(t, func(t *testing.T) storage.Client {
		cfg := storage.Config{Container: "conformance"}
		if err := cfg.Finalize(""); err != nil {
			t.Fatalf("finalize config: %v", err)
		}
		s := storage.New(storagetest.NewFake(storagetest.Options{NoContainer: true}), cfg)
		if err := s.Start(t.Context()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		return s
	})
}

func TestRunMissingContainer_Fake(t *testing.T) {
	storagetest.RunMissingContainer(t, func(*testing.T) storage.Client {
		return storagetest.NewFake(storagetest.Options{NoContainer: true})
	})
}

func TestRunMissingContainer_Store(t *testing.T) {
	storagetest.RunMissingContainer(t, func(t *testing.T) storage.Client {
		cfg := storage.Config{Container: "gone"}
		if err := cfg.Finalize(""); err != nil {
			t.Fatalf("finalize config: %v", err)
		}
		f := storagetest.NewFake(storagetest.Options{})
		s := storage.New(f, cfg)
		if err := s.Start(t.Context()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		f.DropContainer()
		return s
	})
}

const childEnv = "STORAGETEST_BROKEN_CLIENT"

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

type notFoundOnDelete struct{ *storagetest.Fake }

func (c *notFoundOnDelete) Delete(ctx context.Context, key string) error {
	if _, err := c.Stat(ctx, key); err != nil {
		return err
	}
	return c.Fake.Delete(ctx, key)
}

type keepsContentType struct{ *storagetest.Fake }

func (c *keepsContentType) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if old, err := c.Stat(ctx, key); err == nil {
		opts.ContentType = old.ContentType
	}
	return c.Fake.Put(ctx, key, body, opts)
}

type seekOnly struct{ *storagetest.Fake }

func (c *seekOnly) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if _, ok := body.(io.Seeker); !ok {
		return storage.Object{}, errors.New("body must implement io.Seeker")
	}
	return c.Fake.Put(ctx, key, body, opts)
}

type truncatingPut struct{ *storagetest.Fake }

func (c *truncatingPut) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	return c.Fake.Put(ctx, key, io.LimitReader(body, 1<<20), opts)
}

type emptyETag struct{ *storagetest.Fake }

func (c *emptyETag) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	obj, err := c.Fake.Put(ctx, key, body, opts)
	obj.ETag = ""
	return obj, err
}

type onePageList struct{ *storagetest.Fake }

func (c *onePageList) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	page, err := c.Fake.List(ctx, opts)
	page.Next = ""
	return page, err
}

type ignoresPrefix struct{ *storagetest.Fake }

func (c *ignoresPrefix) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	opts.Prefix = ""
	return c.Fake.List(ctx, opts)
}

type ensureOnce struct{ *storagetest.Fake }

func (c *ensureOnce) EnsureContainer(ctx context.Context) error {
	if c.HasContainer() {
		return errors.New("container already exists")
	}
	return c.Fake.EnsureContainer(ctx)
}

type permissiveKeys struct{ *storagetest.Fake }

func (c *permissiveKeys) Capabilities() storage.Capabilities {
	return storage.Capabilities{MaxKeyLength: 1, ValidateKey: func(string) error { return nil }}
}

type unquotedListETag struct{ *storagetest.Fake }

func (c *unquotedListETag) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	page, err := c.Fake.List(ctx, opts)
	for i := range page.Objects {
		page.Objects[i].ETag = strings.Trim(page.Objects[i].ETag, `"`)
	}
	return page, err
}

type statETagDiffers struct{ *storagetest.Fake }

func (c *statETagDiffers) Stat(ctx context.Context, key string) (storage.Object, error) {
	obj, err := c.Fake.Stat(ctx, key)
	obj.ETag = `"stat-` + strings.Trim(obj.ETag, `"`) + `"`
	return obj, err
}

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

type ignoresSize struct{ *storagetest.Fake }

func (c *ignoresSize) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	opts.Size = 0
	return c.Fake.Put(ctx, key, body, opts)
}

type truncatesToSize struct{ *storagetest.Fake }

func (c *truncatesToSize) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if opts.Size > 0 {
		body = io.LimitReader(body, opts.Size)
		opts.Size = 0
	}
	return c.Fake.Put(ctx, key, body, opts)
}

type overwritesBeforeFailing struct{ *storagetest.Fake }

func (c *overwritesBeforeFailing) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if err := c.Delete(ctx, key); err != nil {
		return storage.Object{}, err
	}
	return c.Fake.Put(ctx, key, body, opts)
}

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

type noDefaultContentType struct{ *storagetest.Fake }

func (c *noDefaultContentType) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	obj, err := c.Fake.Put(ctx, key, body, opts)
	if opts.ContentType == "" {
		obj.ContentType = ""
	}
	return obj, err
}

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

type nonUTCListModifiedAt struct{ *storagetest.Fake }

func (c *nonUTCListModifiedAt) List(ctx context.Context, opts storage.ListOptions) (storage.Page, error) {
	page, err := c.Fake.List(ctx, opts)
	for i := range page.Objects {
		page.Objects[i].ModifiedAt = page.Objects[i].ModifiedAt.In(gmt)
	}
	return page, err
}

var brokenClients = []struct {
	name    string
	client  func() storage.Client
	failing string
}{
	{"Delete reports a missing key", func() storage.Client { return &notFoundOnDelete{storagetest.NewFake(storagetest.Options{})} }, "Delete"},
	{"Put keeps the old ContentType on replace", func() storage.Client { return &keepsContentType{storagetest.NewFake(storagetest.Options{})} }, "Replace"},
	{"Put requires a seekable body", func() storage.Client { return &seekOnly{storagetest.NewFake(storagetest.Options{})} }, "PutNonSeekableBody"},
	{"Put requires a seekable body under one-byte reads", func() storage.Client { return &seekOnly{storagetest.NewFake(storagetest.Options{})} }, "PutOneByteReads"},
	{"Put truncates a large body", func() storage.Client { return &truncatingPut{storagetest.NewFake(storagetest.Options{})} }, "LargeBody"},
	{"Put reports no ETag", func() storage.Client { return &emptyETag{storagetest.NewFake(storagetest.Options{})} }, "RoundTrip"},
	{"List never continues past its first page", func() storage.Client { return &onePageList{storagetest.NewFake(storagetest.Options{})} }, "List"},
	{"List ignores the prefix", func() storage.Client { return &ignoresPrefix{storagetest.NewFake(storagetest.Options{})} }, "List"},
	{"EnsureContainer fails over an existing container", func() storage.Client {
		return &ensureOnce{storagetest.NewFake(storagetest.Options{NoContainer: true})}
	}, "EnsureContainer"},
	{"ValidateKey accepts every key", func() storage.Client { return &permissiveKeys{storagetest.NewFake(storagetest.Options{})} }, "Capabilities"},
	{"List reports an unquoted ETag", func() storage.Client { return &unquotedListETag{storagetest.NewFake(storagetest.Options{})} }, "List"},
	{"Stat reports an ETag other than Put's", func() storage.Client { return &statETagDiffers{storagetest.NewFake(storagetest.Options{})} }, "RoundTrip"},
	{"Put commits the bytes read before the body failed", func() storage.Client { return &commitsPartialBody{storagetest.NewFake(storagetest.Options{})} }, "PutBodyFailsMidway"},
	{"Put overwrites the existing object before failing", func() storage.Client {
		return &overwritesBeforeFailing{storagetest.NewFake(storagetest.Options{})}
	}, "PutBodyFailsMidway"},
	{"Put stores the body ignoring Size", func() storage.Client { return &ignoresSize{storagetest.NewFake(storagetest.Options{})} }, "PutSizeMismatch"},
	{"Put truncates the body to Size", func() storage.Client { return &truncatesToSize{storagetest.NewFake(storagetest.Options{})} }, "PutSizeMismatch"},
	{"a missing key also matches ErrContainerNotFound", func() storage.Client {
		return &alsoClassifies{storagetest.NewFake(storagetest.Options{}), storage.ErrContainerNotFound}
	}, "MissingKey"},
	{"a missing key also matches ErrUnavailable", func() storage.Client {
		return &alsoClassifies{storagetest.NewFake(storagetest.Options{}), storage.ErrUnavailable}
	}, "MissingKey"},
	{"Put reports no ContentType when none was given", func() storage.Client { return &noDefaultContentType{storagetest.NewFake(storagetest.Options{})} }, "PutUnknownSize"},
	{"Put, Get, and Stat report ModifiedAt outside UTC", func() storage.Client { return &nonUTCModifiedAt{storagetest.NewFake(storagetest.Options{})} }, "RoundTrip"},
	{"List reports ModifiedAt outside UTC", func() storage.Client { return &nonUTCListModifiedAt{storagetest.NewFake(storagetest.Options{})} }, "List"},
}

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

type notFoundContainer struct{ *storagetest.Fake }

func (c notFoundContainer) Probe(context.Context) error {
	return fmt.Errorf("%w: container gone", storage.ErrNotFound)
}

type swallowsContainer struct{ *storagetest.Fake }

func (c swallowsContainer) Delete(context.Context, string) error { return nil }

type createsContainer struct{ *storagetest.Fake }

func (c createsContainer) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	if err := c.EnsureContainer(ctx); err != nil {
		return storage.Object{}, err
	}
	return c.Fake.Put(ctx, key, body, opts)
}

var missingContainerOps = []string{"Probe", "Put", "Get", "Stat", "Delete", "List"}

func TestRunMissingContainer_CatchesBrokenClients(t *testing.T) {
	tests := []struct {
		name   string
		client func() storage.Client
		op     string
	}{
		{"a Probe that reports ErrNotFound", func() storage.Client {
			return notFoundContainer{storagetest.NewFake(storagetest.Options{NoContainer: true})}
		}, "Probe"},
		{"a Delete that swallows the missing container", func() storage.Client {
			return swallowsContainer{storagetest.NewFake(storagetest.Options{NoContainer: true})}
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

func TestRunMissingContainer_CleansUpAStrayPut(t *testing.T) {
	const client = "createsContainer"
	if os.Getenv(childEnv) == client {
		f := storagetest.NewFake(storagetest.Options{NoContainer: true})
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
