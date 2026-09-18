package storagetest_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"
)

var _ storage.Client = (*storagetest.Fake)(nil)

// putString stores content at key and fails the test on error.
func putString(t *testing.T, c storage.Client, key, content string) storage.Object {
	t.Helper()
	obj, err := c.Put(context.Background(), key, strings.NewReader(content), storage.PutOptions{})
	if err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
	return obj
}

// readBlob drains and closes a Get result.
func readBlob(t *testing.T, blob storage.Blob) string {
	t.Helper()
	data, err := io.ReadAll(blob.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := blob.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	return string(data)
}

// listKeys walks a listing to its final page and returns every key seen, in
// order, along with the number of pages it took.
func listKeys(t *testing.T, c storage.Client, prefix string, limit int) ([]string, int) {
	t.Helper()
	var keys []string
	pages := 0
	token := ""
	for {
		page, err := c.List(context.Background(), storage.ListOptions{Prefix: prefix, Token: token, Limit: limit})
		if err != nil {
			t.Fatalf("List(page %d): %v", pages, err)
		}
		pages++
		for _, obj := range page.Objects {
			keys = append(keys, obj.Key)
		}
		if page.Next == "" {
			return keys, pages
		}
		token = page.Next
		if pages > 100 {
			t.Fatal("List never returned an empty Next")
		}
	}
}

// errReader fails on its first Read.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestFake_PutGetRoundTrip(t *testing.T) {
	stamp := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	f := storagetest.NewFake(storagetest.WithClock(func() time.Time { return stamp }))
	ctx := context.Background()

	obj, err := f.Put(ctx, "a/hello.txt", strings.NewReader("hello"), storage.PutOptions{ContentType: "text/plain"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if obj.Key != "a/hello.txt" {
		t.Errorf("Key = %q, want a/hello.txt", obj.Key)
	}
	if obj.Size != 5 {
		t.Errorf("Size = %d, want 5", obj.Size)
	}
	if obj.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want text/plain", obj.ContentType)
	}
	if obj.ETag == "" {
		t.Error("ETag is empty")
	}
	if !obj.ModifiedAt.Equal(stamp) {
		t.Errorf("ModifiedAt = %v, want the injected clock's %v", obj.ModifiedAt, stamp)
	}

	blob, err := f.Get(ctx, "a/hello.txt", storage.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := readBlob(t, blob); got != "hello" {
		t.Errorf("body = %q, want hello", got)
	}
	if blob.Object != obj {
		t.Errorf("Get metadata = %+v, want the Put result %+v", blob.Object, obj)
	}

	stat, err := f.Stat(ctx, "a/hello.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat != obj {
		t.Errorf("Stat = %+v, want the Put result %+v", stat, obj)
	}
}

func TestFake_ETagFollowsContent(t *testing.T) {
	f := storagetest.NewFake()

	same1 := putString(t, f, "one", "content")
	same2 := putString(t, f, "two", "content")
	other := putString(t, f, "three", "different")

	if same1.ETag != same2.ETag {
		t.Errorf("ETags for equal content differ: %q vs %q", same1.ETag, same2.ETag)
	}
	if same1.ETag == other.ETag {
		t.Errorf("ETags for different content are equal: %q", same1.ETag)
	}
}

func TestFake_PutReplaces(t *testing.T) {
	f := storagetest.NewFake()

	first := putString(t, f, "k", "first")
	second := putString(t, f, "k", "second value")

	if second.Size != 12 {
		t.Errorf("Size after overwrite = %d, want 12", second.Size)
	}
	if first.ETag == second.ETag {
		t.Error("ETag unchanged after overwrite with different content")
	}
	blob, err := f.Get(context.Background(), "k", storage.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := readBlob(t, blob); got != "second value" {
		t.Errorf("body = %q, want second value", got)
	}
	if keys, _ := listKeys(t, f, "", 0); len(keys) != 1 {
		t.Errorf("List after overwrite returned %v, want one key", keys)
	}
}

func TestFake_MissingKey(t *testing.T) {
	f := storagetest.NewFake()
	ctx := context.Background()

	tests := []struct {
		name string
		call func() error
	}{
		{"Get", func() error {
			_, err := f.Get(ctx, "missing", storage.GetOptions{})
			return err
		}},
		{"Stat", func() error {
			_, err := f.Stat(ctx, "missing")
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("errors.Is(err, ErrNotFound) = false for %v", err)
			}
			if !errors.Is(err, storagetest.ErrNoSuchKey) {
				t.Errorf("errors.Is(err, storagetest.ErrNoSuchKey) = false for %v, want the cause to stay matchable", err)
			}
		})
	}
}

func TestFake_DeleteIdempotent(t *testing.T) {
	f := storagetest.NewFake()
	ctx := context.Background()

	if err := f.Delete(ctx, "never-stored"); err != nil {
		t.Errorf("Delete of a missing key = %v, want nil", err)
	}

	putString(t, f, "k", "v")
	if err := f.Delete(ctx, "k"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.Get(ctx, "k", storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := f.Delete(ctx, "k"); err != nil {
		t.Errorf("second Delete = %v, want nil", err)
	}
}

func TestFake_ListPrefix(t *testing.T) {
	f := storagetest.NewFake()
	for _, key := range []string{"b/2", "a/1", "b/1", "c/1", "a/2"} {
		putString(t, f, key, key)
	}

	tests := []struct {
		prefix string
		want   []string
	}{
		{"", []string{"a/1", "a/2", "b/1", "b/2", "c/1"}},
		{"a/", []string{"a/1", "a/2"}},
		{"b/", []string{"b/1", "b/2"}},
		{"z/", nil},
	}
	for _, tc := range tests {
		t.Run("prefix="+tc.prefix, func(t *testing.T) {
			got, _ := listKeys(t, f, tc.prefix, 0)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("List(%q) = %v, want %v", tc.prefix, got, tc.want)
			}
		})
	}
}

func TestFake_ListPagesToTheEnd(t *testing.T) {
	f := storagetest.NewFake()
	want := []string{"k1", "k2", "k3", "k4", "k5"}
	for _, key := range want {
		putString(t, f, key, key)
	}

	got, pages := listKeys(t, f, "", 2)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("walked keys = %v, want %v", got, want)
	}
	if pages != 3 {
		t.Errorf("pages = %d, want 3 for 5 objects at Limit 2", pages)
	}

	// The last page of an exact multiple has an empty Next rather than an
	// empty trailing page.
	putString(t, f, "k6", "k6")
	if _, pages := listKeys(t, f, "", 2); pages != 3 {
		t.Errorf("pages = %d, want 3 for 6 objects at Limit 2", pages)
	}
}

func TestFake_ListDefaultPageSize(t *testing.T) {
	f := storagetest.NewFake(storagetest.WithPageSize(2))
	for _, key := range []string{"k1", "k2", "k3"} {
		putString(t, f, key, key)
	}

	page, err := f.List(context.Background(), storage.ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Objects) != 2 {
		t.Errorf("Limit 0 returned %d objects, want the configured page size 2", len(page.Objects))
	}
	if page.Next == "" {
		t.Error("Next is empty with a third object remaining")
	}

	page, err = f.List(context.Background(), storage.ListOptions{Prefix: "k", Token: page.Next, Limit: 7})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Objects) != 1 || page.Next != "" {
		t.Errorf("final page = %+v, want one object and an empty Next", page)
	}
	if got := f.LastList(); got.Prefix != "k" || got.Limit != 7 || got.Token == "" {
		t.Errorf("LastList() = %+v, want the options of the final call", got)
	}
}

func TestFake_OutageToggle(t *testing.T) {
	f := storagetest.NewFake()
	ctx := context.Background()
	putString(t, f, "k", "v")

	ops := []struct {
		name string
		call func() error
	}{
		{"Put", func() error {
			_, err := f.Put(ctx, "k", strings.NewReader("v"), storage.PutOptions{})
			return err
		}},
		{"Get", func() error {
			_, err := f.Get(ctx, "k", storage.GetOptions{})
			return err
		}},
		{"Stat", func() error {
			_, err := f.Stat(ctx, "k")
			return err
		}},
		{"Delete", func() error { return f.Delete(ctx, "k") }},
		{"List", func() error {
			_, err := f.List(ctx, storage.ListOptions{})
			return err
		}},
		{"EnsureContainer", func() error { return f.EnsureContainer(ctx) }},
		{"Probe", func() error { return f.Probe(ctx) }},
	}

	f.Down.Store(true)
	for _, op := range ops {
		err := op.call()
		if !errors.Is(err, storage.ErrUnavailable) {
			t.Errorf("%s during outage = %v, want ErrUnavailable", op.name, err)
		}
		if !errors.Is(err, storagetest.ErrDown) {
			t.Errorf("%s during outage = %v, want the cause to stay matchable", op.name, err)
		}
	}

	f.Down.Store(false)
	for _, op := range ops {
		if err := op.call(); err != nil {
			t.Errorf("%s after the outage = %v, want nil", op.name, err)
		}
	}
}

func TestFake_PutBodyReadFails(t *testing.T) {
	f := storagetest.NewFake()
	cause := errors.New("stream broke")

	_, err := f.Put(context.Background(), "k", errReader{err: cause}, storage.PutOptions{})
	if !errors.Is(err, cause) {
		t.Fatalf("Put = %v, want it to wrap the body's read error", err)
	}
	if _, err := f.Stat(context.Background(), "k"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat after a failed Put = %v, want ErrNotFound", err)
	}
}

func TestFake_PutRecordsCall(t *testing.T) {
	f := storagetest.NewFake()

	if got := f.Puts(); got != 0 {
		t.Fatalf("puts before any Put = %d, want 0", got)
	}

	opts := storage.PutOptions{ContentType: "application/json", Size: 7}
	if _, err := f.Put(context.Background(), "k", strings.NewReader("1234567"), opts); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if got := f.Puts(); got != 1 {
		t.Errorf("puts = %d, want 1", got)
	}
	gotOpts, consumed := f.LastPut()
	if gotOpts != opts {
		t.Errorf("LastPut() opts = %+v, want %+v", gotOpts, opts)
	}
	if consumed != 7 {
		t.Errorf("LastPut() consumed = %d, want 7", consumed)
	}
}

func TestFake_PutInjectedFailure(t *testing.T) {
	f := storagetest.NewFake()
	cause := errors.New("provider rejected the write")
	f.FailPut(cause)

	_, err := f.Put(context.Background(), "k", strings.NewReader("abc"), storage.PutOptions{})
	if !errors.Is(err, cause) {
		t.Fatalf("Put = %v, want the injected failure", err)
	}
	// The failure fires after the body has been read, which is when a real
	// provider learns the outcome of its request.
	if _, consumed := f.LastPut(); consumed != 3 {
		t.Errorf("consumed = %d, want the whole body read before the failure", consumed)
	}
	if _, err := f.Stat(context.Background(), "k"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat after the failed Put = %v, want ErrNotFound", err)
	}

	f.FailPut(nil)
	putString(t, f, "k", "abc")
}

func TestFake_ProbeRecordsDeadline(t *testing.T) {
	f := storagetest.NewFake()

	if err := f.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := f.Probes(); got != 1 {
		t.Errorf("probes = %d, want 1", got)
	}
	if _, bounded := f.LastProbe(); bounded {
		t.Error("LastProbe() reports a deadline for a background context")
	}

	want := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), want)
	defer cancel()
	if err := f.Probe(ctx); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := f.Probes(); got != 2 {
		t.Errorf("probes = %d, want 2", got)
	}
	deadline, bounded := f.LastProbe()
	if !bounded {
		t.Fatal("LastProbe() reports no deadline for a bounded context")
	}
	if !deadline.Equal(want) {
		t.Errorf("LastProbe() deadline = %v, want %v", deadline, want)
	}
}

func TestFake_EnsureContainerCreatesOnce(t *testing.T) {
	f := storagetest.NewFake(storagetest.WithoutContainer())
	ctx := context.Background()

	ops := []struct {
		name string
		call func() error
	}{
		{"Put", func() error {
			_, err := f.Put(ctx, "k", strings.NewReader("v"), storage.PutOptions{})
			return err
		}},
		{"Get", func() error {
			_, err := f.Get(ctx, "k", storage.GetOptions{})
			return err
		}},
		{"Stat", func() error {
			_, err := f.Stat(ctx, "k")
			return err
		}},
		{"Delete", func() error { return f.Delete(ctx, "k") }},
		{"List", func() error {
			_, err := f.List(ctx, storage.ListOptions{})
			return err
		}},
		{"Probe", func() error { return f.Probe(ctx) }},
	}

	for _, op := range ops {
		err := op.call()
		if !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("%s without a container = %v, want ErrNotFound", op.name, err)
		}
		if !errors.Is(err, storagetest.ErrNoSuchContainer) {
			t.Errorf("%s without a container = %v, want the cause to stay matchable", op.name, err)
		}
	}
	if got := f.Ensures(); got != 0 {
		t.Fatalf("ensures before any EnsureContainer = %d, want 0", got)
	}
	if _, bounded := f.LastEnsure(); bounded {
		t.Error("LastEnsure() reports a deadline before any EnsureContainer")
	}

	want := time.Now().Add(time.Minute)
	bounded, cancel := context.WithDeadline(ctx, want)
	defer cancel()
	for i := range 2 {
		if err := f.EnsureContainer(bounded); err != nil {
			t.Fatalf("EnsureContainer call %d: %v", i+1, err)
		}
	}
	if got := f.Ensures(); got != 2 {
		t.Errorf("ensures = %d, want 2", got)
	}
	if deadline, bounded := f.LastEnsure(); !bounded || !deadline.Equal(want) {
		t.Errorf("LastEnsure() = %v, %t; want %v, true", deadline, bounded, want)
	}
	// Put runs first, so every later operation finds the key or, for
	// Delete, removes it.
	for _, op := range ops {
		if err := op.call(); err != nil {
			t.Errorf("%s after EnsureContainer = %v, want nil", op.name, err)
		}
	}

	// The container is created empty and a repeat call leaves it as it is.
	putString(t, f, "kept", "v")
	if err := f.EnsureContainer(ctx); err != nil {
		t.Fatalf("EnsureContainer over an existing container: %v", err)
	}
	if _, err := f.Stat(ctx, "kept"); err != nil {
		t.Errorf("Stat after a repeat EnsureContainer = %v, want the object kept", err)
	}
}

func TestFake_Capabilities(t *testing.T) {
	got := storagetest.NewFake().Capabilities()
	if got.MaxKeyLength != storagetest.DefaultMaxKeyLength {
		t.Errorf("MaxKeyLength of a plain fake = %d, want DefaultMaxKeyLength %d", got.MaxKeyLength, storagetest.DefaultMaxKeyLength)
	}
	if got.ValidateKey == nil {
		t.Fatal("ValidateKey of a plain fake = nil, want the default")
	}
	if err := got.ValidateKey("a/b.txt"); err != nil {
		t.Errorf("ValidateKey(a/b.txt) = %v, want nil", err)
	}
	if err := got.ValidateKey(""); err == nil {
		t.Error("ValidateKey of an empty key = nil, want an error")
	}
	// The limit counts runes, so a multi-byte key at the limit passes.
	if err := got.ValidateKey(strings.Repeat("\u00e9", storagetest.DefaultMaxKeyLength)); err != nil {
		t.Errorf("ValidateKey of a key at the limit = %v, want nil", err)
	}
	if err := got.ValidateKey(strings.Repeat("a", storagetest.DefaultMaxKeyLength+1)); err == nil {
		t.Error("ValidateKey of a key past the limit = nil, want an error")
	}

	f := storagetest.NewFake(storagetest.WithCapabilities(storage.Capabilities{
		MaxKeyLength: 42,
		ValidateKey:  func(string) error { return errors.New("no keys accepted") },
	}))
	got = f.Capabilities()
	if got.MaxKeyLength != 42 {
		t.Errorf("MaxKeyLength = %d, want 42", got.MaxKeyLength)
	}
	if got.ValidateKey == nil || got.ValidateKey("any") == nil {
		t.Error("ValidateKey did not pass through the configured function")
	}
}

func TestFake_DropContainer(t *testing.T) {
	f := storagetest.NewFake()
	ctx := context.Background()
	putString(t, f, "k", "v")
	if !f.HasContainer() {
		t.Fatal("HasContainer() = false for a plain fake, want true")
	}

	f.DropContainer()
	if f.HasContainer() {
		t.Error("HasContainer() = true after DropContainer, want false")
	}
	if err := f.Probe(ctx); !errors.Is(err, storage.ErrNotFound) || !errors.Is(err, storagetest.ErrNoSuchContainer) {
		t.Errorf("Probe after DropContainer = %v, want ErrNotFound wrapping ErrNoSuchContainer", err)
	}

	// The container comes back empty: the drop took its objects with it.
	if err := f.EnsureContainer(ctx); err != nil {
		t.Fatalf("EnsureContainer after DropContainer: %v", err)
	}
	if !f.HasContainer() {
		t.Error("HasContainer() = false after EnsureContainer, want true")
	}
	if _, err := f.Stat(ctx, "k"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat after the container was dropped and recreated = %v, want ErrNotFound", err)
	}
	if keys, _ := listKeys(t, f, "", 0); len(keys) != 0 {
		t.Errorf("List after the container was recreated = %v, want no keys", keys)
	}
}

func TestFake_ConcurrentPutGet(t *testing.T) {
	f := storagetest.NewFake()
	ctx := context.Background()
	const workers = 8
	const rounds = 50

	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("worker-%d", w)
			for i := range rounds {
				content := fmt.Sprintf("%d:%d", w, i)
				if _, err := f.Put(ctx, key, strings.NewReader(content), storage.PutOptions{}); err != nil {
					t.Errorf("Put(%s): %v", key, err)
					return
				}
				blob, err := f.Get(ctx, key, storage.GetOptions{})
				if err != nil {
					t.Errorf("Get(%s): %v", key, err)
					return
				}
				// The body is read inline because t.Fatal is not allowed
				// off the test goroutine.
				data, err := io.ReadAll(blob.Body)
				_ = blob.Body.Close()
				if err != nil {
					t.Errorf("read %s: %v", key, err)
					return
				}
				if string(data) != content {
					t.Errorf("Get(%s) = %q, want %q", key, data, content)
					return
				}
				if _, err := f.List(ctx, storage.ListOptions{Prefix: "worker-"}); err != nil {
					t.Errorf("List: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	if keys, _ := listKeys(t, f, "", 0); len(keys) != workers {
		t.Errorf("List returned %d keys, want %d", len(keys), workers)
	}
}
