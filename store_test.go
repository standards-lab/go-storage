package storage_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"
)

var (
	_ storage.Client         = (*storage.Store)(nil)
	_ lifecycle.Subsystem    = (*storage.Store)(nil)
	_ lifecycle.ReadyStarter = (*storage.Store)(nil)
)

func TestStore_CoordinatorChecksItUnderItsNodeName(t *testing.T) {
	s := storage.New(storagetest.NewFake(storagetest.Options{}), finalizedConfig(t, 0, 0))
	g := graph.New()
	node := g.Value("storage", s)
	sys, err := g.Build(node)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	checks := lifecycle.New(sys, lifecycle.Config{ShutdownTimeout: config.Duration(time.Second)}).Checks()
	if len(checks) != 1 {
		t.Fatalf("Checks: got %d, want 1: %+v", len(checks), checks)
	}
	if checks[0].Name != "storage" {
		t.Errorf("Check name: got %q, want %q", checks[0].Name, "storage")
	}
	if checks[0].Checker != s {
		t.Errorf("Check checker: got %v, want the Store", checks[0].Checker)
	}
}

const testTimeout = 5 * time.Second

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

func wantNotReady(t *testing.T, s *storage.Store, f *storagetest.Fake) {
	t.Helper()
	puts, probes := f.Puts(), f.Probes()
	for _, op := range storeOps(s) {
		if err := op.call(); !errors.Is(err, storage.ErrNotReady) {
			t.Errorf("%s = %v, want ErrNotReady", op.name, err)
		} else if err.Error() != storage.ErrNotReady.Error() {
			t.Errorf("%s = %v, want the plain sentinel with nothing wrapped around it", op.name, err)
		}
	}
	if got := f.Puts(); got != puts {
		t.Errorf("puts = %d after not-ready calls, want %d", got, puts)
	}
	if got := f.Probes(); got != probes {
		t.Errorf("probes = %d after not-ready calls, want %d", got, probes)
	}
}

func wantProbeDeadline(t *testing.T, f *storagetest.Fake) {
	t.Helper()
	deadline, bounded := f.LastProbe()
	if !bounded {
		t.Fatal("LastProbe() reports no deadline, want one within the configured RequestTimeout")
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
		storage.New(storagetest.NewFake(storagetest.Options{}), validConfig())
	})
}

func TestNew_PerformsNoIO(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{})
	f.SetDown(true)

	s := storage.New(f, finalizedConfig(t, 0, 0))

	if got := f.Probes(); got != 0 {
		t.Errorf("probes after New = %d, want 0", got)
	}
	if s.Ready() {
		t.Error("Ready() = true before Start, want false")
	}
}

func TestStore_NotReadyBeforeStart(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{Capabilities: storage.Capabilities{MaxKeyLength: 17}})
	s := storage.New(f, finalizedConfig(t, 0, 0))

	wantNotReady(t, s, f)
	if s.Ready() {
		t.Error("Ready() = true before Start, want false")
	}
	if got := f.Puts(); got != 0 {
		t.Errorf("puts = %d, want 0", got)
	}
	if got := f.Probes(); got != 0 {
		t.Errorf("probes = %d, want 0", got)
	}
	if got := s.Capabilities().MaxKeyLength; got != 17 {
		t.Errorf("Capabilities().MaxKeyLength = %d before Start, want 17", got)
	}
}

func TestStore_StartEnsuresThenProbesAndReports(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{})
	s := storage.New(f, finalizedConfig(t, 0, 0))

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := f.Ensures(); got != 1 {
		t.Errorf("ensures after Start = %d, want 1", got)
	}
	if got := f.Probes(); got != 1 {
		t.Errorf("probes after Start = %d, want 1", got)
	}
	wantProbeDeadline(t, f)
	ensureDeadline, bounded := f.LastEnsure()
	if !bounded {
		t.Fatal("LastEnsure() reports no deadline, want the one Start built")
	}
	if probeDeadline, _ := f.LastProbe(); !ensureDeadline.Equal(probeDeadline) {
		t.Errorf("ensure deadline %v differs from probe deadline %v, want the same context", ensureDeadline, probeDeadline)
	}
	if !s.Ready() {
		t.Error("Ready() = false after a successful Start, want true")
	}
}

func TestStore_StartCreatesMissingContainer(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{NoContainer: true})
	s := storage.New(f, finalizedConfig(t, 0, 0))

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start against an empty fake: %v", err)
	}
	if got := f.Ensures(); got != 1 {
		t.Errorf("ensures after Start = %d, want 1", got)
	}
	if got := f.Probes(); got != 1 {
		t.Errorf("probes after Start = %d, want 1", got)
	}
	if !f.HasContainer() {
		t.Error("the container does not exist after Start, want it created")
	}
	if !s.Ready() {
		t.Error("Ready() = false after Start created the container, want true")
	}
	putString(t, s, "k", "v")
}

func TestStore_StartFailureWrapsSentinelAndCause(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{})
	f.SetDown(true)
	s := storage.New(f, finalizedConfig(t, 0, 0))

	err := s.Start(context.Background())
	if err == nil {
		t.Fatal("Start succeeded against a fake in outage")
	}
	if !errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("errors.Is(err, ErrUnavailable) = false for %v", err)
	}
	if !errors.Is(err, storagetest.ErrDown) {
		t.Errorf("errors.Is(err, storagetest.ErrDown) = false for %v, want the cause to stay matchable", err)
	}
	if got := strings.Count(err.Error(), storage.ErrUnavailable.Error()); got != 1 {
		t.Errorf("error %q names ErrUnavailable %d times, want once", err, got)
	}
	if got := f.Ensures(); got != 1 {
		t.Errorf("ensures after a failed Start = %d, want 1", got)
	}
	if got := f.Probes(); got != 0 {
		t.Errorf("probes after a failed ensure = %d, want 0", got)
	}
	if s.Ready() {
		t.Error("Ready() = true after a failed Start, want false")
	}
	wantNotReady(t, s, f)
}

func TestStore_StartWrapsUnclassifiedEnsureError(t *testing.T) {
	cause := errors.New("raw ensure failure")
	f := &ensureErrClient{Fake: storagetest.NewFake(storagetest.Options{}), err: cause}
	s := storage.New(f, finalizedConfig(t, 0, 0))

	err := s.Start(context.Background())
	if !errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("errors.Is(err, ErrUnavailable) = false for %v", err)
	}
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is(err, cause) = false for %v", err)
	}
	if got := f.Probes(); got != 0 {
		t.Errorf("probes after a failed ensure = %d, want 0", got)
	}
	if s.Ready() {
		t.Error("Ready() = true after a failed Start, want false")
	}
	wantNotReady(t, s, f.Fake)
}

func TestStore_StartProbeFailureAfterEnsure(t *testing.T) {
	cause := errors.New("raw probe failure")
	tests := []struct {
		name string
		err  error
	}{
		{"unclassified", cause},
		{"classified", fmt.Errorf("%w: %w", storage.ErrUnavailable, cause)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &probeErrClient{Fake: storagetest.NewFake(storagetest.Options{}), err: tc.err}
			s := storage.New(f, finalizedConfig(t, 0, 0))

			err := s.Start(context.Background())
			if !errors.Is(err, storage.ErrUnavailable) {
				t.Errorf("errors.Is(err, ErrUnavailable) = false for %v", err)
			}
			if !errors.Is(err, cause) {
				t.Errorf("errors.Is(err, cause) = false for %v", err)
			}
			if got := strings.Count(err.Error(), storage.ErrUnavailable.Error()); got != 1 {
				t.Errorf("error %q names ErrUnavailable %d times, want once", err, got)
			}
			if got := f.Ensures(); got != 1 {
				t.Errorf("ensures = %d, want 1", got)
			}
			if s.Ready() {
				t.Error("Ready() = true after a failed Start, want false")
			}
			wantNotReady(t, s, f.Fake)
		})
	}
}

type probeErrClient struct {
	*storagetest.Fake
	err error
}

func (c *probeErrClient) Probe(context.Context) error { return c.err }

type ensureErrClient struct {
	*storagetest.Fake
	err error
}

func (c *ensureErrClient) EnsureContainer(context.Context) error { return c.err }

func TestStore_EnsureContainerNeedsNoReadiness(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{NoContainer: true})
	s := storage.New(f, finalizedConfig(t, 0, 0))
	ctx := context.Background()

	if err := s.EnsureContainer(ctx); err != nil {
		t.Fatalf("EnsureContainer before Start: %v", err)
	}
	if got := f.Ensures(); got != 1 {
		t.Errorf("ensures = %d, want 1", got)
	}
	if _, bounded := f.LastEnsure(); bounded {
		t.Error("EnsureContainer added a deadline to a background context, want the caller's context passed through")
	}
	if !f.HasContainer() {
		t.Error("the container does not exist after EnsureContainer, want it created")
	}
	wantNotReady(t, s, f)

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := f.Ensures(); got != 2 {
		t.Errorf("ensures after Start = %d, want 2", got)
	}

	f.DropContainer()
	if s.Ready() {
		t.Error("Ready() = true with the container gone, want false")
	}
	if got := f.Ensures(); got != 2 {
		t.Errorf("ensures after Ready = %d, want 2 (Ready does not create)", got)
	}
	if err := s.EnsureContainer(ctx); err != nil {
		t.Fatalf("EnsureContainer while not ready: %v", err)
	}
	if got := f.Ensures(); got != 3 {
		t.Errorf("ensures = %d, want 3", got)
	}
	if !s.Ready() {
		t.Error("Ready() = false after EnsureContainer recovered the container, want true")
	}

	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := s.EnsureContainer(ctx); err != nil {
		t.Errorf("EnsureContainer after Shutdown = %v, want nil", err)
	}
	if got := f.Ensures(); got != 4 {
		t.Errorf("ensures = %d, want 4", got)
	}
	wantNotReady(t, s, f)
}

func TestStore_EnsureContainerReturnsProviderError(t *testing.T) {
	cause := errors.New("provider refused to create the container")
	f := &ensureErrClient{Fake: storagetest.NewFake(storagetest.Options{}), err: cause}
	s := storage.New(f, finalizedConfig(t, 0, 0))

	if err := s.EnsureContainer(context.Background()); err != cause {
		t.Errorf("EnsureContainer = %v, want the provider's error unchanged", err)
	}

	down := storagetest.NewFake(storagetest.Options{})
	down.SetDown(true)
	err := storage.New(down, finalizedConfig(t, 0, 0)).EnsureContainer(context.Background())
	if !errors.Is(err, storage.ErrUnavailable) || !errors.Is(err, storagetest.ErrDown) {
		t.Errorf("EnsureContainer during an outage = %v, want the fake's classified error", err)
	}
	if got := strings.Count(err.Error(), storage.ErrUnavailable.Error()); got != 1 {
		t.Errorf("error %q names ErrUnavailable %d times, want once", err, got)
	}
}

func TestStore_Container(t *testing.T) {
	cfg := finalizedConfig(t, 0, 0)
	s := storage.New(storagetest.NewFake(storagetest.Options{}), cfg)

	if got := s.Container(); got != cfg.Container {
		t.Errorf("Container() = %q, want the configured %q", got, cfg.Container)
	}
}

func TestStore_ReadySelfHeals(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{})
	s := startedStore(t, f, 0, 0)

	f.SetDown(true)
	if s.Ready() {
		t.Error("Ready() = true during an outage, want false")
	}

	f.SetDown(false)
	if !s.Ready() {
		t.Error("Ready() = false after the outage cleared, want true")
	}
	wantProbeDeadline(t, f)
	if got := f.Probes(); got != 3 {
		t.Errorf("probes = %d, want 3 (Start and two Ready calls)", got)
	}
}

func TestStore_ProbeUsesCallerContext(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{})
	s := startedStore(t, f, 0, 0)

	if err := s.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if _, bounded := f.LastProbe(); bounded {
		t.Error("Probe added a deadline to a background context, want the caller's context passed through")
	}

	f.SetDown(true)
	if err := s.Probe(context.Background()); !errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("Probe during an outage = %v, want ErrUnavailable", err)
	}
}

func TestStore_ShutdownClearsReadiness(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{})
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
		s := storage.New(storagetest.NewFake(storagetest.Options{}), finalizedConfig(t, 0, 0))
		if err := s.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown before Start = %v, want nil", err)
		}
	})

	t.Run("after a failed Start", func(t *testing.T) {
		f := storagetest.NewFake(storagetest.Options{})
		f.SetDown(true)
		s := storage.New(f, finalizedConfig(t, 0, 0))
		if err := s.Start(context.Background()); err == nil {
			t.Fatal("Start succeeded against a fake in outage")
		}
		if err := s.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown after a failed Start = %v, want nil", err)
		}
	})

	t.Run("twice", func(t *testing.T) {
		s := startedStore(t, storagetest.NewFake(storagetest.Options{}), 0, 0)
		for i := range 2 {
			if err := s.Shutdown(context.Background()); err != nil {
				t.Errorf("Shutdown call %d = %v, want nil", i+1, err)
			}
		}
	})
}

func TestStore_StartAfterShutdownIsNotReady(t *testing.T) {
	c := newClosingFake(nil)
	s := startedStore(t, c, 0, 0)
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	ensures, probes := c.Ensures(), c.Probes()

	if err := s.Start(context.Background()); !errors.Is(err, storage.ErrNotReady) {
		t.Errorf("Start after Shutdown = %v, want ErrNotReady", err)
	}
	if c.Ensures() != ensures || c.Probes() != probes {
		t.Errorf("Start after Shutdown reached the client: ensures %d->%d, probes %d->%d", ensures, c.Ensures(), probes, c.Probes())
	}
	if s.Ready() {
		t.Error("Ready() = true after Start following Shutdown, want false")
	}
	wantNotReady(t, s, c.Fake)
}

func TestStore_ShutdownDuringStartProbe(t *testing.T) {
	f := &blockingProbeClient{Fake: storagetest.NewFake(storagetest.Options{}), entered: make(chan struct{}), release: make(chan struct{})}
	s := storage.New(f, finalizedConfig(t, 0, 0))

	started := make(chan error, 1)
	go func() { started <- s.Start(context.Background()) }()
	<-f.entered
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	close(f.release)

	if err := <-started; !errors.Is(err, storage.ErrNotReady) {
		t.Errorf("Start whose probe outlived a Shutdown = %v, want ErrNotReady", err)
	}
	if s.Ready() {
		t.Error("Ready() = true after Shutdown landed during Start, want false")
	}
	wantNotReady(t, s, f.Fake)
}

func TestStore_ShutdownDuringStartProbeThatFails(t *testing.T) {
	closed := errors.New("transport closed")
	f := &blockingProbeClient{Fake: storagetest.NewFake(storagetest.Options{}), entered: make(chan struct{}), release: make(chan struct{}), err: closed}
	s := storage.New(f, finalizedConfig(t, 0, 0))

	started := make(chan error, 1)
	go func() { started <- s.Start(context.Background()) }()
	<-f.entered
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	close(f.release)

	err := <-started
	if !errors.Is(err, storage.ErrNotReady) {
		t.Errorf("Start whose probe failed after a Shutdown = %v, want ErrNotReady", err)
	}
	if errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("Start whose probe failed after a Shutdown = %v, want it not to match ErrUnavailable", err)
	}
	wantNotReady(t, s, f.Fake)
}

func TestStore_ShutdownDuringStartEnsure(t *testing.T) {
	f := &blockingEnsureClient{Fake: storagetest.NewFake(storagetest.Options{}), entered: make(chan struct{}), release: make(chan struct{})}
	s := storage.New(f, finalizedConfig(t, 0, 0))

	started := make(chan error, 1)
	go func() { started <- s.Start(context.Background()) }()
	<-f.entered
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	close(f.release)

	if err := <-started; !errors.Is(err, storage.ErrNotReady) {
		t.Errorf("Start whose container check outlived a Shutdown = %v, want ErrNotReady", err)
	}
	if got := f.Probes(); got != 0 {
		t.Errorf("probes = %d, want 0: Start probed a client Shutdown had closed", got)
	}
	wantNotReady(t, s, f.Fake)
}

type blockingProbeClient struct {
	*storagetest.Fake
	entered chan struct{}
	release chan struct{}
	err     error
}

func (c *blockingProbeClient) Probe(ctx context.Context) error {
	close(c.entered)
	<-c.release
	if c.err != nil {
		return c.err
	}
	return c.Fake.Probe(ctx)
}

type blockingEnsureClient struct {
	*storagetest.Fake
	entered chan struct{}
	release chan struct{}
}

func (c *blockingEnsureClient) EnsureContainer(ctx context.Context) error {
	close(c.entered)
	<-c.release
	return c.Fake.EnsureContainer(ctx)
}

func TestStore_StartReturnsCallerCancellation(t *testing.T) {
	f := &ctxEnsureClient{Fake: storagetest.NewFake(storagetest.Options{})}
	s := storage.New(f, finalizedConfig(t, 0, 0))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.Start(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Start under a cancelled context = %v, want context.Canceled", err)
	}
	if errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("Start under a cancelled context = %v, want it not to match ErrUnavailable", err)
	}
	if s.Ready() {
		t.Error("Ready() = true after a cancelled Start, want false")
	}
}

func TestStore_StartCallerDeadlineOutranksProviderClassification(t *testing.T) {
	f := &failingEnsureClient{Fake: storagetest.NewFake(storagetest.Options{}), err: fmt.Errorf("%w: request timed out", storage.ErrUnavailable)}
	s := storage.New(f, finalizedConfig(t, 0, 0))

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	err := s.Start(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Start past the caller's deadline = %v, want context.DeadlineExceeded", err)
	}
	if errors.Is(err, storage.ErrUnavailable) {
		t.Errorf("Start past the caller's deadline = %v, want it not to match ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), "request timed out") {
		t.Errorf("error %q drops the provider's text", err)
	}
}

type failingEnsureClient struct {
	*storagetest.Fake
	err error
}

func (c *failingEnsureClient) EnsureContainer(context.Context) error {
	return c.err
}

type ctxEnsureClient struct {
	*storagetest.Fake
}

func (c *ctxEnsureClient) EnsureContainer(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.Fake.EnsureContainer(ctx)
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
	s := startedStore(t, storagetest.NewFake(storagetest.Options{}), 0, 0)

	if err := s.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown = %v, want nil for a Client with no Close", err)
	}
}

func TestStore_DelegatesRoundTrip(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{})
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
	if gotOpts, consumed := f.LastPut(); gotOpts != opts || consumed != 5 {
		t.Errorf("LastPut() = %+v, %d; want %+v, 5", gotOpts, consumed, opts)
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
		wantMismatch bool
		wantPuts     int64
		wantConsumed int64
		wantStored   string
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
			wantMismatch: true,
			wantPuts:     1,
			wantConsumed: 2,
		},
		{
			name:         "a declared size at the bound reports a longer body as too large",
			maxSize:      bound,
			body:         large,
			size:         bound,
			wantTooLarge: true,
			wantPuts:     1,
			wantConsumed: bound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := storagetest.NewFake(storagetest.Options{})
			s := startedStore(t, f, tc.maxSize, 0)
			ctx := context.Background()

			_, err := s.Put(ctx, "k", strings.NewReader(tc.body), storage.PutOptions{Size: tc.size})
			switch {
			case tc.wantTooLarge:
				if !errors.Is(err, storage.ErrTooLarge) {
					t.Errorf("Put = %v, want ErrTooLarge", err)
				}
				if err != nil && !strings.Contains(err.Error(), fmt.Sprint(bound)) {
					t.Errorf("error %q does not name the bound %d", err, bound)
				}
			case tc.wantMismatch:
				if err == nil || errors.Is(err, storage.ErrTooLarge) {
					t.Errorf("Put = %v, want a size mismatch that is not ErrTooLarge", err)
				}
				if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("declared size (%d bytes)", tc.size)) {
					t.Errorf("error %q does not name the declared size %d", err, tc.size)
				}
			case err != nil:
				t.Fatalf("Put: %v", err)
			}

			if got := f.Puts(); got != tc.wantPuts {
				t.Errorf("puts = %d, want %d", got, tc.wantPuts)
			}
			if _, consumed := f.LastPut(); tc.wantConsumed >= 0 && consumed > tc.wantConsumed+1 {
				t.Errorf("fake consumed %d bytes, want at most %d", consumed, tc.wantConsumed+1)
			}
			if tc.wantConsumed >= 0 && !tc.wantTooLarge && !tc.wantMismatch {
				if _, consumed := f.LastPut(); consumed != tc.wantConsumed {
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
			f := storagetest.NewFake(storagetest.Options{})
			s := startedStore(t, f, bound, 0)
			ctx := context.Background()

			exact := strings.Repeat("e", bound)
			if _, err := s.Put(ctx, "exact", shape.wrap(strings.NewReader(exact)), storage.PutOptions{}); err != nil {
				t.Fatalf("Put at the bound: %v", err)
			}
			if _, consumed := f.LastPut(); consumed != bound {
				t.Errorf("fake consumed %d bytes at the bound, want %d", consumed, bound)
			}

			over := strings.Repeat("o", bound+1)
			_, err := s.Put(ctx, "over", shape.wrap(strings.NewReader(over)), storage.PutOptions{})
			if !errors.Is(err, storage.ErrTooLarge) {
				t.Errorf("Put one byte over the bound = %v, want ErrTooLarge", err)
			}
			if _, consumed := f.LastPut(); consumed > bound {
				t.Errorf("fake consumed %d bytes over the bound, want at most %d", consumed, bound)
			}
			if _, err := s.Stat(ctx, "over"); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("Stat after a rejected Put = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestStore_PutBoundKeepsClientCause(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{})
	s := startedStore(t, f, 4, 0)

	_, err := s.Put(context.Background(), "k", strings.NewReader("0123456789"), storage.PutOptions{})
	if !errors.Is(err, storage.ErrTooLarge) {
		t.Fatalf("Put = %v, want ErrTooLarge", err)
	}
	want := "storage object too large: fake put: body exceeds the configured max object size (4 bytes)"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestStore_PutBoundNamesBoundWhenClientErrorOmitsIt(t *testing.T) {
	cause := errors.New("provider closed the connection")
	f := &lenientPutClient{Fake: storagetest.NewFake(storagetest.Options{}), err: cause}
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
	s := startedStore(t, storagetest.NewFake(storagetest.Options{}), 4, 0)

	_, err := s.Put(context.Background(), "k", strings.NewReader("x"), storage.PutOptions{Size: 9})
	want := "storage object too large: declared size 9 exceeds the configured max object size (4 bytes)"
	if err == nil || err.Error() != want {
		t.Errorf("error = %v, want %q", err, want)
	}
}

func TestStore_PutBoundDoesNotReclassifyClientFailure(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{})
	s := startedStore(t, f, 16, 0)
	cause := errors.New("provider rejected the write")
	f.FailPut(cause)

	_, err := s.Put(context.Background(), "k", strings.NewReader("within bound"), storage.PutOptions{})
	if !errors.Is(err, cause) {
		t.Fatalf("Put = %v, want the client's failure", err)
	}
	if errors.Is(err, storage.ErrTooLarge) {
		t.Errorf("Put = %v matches ErrTooLarge for a within-bound body", err)
	}
}

func TestStore_PutBoundTripsEvenWhenClientSucceeds(t *testing.T) {
	f := &lenientPutClient{Fake: storagetest.NewFake(storagetest.Options{})}
	s := startedStore(t, f, 4, 0)

	_, err := s.Put(context.Background(), "k", strings.NewReader("0123456789"), storage.PutOptions{})
	if !errors.Is(err, storage.ErrTooLarge) {
		t.Errorf("Put = %v, want ErrTooLarge even though the client returned nil", err)
	}
	if got := f.Puts(); got != 1 {
		t.Errorf("puts = %d, want 1", got)
	}
}

type lenientPutClient struct {
	*storagetest.Fake
	err error
}

func (c *lenientPutClient) Put(ctx context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, body)
	if c.err != nil {
		return storage.Object{}, c.err
	}
	opts.Size = 0
	return c.Fake.Put(ctx, key, &buf, opts)
}

func TestStore_PutDeclaredSize(t *testing.T) {
	body := "0123456789"
	tests := []struct {
		name      string
		maxSize   int64
		size      int64
		wantShort bool
		wantLong  bool
	}{
		{"exact size with no bound", 0, 10, false, false},
		{"exact size within the bound", 64, 10, false, false},
		{"short body with no bound", 0, 12, true, false},
		{"short body within the bound", 64, 12, true, false},
		{"long body with no bound", 0, 4, false, true},
		{"long body within the bound", 64, 4, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := storagetest.NewFake(storagetest.Options{})
			s := startedStore(t, f, tc.maxSize, 0)
			ctx := context.Background()

			_, err := s.Put(ctx, "k", strings.NewReader(body), storage.PutOptions{Size: tc.size})
			_, consumed := f.LastPut()
			switch {
			case tc.wantShort:
				if !errors.Is(err, io.ErrUnexpectedEOF) {
					t.Errorf("Put = %v, want io.ErrUnexpectedEOF wrapped", err)
				}
				want := fmt.Sprintf("body ended after %d bytes, short of the declared size (%d bytes)", len(body), tc.size)
				if err != nil && !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			case tc.wantLong:
				if err == nil || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, storage.ErrTooLarge) {
					t.Errorf("Put = %v, want a plain error for a body past its declared size", err)
				}
				want := fmt.Sprintf("body is longer than the declared size (%d bytes)", tc.size)
				if err != nil && !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
				if consumed > tc.size {
					t.Errorf("fake consumed %d bytes, want at most the declared %d", consumed, tc.size)
				}
			default:
				if err != nil {
					t.Fatalf("Put: %v", err)
				}
				if consumed != tc.size {
					t.Errorf("fake consumed %d bytes, want %d", consumed, tc.size)
				}
			}
			if errors.Is(err, storage.ErrTooLarge) {
				t.Errorf("Put = %v matches ErrTooLarge for a body within the bound", err)
			}

			_, statErr := s.Stat(ctx, "k")
			if tc.wantShort || tc.wantLong {
				if !errors.Is(statErr, storage.ErrNotFound) {
					t.Errorf("Stat after a rejected Put = %v, want ErrNotFound", statErr)
				}
			} else if statErr != nil {
				t.Errorf("Stat after an accepted Put = %v, want nil", statErr)
			}
		})
	}
}

func TestStore_PutDeclaredSizeReaderShapes(t *testing.T) {
	const size = 8
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
			f := storagetest.NewFake(storagetest.Options{})
			s := startedStore(t, f, 0, 0)
			ctx := context.Background()

			exact := strings.Repeat("e", size)
			if _, err := s.Put(ctx, "exact", shape.wrap(strings.NewReader(exact)), storage.PutOptions{Size: size}); err != nil {
				t.Fatalf("Put of the declared size: %v", err)
			}
			if _, consumed := f.LastPut(); consumed != size {
				t.Errorf("fake consumed %d bytes, want %d", consumed, size)
			}

			short := strings.Repeat("s", size-1)
			if _, err := s.Put(ctx, "short", shape.wrap(strings.NewReader(short)), storage.PutOptions{Size: size}); !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("Put one byte short of the declared size = %v, want io.ErrUnexpectedEOF", err)
			}
			if _, err := s.Stat(ctx, "short"); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("Stat after a short Put = %v, want ErrNotFound", err)
			}

			over := strings.Repeat("o", size+1)
			if _, err := s.Put(ctx, "over", shape.wrap(strings.NewReader(over)), storage.PutOptions{Size: size}); err == nil {
				t.Error("Put one byte over the declared size = nil, want an error")
			}
			if _, consumed := f.LastPut(); consumed > size {
				t.Errorf("fake consumed %d bytes over the declared size, want at most %d", consumed, size)
			}
			if _, err := s.Stat(ctx, "over"); !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("Stat after an over-long Put = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestStore_PutDeclaredSizeTripsEvenWhenClientSucceeds(t *testing.T) {
	f := &lenientPutClient{Fake: storagetest.NewFake(storagetest.Options{})}
	s := startedStore(t, f, 0, 0)
	ctx := context.Background()

	_, err := s.Put(ctx, "short", strings.NewReader("abc"), storage.PutOptions{Size: 5})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("Put of a short body = %v, want io.ErrUnexpectedEOF even though the client returned nil", err)
	}
	_, err = s.Put(ctx, "long", strings.NewReader("abcdef"), storage.PutOptions{Size: 5})
	if err == nil || !strings.Contains(err.Error(), "longer than the declared size (5 bytes)") {
		t.Errorf("Put of a long body = %v, want the size error even though the client returned nil", err)
	}
	if got := f.Puts(); got != 2 {
		t.Errorf("puts = %d, want 2", got)
	}
}

func TestStore_PutDeclaredSizeKeepsClientCause(t *testing.T) {
	cause := errors.New("provider closed the connection")
	f := &lenientPutClient{Fake: storagetest.NewFake(storagetest.Options{}), err: cause}
	s := startedStore(t, f, 0, 0)

	_, err := s.Put(context.Background(), "k", strings.NewReader("abc"), storage.PutOptions{Size: 5})
	if !errors.Is(err, io.ErrUnexpectedEOF) || !errors.Is(err, cause) {
		t.Fatalf("Put = %v, want io.ErrUnexpectedEOF with the client's cause", err)
	}
	if !strings.Contains(err.Error(), "(5 bytes)") {
		t.Errorf("error %q does not name the declared size", err)
	}
}

func TestStore_PutDeclaredSizeLeavesExistingObject(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{})
	s := startedStore(t, f, 0, 0)
	ctx := context.Background()
	before := putString(t, s, "k", "the original")

	if _, err := s.Put(ctx, "k", strings.NewReader("replacement"), storage.PutOptions{Size: 3}); err == nil {
		t.Fatal("Put with a Size shorter than the body = nil, want an error")
	}
	after, err := s.Stat(ctx, "k")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if after != before {
		t.Errorf("Stat after a rejected replace = %+v, want the original %+v", after, before)
	}
}

func TestStore_PutRejectsNegativeSize(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{})
	s := startedStore(t, f, 0, 0)

	_, err := s.Put(context.Background(), "k", strings.NewReader("v"), storage.PutOptions{Size: -1})
	if err == nil || !strings.Contains(err.Error(), "-1") {
		t.Errorf("Put with Size -1 = %v, want an error naming the size", err)
	}
	if got := f.Puts(); got != 0 {
		t.Errorf("puts = %d, want 0: the provider never sees a negative Size", got)
	}
}

func TestStore_PutValidatesKey(t *testing.T) {
	rejected := errors.New("key rejected")
	f := storagetest.NewFake(storagetest.Options{Capabilities: storage.Capabilities{
		MaxKeyLength: 8,
		ValidateKey: func(key string) error {
			if strings.HasPrefix(key, "/") {
				return rejected
			}
			return nil
		},
	}})
	s := startedStore(t, f, 0, 0)

	if _, err := s.Put(context.Background(), "/abs", strings.NewReader("v"), storage.PutOptions{}); !errors.Is(err, rejected) {
		t.Errorf("Put of a rejected key = %v, want the ValidateKey error", err)
	}
	if got := f.Puts(); got != 0 {
		t.Errorf("puts = %d, want 0: a rejected key never reaches the provider", got)
	}
	putString(t, s, "rel", "v")
}

func TestStore_PutNilValidateKeyAcceptsEveryKey(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{Capabilities: storage.Capabilities{MaxKeyLength: 1024}})
	s := startedStore(t, f, 0, 0)

	for _, key := range []string{"", "/abs", "a//b", strings.Repeat("k", 2048)} {
		if _, err := s.Put(context.Background(), key, strings.NewReader("v"), storage.PutOptions{}); err != nil {
			t.Errorf("Put(%q) with a nil ValidateKey = %v, want nil", key, err)
		}
	}
	if got := f.Puts(); got != 4 {
		t.Errorf("puts = %d, want 4: a nil ValidateKey passes every key to the provider", got)
	}
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
			f := storagetest.NewFake(storagetest.Options{})
			s := startedStore(t, f, 0, tc.pageSize)

			if _, err := s.List(context.Background(), storage.ListOptions{Prefix: "p/", Limit: tc.limit}); err != nil {
				t.Fatalf("List: %v", err)
			}
			got := f.LastList()
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
	f := storagetest.NewFake(storagetest.Options{})
	s := startedStore(t, f, 0, 25)

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
	if got := f.LastList(); got.Prefix != "marker" {
		t.Errorf("client saw a List with Prefix %q after the rejected call, want none", got.Prefix)
	}
}

func TestStore_CapabilitiesPassThrough(t *testing.T) {
	f := storagetest.NewFake(storagetest.Options{Capabilities: storage.Capabilities{
		MaxKeyLength: 1024,
		ValidateKey: func(key string) error {
			if strings.HasPrefix(key, "/") {
				return errors.New("leading slash")
			}
			return nil
		},
	}})
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
	f := storagetest.NewFake(storagetest.Options{})
	s := startedStore(t, f, 64, 10)
	ctx := context.Background()
	const workers = 8
	const rounds = 30

	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			key := fmt.Sprintf("worker-%d", w)
			for i := range rounds {
				content := fmt.Sprintf("%d:%d", w, i)
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
		})
	}
	wg.Go(func() {
		if err := s.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	wg.Wait()

	if s.Ready() {
		t.Error("Ready() = true after Shutdown, want false")
	}
}

type closingFake struct {
	*storagetest.Fake

	closes   atomic.Int64
	closeErr error
}

func newClosingFake(closeErr error) *closingFake {
	return &closingFake{Fake: storagetest.NewFake(storagetest.Options{}), closeErr: closeErr}
}

func (c *closingFake) Close() error {
	c.closes.Add(1)
	return c.closeErr
}

func putString(t *testing.T, c storage.Client, key, content string) storage.Object {
	t.Helper()
	obj, err := c.Put(context.Background(), key, strings.NewReader(content), storage.PutOptions{})
	if err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
	return obj
}

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
