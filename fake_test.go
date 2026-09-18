package storage_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/standards-lab/go-storage"
)

var (
	_ storage.Client = (*fake)(nil)
	_ storage.Client = (*closingFake)(nil)
	_ io.Closer      = (*closingFake)(nil)
)

// errFakeDown is the cause the fake wraps under ErrUnavailable while its
// outage toggle is set.
var errFakeDown = errors.New("fake: connection refused")

// errFakeMissing is the cause the fake wraps under ErrNotFound for a key it
// does not hold.
var errFakeMissing = errors.New("fake: no such key")

// errFakeNoContainer is the cause the fake wraps under ErrNotFound while its
// container does not exist.
var errFakeNoContainer = errors.New("fake: no such container")

// fake is an in-memory storage.Client. It honors the Client contract so the
// Store tests can wrap it, and it records what it received so those tests can
// assert what Store passed through. Its outage toggle makes every method,
// EnsureContainer and Probe included, fail with ErrUnavailable until the
// toggle is cleared. Its container toggle models whether the container
// exists: while it does not, Probe and every object operation fail with
// ErrNotFound, and EnsureContainer creates it.
type fake struct {
	// down is the outage toggle. Every method consults it on entry.
	down atomic.Bool

	// hasContainer is the container toggle. It starts true; withoutContainer
	// clears it, and EnsureContainer sets it.
	hasContainer atomic.Bool

	// puts counts Put calls, whether or not they succeeded.
	puts atomic.Int64

	// ensures counts EnsureContainer calls, whether or not they succeeded.
	ensures atomic.Int64

	// probes counts Probe calls, whether or not they succeeded.
	probes atomic.Int64

	now      func() time.Time
	pageSize int
	caps     storage.Capabilities

	mu      sync.Mutex
	objects map[string]fakeObject

	// putErr, when set, fails every Put after the body has been fully read.
	putErr error

	lastPutOpts     storage.PutOptions
	lastPutConsumed int64

	lastEnsureDeadline time.Time
	lastEnsureBounded  bool

	lastProbeDeadline time.Time
	lastProbeBounded  bool

	lastListOpts storage.ListOptions
}

type fakeObject struct {
	meta storage.Object
	data []byte
}

type fakeOption func(*fake)

// withClock supplies the time stamped on every Put as ModifiedAt.
func withClock(now func() time.Time) fakeOption {
	return func(f *fake) { f.now = now }
}

// withPageSize sets the page size List uses when Limit is 0.
func withPageSize(n int) fakeOption {
	return func(f *fake) { f.pageSize = n }
}

// withCapabilities sets what Capabilities returns.
func withCapabilities(c storage.Capabilities) fakeOption {
	return func(f *fake) { f.caps = c }
}

// withoutContainer starts the fake with no container, so a test can watch
// EnsureContainer create it.
func withoutContainer() fakeOption {
	return func(f *fake) { f.hasContainer.Store(false) }
}

func newFake(opts ...fakeOption) *fake {
	f := &fake{
		now:      time.Now,
		pageSize: 3,
		objects:  make(map[string]fakeObject),
	}
	f.hasContainer.Store(true)
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// failPut sets the error every subsequent Put returns after it has read its
// body. A nil err restores normal Puts.
func (f *fake) failPut(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putErr = err
}

// lastPut reports the options of the most recent Put and how many body bytes
// it read.
func (f *fake) lastPut() (storage.PutOptions, int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastPutOpts, f.lastPutConsumed
}

// lastEnsure reports the deadline of the context the most recent
// EnsureContainer received, and whether that context carried one.
func (f *fake) lastEnsure() (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastEnsureDeadline, f.lastEnsureBounded
}

// lastProbe reports the deadline of the context the most recent Probe
// received, and whether that context carried one.
func (f *fake) lastProbe() (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastProbeDeadline, f.lastProbeBounded
}

// lastList reports the options of the most recent List call.
func (f *fake) lastList() storage.ListOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastListOpts
}

func (f *fake) unavailable() error {
	return fmt.Errorf("%w: %w", storage.ErrUnavailable, errFakeDown)
}

func (f *fake) noContainer() error {
	return fmt.Errorf("%w: %w", storage.ErrNotFound, errFakeNoContainer)
}

func (f *fake) Put(_ context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	f.puts.Add(1)
	if f.down.Load() {
		return storage.Object{}, f.unavailable()
	}
	if !f.hasContainer.Load() {
		return storage.Object{}, f.noContainer()
	}

	data, readErr := io.ReadAll(body)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastPutOpts = opts
	f.lastPutConsumed = int64(len(data))

	if readErr != nil {
		return storage.Object{}, fmt.Errorf("fake put: %w", readErr)
	}
	if f.putErr != nil {
		return storage.Object{}, fmt.Errorf("fake put: %w", f.putErr)
	}

	sum := sha256.Sum256(data)
	obj := fakeObject{
		meta: storage.Object{
			Key:         key,
			Size:        int64(len(data)),
			ContentType: opts.ContentType,
			ETag:        hex.EncodeToString(sum[:]),
			ModifiedAt:  f.now(),
		},
		data: data,
	}
	f.objects[key] = obj
	return obj.meta, nil
}

func (f *fake) Get(_ context.Context, key string, _ storage.GetOptions) (storage.Blob, error) {
	if f.down.Load() {
		return storage.Blob{}, f.unavailable()
	}
	if !f.hasContainer.Load() {
		return storage.Blob{}, f.noContainer()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	obj, ok := f.objects[key]
	if !ok {
		return storage.Blob{}, fmt.Errorf("%w: %w", storage.ErrNotFound, errFakeMissing)
	}
	return storage.Blob{
		Object: obj.meta,
		Body:   io.NopCloser(bytes.NewReader(bytes.Clone(obj.data))),
	}, nil
}

func (f *fake) Stat(_ context.Context, key string) (storage.Object, error) {
	if f.down.Load() {
		return storage.Object{}, f.unavailable()
	}
	if !f.hasContainer.Load() {
		return storage.Object{}, f.noContainer()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	obj, ok := f.objects[key]
	if !ok {
		return storage.Object{}, fmt.Errorf("%w: %w", storage.ErrNotFound, errFakeMissing)
	}
	return obj.meta, nil
}

func (f *fake) Delete(_ context.Context, key string) error {
	if f.down.Load() {
		return f.unavailable()
	}
	if !f.hasContainer.Load() {
		return f.noContainer()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	return nil
}

// List pages in key order. The continuation token is the last key of the
// previous page, so a page starts at the first key after it.
func (f *fake) List(_ context.Context, opts storage.ListOptions) (storage.Page, error) {
	if f.down.Load() {
		return storage.Page{}, f.unavailable()
	}
	if !f.hasContainer.Load() {
		return storage.Page{}, f.noContainer()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastListOpts = opts

	limit := opts.Limit
	if limit <= 0 {
		limit = f.pageSize
	}

	keys := make([]string, 0, len(f.objects))
	for key := range f.objects {
		if strings.HasPrefix(key, opts.Prefix) && key > opts.Token {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	var page storage.Page
	for i, key := range keys {
		if i == limit {
			page.Next = keys[i-1]
			break
		}
		page.Objects = append(page.Objects, f.objects[key].meta)
	}
	return page, nil
}

// EnsureContainer creates the container when it does not exist. It is
// idempotent: a container that already exists is left as it is.
func (f *fake) EnsureContainer(ctx context.Context) error {
	f.ensures.Add(1)

	deadline, bounded := ctx.Deadline()
	f.mu.Lock()
	f.lastEnsureDeadline = deadline
	f.lastEnsureBounded = bounded
	f.mu.Unlock()

	if f.down.Load() {
		return f.unavailable()
	}
	f.hasContainer.Store(true)
	return nil
}

func (f *fake) Probe(ctx context.Context) error {
	f.probes.Add(1)

	deadline, bounded := ctx.Deadline()
	f.mu.Lock()
	f.lastProbeDeadline = deadline
	f.lastProbeBounded = bounded
	f.mu.Unlock()

	if f.down.Load() {
		return f.unavailable()
	}
	if !f.hasContainer.Load() {
		return f.noContainer()
	}
	return nil
}

func (f *fake) Capabilities() storage.Capabilities {
	return f.caps
}

// closingFake is a fake that also implements io.Closer, for the Store
// Shutdown path that closes a Client when it can.
type closingFake struct {
	*fake

	closes   atomic.Int64
	closeErr error
}

func newClosingFake(closeErr error, opts ...fakeOption) *closingFake {
	return &closingFake{fake: newFake(opts...), closeErr: closeErr}
}

func (c *closingFake) Close() error {
	c.closes.Add(1)
	return c.closeErr
}

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
	f := newFake(withClock(func() time.Time { return stamp }))
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
	f := newFake()

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
	f := newFake()

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
	f := newFake()
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
			if !errors.Is(err, errFakeMissing) {
				t.Errorf("errors.Is(err, errFakeMissing) = false for %v, want the cause to stay matchable", err)
			}
		})
	}
}

func TestFake_DeleteIdempotent(t *testing.T) {
	f := newFake()
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
	f := newFake()
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
	f := newFake()
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
	f := newFake(withPageSize(2))
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
	if got := f.lastList(); got.Prefix != "k" || got.Limit != 7 || got.Token == "" {
		t.Errorf("lastList() = %+v, want the options of the final call", got)
	}
}

func TestFake_OutageToggle(t *testing.T) {
	f := newFake()
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

	f.down.Store(true)
	for _, op := range ops {
		err := op.call()
		if !errors.Is(err, storage.ErrUnavailable) {
			t.Errorf("%s during outage = %v, want ErrUnavailable", op.name, err)
		}
		if !errors.Is(err, errFakeDown) {
			t.Errorf("%s during outage = %v, want the cause to stay matchable", op.name, err)
		}
	}

	f.down.Store(false)
	for _, op := range ops {
		if err := op.call(); err != nil {
			t.Errorf("%s after the outage = %v, want nil", op.name, err)
		}
	}
}

func TestFake_PutBodyReadFails(t *testing.T) {
	f := newFake()
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
	f := newFake()

	if got := f.puts.Load(); got != 0 {
		t.Fatalf("puts before any Put = %d, want 0", got)
	}

	opts := storage.PutOptions{ContentType: "application/json", Size: 7}
	if _, err := f.Put(context.Background(), "k", strings.NewReader("1234567"), opts); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if got := f.puts.Load(); got != 1 {
		t.Errorf("puts = %d, want 1", got)
	}
	gotOpts, consumed := f.lastPut()
	if gotOpts != opts {
		t.Errorf("lastPut() opts = %+v, want %+v", gotOpts, opts)
	}
	if consumed != 7 {
		t.Errorf("lastPut() consumed = %d, want 7", consumed)
	}
}

func TestFake_PutInjectedFailure(t *testing.T) {
	f := newFake()
	cause := errors.New("provider rejected the write")
	f.failPut(cause)

	_, err := f.Put(context.Background(), "k", strings.NewReader("abc"), storage.PutOptions{})
	if !errors.Is(err, cause) {
		t.Fatalf("Put = %v, want the injected failure", err)
	}
	// The failure fires after the body has been read, which is when a real
	// provider learns the outcome of its request.
	if _, consumed := f.lastPut(); consumed != 3 {
		t.Errorf("consumed = %d, want the whole body read before the failure", consumed)
	}
	if _, err := f.Stat(context.Background(), "k"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat after the failed Put = %v, want ErrNotFound", err)
	}

	f.failPut(nil)
	putString(t, f, "k", "abc")
}

func TestFake_ProbeRecordsDeadline(t *testing.T) {
	f := newFake()

	if err := f.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := f.probes.Load(); got != 1 {
		t.Errorf("probes = %d, want 1", got)
	}
	if _, bounded := f.lastProbe(); bounded {
		t.Error("lastProbe() reports a deadline for a background context")
	}

	want := time.Now().Add(time.Minute)
	ctx, cancel := context.WithDeadline(context.Background(), want)
	defer cancel()
	if err := f.Probe(ctx); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := f.probes.Load(); got != 2 {
		t.Errorf("probes = %d, want 2", got)
	}
	deadline, bounded := f.lastProbe()
	if !bounded {
		t.Fatal("lastProbe() reports no deadline for a bounded context")
	}
	if !deadline.Equal(want) {
		t.Errorf("lastProbe() deadline = %v, want %v", deadline, want)
	}
}

func TestFake_EnsureContainerCreatesOnce(t *testing.T) {
	f := newFake(withoutContainer())
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
		if !errors.Is(err, errFakeNoContainer) {
			t.Errorf("%s without a container = %v, want the cause to stay matchable", op.name, err)
		}
	}
	if got := f.ensures.Load(); got != 0 {
		t.Fatalf("ensures before any EnsureContainer = %d, want 0", got)
	}
	if _, bounded := f.lastEnsure(); bounded {
		t.Error("lastEnsure() reports a deadline before any EnsureContainer")
	}

	want := time.Now().Add(time.Minute)
	bounded, cancel := context.WithDeadline(ctx, want)
	defer cancel()
	for i := range 2 {
		if err := f.EnsureContainer(bounded); err != nil {
			t.Fatalf("EnsureContainer call %d: %v", i+1, err)
		}
	}
	if got := f.ensures.Load(); got != 2 {
		t.Errorf("ensures = %d, want 2", got)
	}
	if deadline, bounded := f.lastEnsure(); !bounded || !deadline.Equal(want) {
		t.Errorf("lastEnsure() = %v, %t; want %v, true", deadline, bounded, want)
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
	if got := newFake().Capabilities(); got.MaxKeyLength != 0 || got.ValidateKey != nil {
		t.Errorf("Capabilities() of a plain fake = %+v, want the zero value", got)
	}

	f := newFake(withCapabilities(storage.Capabilities{
		MaxKeyLength: 42,
		ValidateKey:  func(string) error { return errors.New("no keys accepted") },
	}))
	got := f.Capabilities()
	if got.MaxKeyLength != 42 {
		t.Errorf("MaxKeyLength = %d, want 42", got.MaxKeyLength)
	}
	if got.ValidateKey == nil || got.ValidateKey("any") == nil {
		t.Error("ValidateKey did not pass through the configured function")
	}
}

func TestFake_ClosingFake(t *testing.T) {
	c := newClosingFake(nil, withPageSize(1))
	putString(t, c, "k", "v")

	if err := c.Close(); err != nil {
		t.Errorf("Close = %v, want nil", err)
	}
	if got := c.closes.Load(); got != 1 {
		t.Errorf("closes = %d, want 1", got)
	}
	if _, pages := listKeys(t, c, "", 0); pages != 1 {
		t.Errorf("pages = %d, want the embedded fake's options to apply", pages)
	}

	cause := errors.New("close failed")
	failing := newClosingFake(cause)
	if err := failing.Close(); !errors.Is(err, cause) {
		t.Errorf("Close = %v, want the configured error", err)
	}
}

func TestFake_ConcurrentPutGet(t *testing.T) {
	f := newFake()
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
