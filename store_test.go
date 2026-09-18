package storage_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-storage"
)

var (
	_ storage.Client             = (*storage.Store)(nil)
	_ lifecycle.ReadinessChecker = (*storage.Store)(nil)
)

// testTimeout is the RequestTimeout every finalized test Config carries, so
// the probe-deadline assertions have a known bound to check against.
const testTimeout = 5 * time.Second

// finalizedConfig returns a valid finalized Config with the given limits.
func finalizedConfig(t *testing.T, maxObjectSize int64, listPageSize int) storage.Config {
	t.Helper()
	cfg := validConfig()
	cfg.MaxObjectSize = maxObjectSize
	cfg.ListPageSize = listPageSize
	cfg.RequestTimeout = new(config.Duration(testTimeout))
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("finalize config: %v", err)
	}
	return cfg
}

// startedStore wraps f in a Store built with the given limits and starts it.
func startedStore(t *testing.T, f storage.Client, maxObjectSize int64, listPageSize int) *storage.Store {
	t.Helper()
	s := storage.New(f, finalizedConfig(t, maxObjectSize, listPageSize))
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return s
}

func wantPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("no panic, want one containing %q", want)
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, want) {
			t.Errorf("panic = %v, want it to contain %q", r, want)
		}
	}()
	fn()
}

// storeOps lists every readiness-guarded operation on s as a named call, so
// the not-ready tests run each one the same way.
func storeOps(s *storage.Store) []struct {
	name string
	call func() error
} {
	ctx := context.Background()
	return []struct {
		name string
		call func() error
	}{
		{"Put", func() error {
			_, err := s.Put(ctx, "k", strings.NewReader("v"), storage.PutOptions{})
			return err
		}},
		{"Get", func() error {
			_, err := s.Get(ctx, "k", storage.GetOptions{})
			return err
		}},
		{"Stat", func() error {
			_, err := s.Stat(ctx, "k")
			return err
		}},
		{"Delete", func() error { return s.Delete(ctx, "k") }},
		{"List", func() error {
			_, err := s.List(ctx, storage.ListOptions{})
			return err
		}},
		{"Probe", func() error { return s.Probe(ctx) }},
	}
}

// wantNotReady asserts every guarded operation returns the plain ErrNotReady
// sentinel and that the fake saw no Put or Probe as a result.
func wantNotReady(t *testing.T, s *storage.Store, f *fake) {
	t.Helper()
	puts, probes := f.puts.Load(), f.probes.Load()
	for _, op := range storeOps(s) {
		if err := op.call(); !errors.Is(err, storage.ErrNotReady) {
			t.Errorf("%s = %v, want ErrNotReady", op.name, err)
		} else if err.Error() != storage.ErrNotReady.Error() {
			t.Errorf("%s = %v, want the plain sentinel with nothing wrapped around it", op.name, err)
		}
	}
	if got := f.puts.Load(); got != puts {
		t.Errorf("puts = %d after not-ready calls, want %d", got, puts)
	}
	if got := f.probes.Load(); got != probes {
		t.Errorf("probes = %d after not-ready calls, want %d", got, probes)
	}
}

// wantProbeDeadline asserts the fake's most recent Probe carried a deadline
// no further out than testTimeout from now and not already spent.
func wantProbeDeadline(t *testing.T, f *fake) {
	t.Helper()
	deadline, bounded := f.lastProbe()
	if !bounded {
		t.Fatal("lastProbe() reports no deadline, want one within the configured RequestTimeout")
	}
	remaining := time.Until(deadline)
	if remaining > testTimeout || remaining < testTimeout/2 {
		t.Errorf("probe deadline is %v away, want within (%v, %v]", remaining, testTimeout/2, testTimeout)
	}
}

func TestNew_PanicsOnNilClient(t *testing.T) {
	wantPanic(t, "storage: nil client", func() {
		storage.New(nil, finalizedConfig(t, 0, 0))
	})
}

func TestNew_PanicsOnUnfinalizedConfig(t *testing.T) {
	wantPanic(t, "storage: Config not finalized: call Finalize before New", func() {
		storage.New(newFake(), validConfig())
	})
}

func TestNew_PerformsNoIO(t *testing.T) {
	f := newFake()
	f.down.Store(true)

	s := storage.New(f, finalizedConfig(t, 0, 0))

	if got := f.probes.Load(); got != 0 {
		t.Errorf("probes after New = %d, want 0", got)
	}
	if s.Ready() {
		t.Error("Ready() = true before Start, want false")
	}
}

func TestStore_LifecycleServiceShape(t *testing.T) {
	f := newFake()
	s := storage.New(f, finalizedConfig(t, 0, 0))

	// The bare method values and the Store itself fill a lifecycle.Service
	// without an adapter; driving the service through its fields proves the
	// wiring the composition root relies on.
	svc := lifecycle.Service{Name: "storage", Stage: 0, Start: s.Start, Shutdown: s.Shutdown, Check: s}

	if svc.Check.Ready() {
		t.Error("Check.Ready() = true before Start, want false")
	}
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !svc.Check.Ready() {
		t.Error("Check.Ready() = false after Start, want true")
	}
	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if svc.Check.Ready() {
		t.Error("Check.Ready() = true after Shutdown, want false")
	}
}

func TestStore_NotReadyBeforeStart(t *testing.T) {
	f := newFake(withCapabilities(storage.Capabilities{MaxKeyLength: 17}))
	s := storage.New(f, finalizedConfig(t, 0, 0))

	wantNotReady(t, s, f)
	if s.Ready() {
		t.Error("Ready() = true before Start, want false")
	}
	if got := f.puts.Load(); got != 0 {
		t.Errorf("puts = %d, want 0", got)
	}
	if got := f.probes.Load(); got != 0 {
		t.Errorf("probes = %d, want 0", got)
	}
	// Capabilities is a static provider fact and needs no readiness.
	if got := s.Capabilities().MaxKeyLength; got != 17 {
		t.Errorf("Capabilities().MaxKeyLength = %d before Start, want 17", got)
	}
}

func TestStore_StartProbesAndReports(t *testing.T) {
	f := newFake()
	s := storage.New(f, finalizedConfig(t, 0, 0))

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := f.probes.Load(); got != 1 {
		t.Errorf("probes after Start = %d, want 1", got)
	}
	wantProbeDeadline(t, f)
	if !s.Ready() {
		t.Error("Ready() = false after a successful Start, want true")
	}
}

func TestStore_StartFailureWrapsSentinelAndCause(t *testing.T) {
	f := newFake()
	f.down.Store(true)
	s := storage.New(f, finalizedConfig(t, 0, 0))

	err := s.Start(context.Background())
	if err == nil {
		t.Fatal("Start succeeded against a fake in outage")
	}
	if !errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("errors.Is(err, ErrUnavailable) = false for %v", err)
	}
	if !errors.Is(err, errFakeDown) {
		t.Errorf("errors.Is(err, errFakeDown) = false for %v, want the cause to stay matchable", err)
	}
	// The fake already classifies its outage under ErrUnavailable, so Start
	// must not stack a second copy of the sentinel onto the message.
	if got := strings.Count(err.Error(), storage.ErrUnavailable.Error()); got != 1 {
		t.Errorf("error %q names ErrUnavailable %d times, want once", err, got)
	}
	if s.Ready() {
		t.Error("Ready() = true after a failed Start, want false")
	}
	wantNotReady(t, s, f)
}

func TestStore_StartWrapsUnclassifiedProbeError(t *testing.T) {
	cause := errors.New("raw probe failure")
	f := &probeErrClient{fake: newFake(), err: cause}
	s := storage.New(f, finalizedConfig(t, 0, 0))

	err := s.Start(context.Background())
	if !errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("errors.Is(err, ErrUnavailable) = false for %v", err)
	}
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is(err, cause) = false for %v", err)
	}
	if s.Ready() {
		t.Error("Ready() = true after a failed Start, want false")
	}
}

// probeErrClient is a fake whose Probe fails with an error that is not
// classified under ErrUnavailable, so the Start wrap path can be exercised.
type probeErrClient struct {
	*fake
	err error
}

func (c *probeErrClient) Probe(context.Context) error { return c.err }

func TestStore_ReadySelfHeals(t *testing.T) {
	f := newFake()
	s := startedStore(t, f, 0, 0)

	f.down.Store(true)
	if s.Ready() {
		t.Error("Ready() = true during an outage, want false")
	}

	f.down.Store(false)
	if !s.Ready() {
		t.Error("Ready() = false after the outage cleared, want true")
	}
	wantProbeDeadline(t, f)
	// Start probed once; each Ready call is a live probe of its own.
	if got := f.probes.Load(); got != 3 {
		t.Errorf("probes = %d, want 3 (Start and two Ready calls)", got)
	}
}

func TestStore_ProbeUsesCallerContext(t *testing.T) {
	f := newFake()
	s := startedStore(t, f, 0, 0)

	if err := s.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if _, bounded := f.lastProbe(); bounded {
		t.Error("Probe added a deadline to a background context, want the caller's context passed through")
	}

	f.down.Store(true)
	if err := s.Probe(context.Background()); !errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("Probe during an outage = %v, want ErrUnavailable", err)
	}
}

func TestStore_ShutdownClearsReadiness(t *testing.T) {
	f := newFake()
	s := startedStore(t, f, 0, 0)

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if s.Ready() {
		t.Error("Ready() = true after Shutdown, want false")
	}
	wantNotReady(t, s, f)
}

func TestStore_ShutdownIsSafeAtEveryPoint(t *testing.T) {
	t.Run("before Start", func(t *testing.T) {
		s := storage.New(newFake(), finalizedConfig(t, 0, 0))
		if err := s.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown before Start = %v, want nil", err)
		}
	})

	t.Run("after a failed Start", func(t *testing.T) {
		f := newFake()
		f.down.Store(true)
		s := storage.New(f, finalizedConfig(t, 0, 0))
		if err := s.Start(context.Background()); err == nil {
			t.Fatal("Start succeeded against a fake in outage")
		}
		if err := s.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown after a failed Start = %v, want nil", err)
		}
	})

	t.Run("twice", func(t *testing.T) {
		s := startedStore(t, newFake(), 0, 0)
		for i := range 2 {
			if err := s.Shutdown(context.Background()); err != nil {
				t.Errorf("Shutdown call %d = %v, want nil", i+1, err)
			}
		}
	})
}

func TestStore_ShutdownClosesClientOnce(t *testing.T) {
	cause := errors.New("close failed")
	c := newClosingFake(cause)
	s := startedStore(t, c, 0, 0)

	if err := s.Shutdown(context.Background()); !errors.Is(err, cause) {
		t.Errorf("first Shutdown = %v, want the Close error", err)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("second Shutdown = %v, want nil", err)
	}
	if got := c.closes.Load(); got != 1 {
		t.Errorf("closes = %d across two Shutdown calls, want 1", got)
	}
}

func TestStore_ShutdownWithoutCloser(t *testing.T) {
	s := startedStore(t, newFake(), 0, 0)

	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown = %v, want nil for a Client with no Close", err)
	}
}

func TestStore_DelegatesRoundTrip(t *testing.T) {
	f := newFake()
	s := startedStore(t, f, 0, 0)
	ctx := context.Background()

	opts := storage.PutOptions{ContentType: "text/plain", Size: 5}
	obj, err := s.Put(ctx, "a/hello.txt", strings.NewReader("hello"), opts)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if obj.Key != "a/hello.txt" || obj.Size != 5 || obj.ContentType != "text/plain" {
		t.Errorf("Put = %+v, want the fake's metadata for a/hello.txt", obj)
	}
	if gotOpts, consumed := f.lastPut(); gotOpts != opts || consumed != 5 {
		t.Errorf("lastPut() = %+v, %d; want %+v, 5", gotOpts, consumed, opts)
	}

	blob, err := s.Get(ctx, "a/hello.txt", storage.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := readBlob(t, blob); got != "hello" {
		t.Errorf("Get body = %q, want hello", got)
	}
	if blob.Object != obj {
		t.Errorf("Get metadata = %+v, want %+v", blob.Object, obj)
	}

	stat, err := s.Stat(ctx, "a/hello.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat != obj {
		t.Errorf("Stat = %+v, want %+v", stat, obj)
	}

	putString(t, s, "b/other.txt", "other")
	keys, _ := listKeys(t, s, "a/", 0)
	if strings.Join(keys, ",") != "a/hello.txt" {
		t.Errorf("List(a/) = %v, want [a/hello.txt]", keys)
	}

	if err := s.Delete(ctx, "a/hello.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Stat(ctx, "a/hello.txt"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat after Delete = %v, want ErrNotFound", err)
	}
	if _, err := s.Get(ctx, "a/hello.txt", storage.GetOptions{}); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
}

func TestStore_PutBound(t *testing.T) {
	const bound = 8
	large := strings.Repeat("x", 4*bound)

	tests := []struct {
		name         string
		maxSize      int64
		body         string
		size         int64
		wantTooLarge bool
		// wantPuts is the number of Put calls the fake should have seen.
		wantPuts int64
		// wantConsumed is the fake's consumed byte count when a Put reached
		// it; -1 skips the check.
		wantConsumed int64
		// wantStored is the content Stat and Get should find afterward;
		// empty means the key must be absent.
		wantStored string
	}{
		{
			name:         "unbounded accepts a large body",
			maxSize:      0,
			body:         large,
			wantPuts:     1,
			wantConsumed: int64(len(large)),
			wantStored:   large,
		},
		{
			name:         "known size over the bound is rejected before the client",
			maxSize:      bound,
			body:         large,
			size:         int64(len(large)),
			wantTooLarge: true,
			wantPuts:     0,
			wantConsumed: -1,
		},
		{
			name:         "known size equal to the bound is accepted",
			maxSize:      bound,
			body:         strings.Repeat("y", bound),
			size:         bound,
			wantPuts:     1,
			wantConsumed: bound,
			wantStored:   strings.Repeat("y", bound),
		},
		{
			name:         "unknown size exactly at the bound is accepted",
			maxSize:      bound,
			body:         strings.Repeat("z", bound),
			wantPuts:     1,
			wantConsumed: bound,
			wantStored:   strings.Repeat("z", bound),
		},
		{
			name:         "unknown size over the bound is rejected",
			maxSize:      bound,
			body:         large,
			wantTooLarge: true,
			wantPuts:     1,
			wantConsumed: bound,
		},
		{
			name:         "a small declared size does not exempt a long body",
			maxSize:      bound,
			body:         large,
			size:         2,
			wantTooLarge: true,
			wantPuts:     1,
			wantConsumed: bound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			s := startedStore(t, f, tc.maxSize, 0)
			ctx := context.Background()

			_, err := s.Put(ctx, "k", strings.NewReader(tc.body), storage.PutOptions{Size: tc.size})
			if tc.wantTooLarge {
				if !errors.Is(err, storage.ErrTooLarge) {
					t.Errorf("Put = %v, want ErrTooLarge", err)
				}
				if err != nil && !strings.Contains(err.Error(), fmt.Sprint(bound)) {
					t.Errorf("error %q does not name the bound %d", err, bound)
				}
			} else if err != nil {
				t.Fatalf("Put: %v", err)
			}

			if got := f.puts.Load(); got != tc.wantPuts {
				t.Errorf("puts = %d, want %d", got, tc.wantPuts)
			}
			if _, consumed := f.lastPut(); tc.wantConsumed >= 0 && consumed > tc.wantConsumed+1 {
				t.Errorf("fake consumed %d bytes, want at most %d", consumed, tc.wantConsumed+1)
			}
			if tc.wantConsumed >= 0 && !tc.wantTooLarge {
				if _, consumed := f.lastPut(); consumed != tc.wantConsumed {
					t.Errorf("fake consumed %d bytes, want %d", consumed, tc.wantConsumed)
				}
			}

			stat, err := s.Stat(ctx, "k")
			if tc.wantStored == "" {
				if !errors.Is(err, storage.ErrNotFound) {
					t.Errorf("Stat after a rejected Put = (%+v, %v), want ErrNotFound", stat, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Stat: %v", err)
			}
			blob, err := s.Get(ctx, "k", storage.GetOptions{})
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got := readBlob(t, blob); got != tc.wantStored {
				t.Errorf("stored body has %d bytes, want %d", len(got), len(tc.wantStored))
			}
		})
	}
}

func TestStore_PutBoundReaderShapes(t *testing.T) {
	// The bound must hold whatever shape the body's reads take: a reader
	// that delivers its final bytes together with io.EOF, and one that
	// delivers a single byte per Read.
	const bound = 8
	shapes := []struct {
		name string
		wrap func(io.Reader) io.Reader
	}{
		{"data with EOF", iotest.DataErrReader},
		{"one byte per read", iotest.OneByteReader},
		{"half reads", iotest.HalfReader},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			f := newFake()
			s := startedStore(t, f, bound, 0)
			ctx := context.Background()

			exact := strings.Repeat("e", bound)
			if _, err := s.Put(ctx, "exact", shape.wrap(strings.NewReader(exact)), storage.PutOptions{}); err != nil {
				t.Fatalf("Put at the bound: %v", err)
			}
			if _, consumed := f.lastPut(); consumed != bound {
				t.Errorf("fake consumed %d bytes at the bound, want %d", consumed, bound)
			}

			over := strings.Repeat("o", bound+1)
			_, err := s.Put(ctx, "over", shape.wrap(strings.NewReader(over)), storage.PutOptions{})
			if !errors.Is(err, storage.ErrTooLarge) {
				t.Errorf("Put one byte over the bound = %v, want ErrTooLarge", err)
			}
			if _, consumed := f.lastPut(); consumed > bound {
				t.Errorf("fake consumed %d bytes over the bound, want at most %d", consumed, bound)
			}
			if _, err := s.Stat(ctx, "over"); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("Stat after a rejected Put = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestStore_PutBoundKeepsClientCause(t *testing.T) {
	f := newFake()
	s := startedStore(t, f, 4, 0)

	_, err := s.Put(context.Background(), "k", strings.NewReader("0123456789"), storage.PutOptions{})
	if !errors.Is(err, storage.ErrTooLarge) {
		t.Fatalf("Put = %v, want ErrTooLarge", err)
	}
	// The fake wraps the body's read error with its own prefix, and Put
	// wraps that under the sentinel, so the provider's message survives and
	// the bound appears once.
	want := "storage object too large: fake put: body exceeds the configured max object size (4 bytes)"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestStore_PutBoundNamesBoundWhenClientErrorOmitsIt(t *testing.T) {
	// A client that swallows the read error and then fails for another
	// reason still yields an error that names the bound and keeps the
	// client's cause matchable.
	cause := errors.New("provider closed the connection")
	f := &lenientPutClient{fake: newFake(), err: cause}
	s := startedStore(t, f, 4, 0)

	_, err := s.Put(context.Background(), "k", strings.NewReader("0123456789"), storage.PutOptions{})
	if !errors.Is(err, storage.ErrTooLarge) || !errors.Is(err, cause) {
		t.Fatalf("Put = %v, want ErrTooLarge with the client's cause", err)
	}
	if !strings.Contains(err.Error(), "(4 bytes)") {
		t.Errorf("error %q does not name the bound", err)
	}
}

func TestStore_PutDeclaredSizeErrorNamesBound(t *testing.T) {
	s := startedStore(t, newFake(), 4, 0)

	_, err := s.Put(context.Background(), "k", strings.NewReader("x"), storage.PutOptions{Size: 9})
	want := "storage object too large: declared size 9 exceeds the configured max object size (4 bytes)"
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestStore_PutBoundDoesNotReclassifyClientFailure(t *testing.T) {
	f := newFake()
	s := startedStore(t, f, 16, 0)
	cause := errors.New("provider rejected the write")
	f.failPut(cause)

	_, err := s.Put(context.Background(), "k", strings.NewReader("within bound"), storage.PutOptions{})
	if !errors.Is(err, cause) {
		t.Fatalf("Put = %v, want the client's failure", err)
	}
	if errors.Is(err, storage.ErrTooLarge) {
		t.Errorf("Put = %v matches ErrTooLarge for a within-bound body", err)
	}
}

func TestStore_PutBoundTripsEvenWhenClientSucceeds(t *testing.T) {
	// A client that swallows the read error and reports success has stored a
	// truncated object; the Store must still fail the Put.
	f := &lenientPutClient{fake: newFake()}
	s := startedStore(t, f, 4, 0)

	_, err := s.Put(context.Background(), "k", strings.NewReader("0123456789"), storage.PutOptions{})
	if !errors.Is(err, storage.ErrTooLarge) {
		t.Errorf("Put = %v, want ErrTooLarge even though the client returned nil", err)
	}
	if got := f.puts.Load(); got != 1 {
		t.Errorf("puts = %d, want 1", got)
	}
}

// lenientPutClient is a fake whose Put reads what it can and ignores a read
// error. It then reports success for whatever it got, or err when one is set.
type lenientPutClient struct {
	*fake
	err error
}

func (c *lenientPutClient) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, body)
	if c.err != nil {
		return storage.Object{}, c.err
	}
	return c.fake.Put(ctx, key, &buf, opts)
}

func TestStore_ListLimit(t *testing.T) {
	tests := []struct {
		name      string
		pageSize  int
		limit     int
		wantLimit int
	}{
		{"zero limit takes the configured page size", 25, 0, 25},
		{"zero limit stays zero with no configured page size", 0, 0, 0},
		{"explicit limit below the page size passes through", 25, 3, 3},
		{"explicit limit above the page size passes through", 25, 100, 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			s := startedStore(t, f, 0, tc.pageSize)

			if _, err := s.List(context.Background(), storage.ListOptions{Prefix: "p/", Limit: tc.limit}); err != nil {
				t.Fatalf("List: %v", err)
			}
			got := f.lastList()
			if got.Limit != tc.wantLimit {
				t.Errorf("client saw Limit %d, want %d", got.Limit, tc.wantLimit)
			}
			if got.Prefix != "p/" {
				t.Errorf("client saw Prefix %q, want p/", got.Prefix)
			}
		})
	}
}

func TestStore_ListRejectsNegativeLimit(t *testing.T) {
	f := newFake()
	s := startedStore(t, f, 0, 25)

	// A first valid call leaves a marker, so a rejected call is provably one
	// the fake never saw.
	if _, err := s.List(context.Background(), storage.ListOptions{Prefix: "marker"}); err != nil {
		t.Fatalf("List: %v", err)
	}
	_, err := s.List(context.Background(), storage.ListOptions{Prefix: "bad", Limit: -1})
	if err == nil {
		t.Fatal("List accepted a negative Limit")
	}
	if !strings.Contains(err.Error(), "-1") {
		t.Errorf("error %q does not name the limit", err)
	}
	if got := f.lastList(); got.Prefix != "marker" {
		t.Errorf("client saw a List with Prefix %q after the rejected call, want none", got.Prefix)
	}
}

func TestStore_CapabilitiesPassThrough(t *testing.T) {
	f := newFake(withCapabilities(storage.Capabilities{
		MaxKeyLength: 1024,
		ValidateKey: func(key string) error {
			if strings.HasPrefix(key, "/") {
				return errors.New("leading slash")
			}
			return nil
		},
	}))
	s := storage.New(f, finalizedConfig(t, 0, 0))

	got := s.Capabilities()
	if got.MaxKeyLength != 1024 {
		t.Errorf("MaxKeyLength = %d, want 1024", got.MaxKeyLength)
	}
	if got.ValidateKey == nil {
		t.Fatal("ValidateKey = nil, want the fake's function")
	}
	if err := got.ValidateKey("/abs"); err == nil {
		t.Error("ValidateKey(/abs) = nil, want the fake's rejection")
	}
	if err := got.ValidateKey("rel"); err != nil {
		t.Errorf("ValidateKey(rel) = %v, want nil", err)
	}
}

func TestStore_ConcurrentUse(t *testing.T) {
	f := newFake()
	s := startedStore(t, f, 64, 10)
	ctx := context.Background()
	const workers = 8
	const rounds = 30

	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("worker-%d", w)
			for i := range rounds {
				content := fmt.Sprintf("%d:%d", w, i)
				// Shutdown races these calls, so ErrNotReady is an accepted
				// outcome; any other error is a failure.
				_, err := s.Put(ctx, key, strings.NewReader(content), storage.PutOptions{})
				if err != nil && !errors.Is(err, storage.ErrNotReady) {
					t.Errorf("Put(%s): %v", key, err)
					return
				}
				blob, err := s.Get(ctx, key, storage.GetOptions{})
				if err == nil {
					_, _ = io.Copy(io.Discard, blob.Body)
					_ = blob.Body.Close()
				} else if !errors.Is(err, storage.ErrNotReady) && !errors.Is(err, storage.ErrNotFound) {
					t.Errorf("Get(%s): %v", key, err)
					return
				}
				_ = s.Ready()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	}()
	wg.Wait()

	if s.Ready() {
		t.Error("Ready() = true after Shutdown, want false")
	}
}
