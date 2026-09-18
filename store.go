package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// Store wraps a provider's Client with lifecycle integration and the limits
// from Config. Construction performs no I/O, Start ensures the container
// exists and establishes connectivity, and Ready reports connectivity live.
// Start and Shutdown carry the lifecycle hook signature, so the composition
// root registers the bare method values, and Ready satisfies
// lifecycle.ReadinessChecker structurally; the package registers no hooks of
// its own.
//
// Store implements Client. Every object operation returns [ErrNotReady]
// before a successful Start or after Shutdown and otherwise delegates to the
// provider under the caller's context. Store applies no timeout of its own to
// an object operation; the caller's context and the provider's transport
// govern. The configured RequestTimeout bounds only the calls Start and Ready
// make on their own behalf.
//
// A Store is safe for concurrent use.
type Store struct {
	client         Client
	container      string
	maxObjectSize  int64
	listPageSize   int
	requestTimeout time.Duration
	started        atomic.Bool
	closed         atomic.Bool
}

// New wraps c with cfg's limits. It panics on a nil c or a cfg that was not
// finalized: each is a wiring defect at the composition root, not a runtime
// condition. New copies what it needs out of cfg and does not retain it.
func New(c Client, cfg Config) *Store {
	if c == nil {
		panic("storage: nil client")
	}
	if !cfg.finalized() {
		panic("storage: Config not finalized: call Finalize before New")
	}
	return &Store{
		client:         c,
		container:      cfg.Container,
		maxObjectSize:  cfg.MaxObjectSize,
		listPageSize:   cfg.ListPageSize,
		requestTimeout: cfg.RequestTimeout.Duration(),
	}
}

// Start ensures the configured container exists, then probes the provider,
// both under one context bounded by the configured RequestTimeout, and marks
// the store started. A failure of either step matches [ErrUnavailable] and
// keeps the provider's error matchable, and the store stays not started.
// There is no started guard: a repeat Start ensures and probes again.
func (s *Store) Start(ctx context.Context) error {
	startCtx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	if err := s.client.EnsureContainer(startCtx); err != nil {
		return unavailable(err)
	}
	if err := s.client.Probe(startCtx); err != nil {
		return unavailable(err)
	}
	s.started.Store(true)
	return nil
}

// unavailable classifies a Start failure under [ErrUnavailable]. An error the
// provider already classified passes through unchanged, so the sentinel is
// never stacked twice.
func unavailable(err error) error {
	if errors.Is(err, ErrUnavailable) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrUnavailable, err)
}

// Shutdown clears readiness and, when the Client implements io.Closer,
// closes it. The Client is closed at most once across every Shutdown call;
// the first call returns the Close error and later calls return nil.
// Shutdown is safe before Start and after a failed Start. The context exists
// for the lifecycle hook signature.
func (s *Store) Shutdown(ctx context.Context) error {
	s.started.Store(false)
	closer, ok := s.client.(io.Closer)
	if !ok {
		return nil
	}
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	return closer.Close()
}

// Ready reports live connectivity: false before Start or after Shutdown, and
// otherwise the result of a probe bounded by the configured RequestTimeout.
// Readiness drops during an outage and recovers when the provider does, at
// the cost of one bounded round trip per call.
func (s *Store) Ready() bool {
	if !s.started.Load() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.requestTimeout)
	defer cancel()
	return s.client.Probe(ctx) == nil
}

// Put writes body as the object at key after enforcing the configured
// MaxObjectSize and the declared opts.Size. A declared Size over the bound is
// rejected with [ErrTooLarge] before the provider sees the body. Otherwise
// the body is read through a bound, whether or not a size was declared,
// because a declared size is the caller's claim and the bound exists for
// untrusted bodies. A body that runs past the bound fails with [ErrTooLarge],
// wrapped around the provider's error when it returned one.
//
// A declared Size greater than 0 is enforced on the body too, whether or not
// a bound is configured: a body that ends short of Size fails with an error
// wrapping io.ErrUnexpectedEOF, and one that runs past Size fails on the
// first byte beyond it. Either error names the declared size and reaches the
// provider as a read failure before the body ends, so the provider commits
// nothing. The bound is the inner reader, so when both would trip on the
// same byte, which happens only when Size equals the bound, the body fails
// with [ErrTooLarge]. A body past a Size that is smaller than the bound fails
// on the size check first, since that check needs fewer bytes to decide.
//
// Store cannot undo a commit, so it relies on the provider's Put being all or
// nothing, which the storagetest suite proves for each provider. When the
// provider nonetheless reports success after the body failed one of these
// checks, Put returns the check's error rather than the Object.
func (s *Store) Put(ctx context.Context, key string, body io.Reader, opts PutOptions) (Object, error) {
	if !s.started.Load() {
		return Object{}, ErrNotReady
	}
	if s.maxObjectSize > 0 && opts.Size > s.maxObjectSize {
		return Object{}, fmt.Errorf("%w: declared size %d exceeds the configured max object size (%d bytes)", ErrTooLarge, opts.Size, s.maxObjectSize)
	}

	var bounded *boundedReader
	if s.maxObjectSize > 0 {
		bounded = newBoundedReader(body, s.maxObjectSize)
		body = bounded
	}
	var sized *sizedReader
	if opts.Size > 0 {
		sized = newSizedReader(body, opts.Size)
		body = sized
	}

	obj, err := s.client.Put(ctx, key, body, opts)
	if bounded != nil && bounded.tripped.Load() {
		return Object{}, fmt.Errorf("%w: %w", ErrTooLarge, readFailure(bounded.err, err))
	}
	if sized != nil && sized.tripped.Load() {
		return Object{}, readFailure(sized.err, err)
	}
	return obj, err
}

// readFailure combines the error a body wrapper recorded with what the
// provider returned: the recorded error alone when the provider reported
// success or already wrapped it, and both otherwise, so the provider's cause
// stays matchable.
func readFailure(recorded, provider error) error {
	switch {
	case provider == nil:
		return recorded
	case errors.Is(provider, recorded):
		return provider
	default:
		return fmt.Errorf("%w: %w", recorded, provider)
	}
}

// Get opens the object at key for reading. The caller closes the returned
// Blob's Body.
func (s *Store) Get(ctx context.Context, key string, opts GetOptions) (Blob, error) {
	if !s.started.Load() {
		return Blob{}, ErrNotReady
	}
	return s.client.Get(ctx, key, opts)
}

// Stat returns the metadata of the object at key without reading its
// content.
func (s *Store) Stat(ctx context.Context, key string) (Object, error) {
	if !s.started.Load() {
		return Object{}, ErrNotReady
	}
	return s.client.Stat(ctx, key)
}

// Delete removes the object at key. A missing key is a no-op success.
func (s *Store) Delete(ctx context.Context, key string) error {
	if !s.started.Load() {
		return ErrNotReady
	}
	return s.client.Delete(ctx, key)
}

// List returns one page of objects selected by opts. A Limit of 0 takes the
// configured ListPageSize when one is set; an explicit positive Limit passes
// through unchanged. A negative Limit is an error the provider never sees.
func (s *Store) List(ctx context.Context, opts ListOptions) (Page, error) {
	if !s.started.Load() {
		return Page{}, ErrNotReady
	}
	if opts.Limit < 0 {
		return Page{}, fmt.Errorf("invalid list limit: %d", opts.Limit)
	}
	if opts.Limit == 0 {
		opts.Limit = s.listPageSize
	}
	return s.client.List(ctx, opts)
}

// EnsureContainer creates the configured container and succeeds when it
// already exists, under the caller's context. It has no readiness check: a
// missing container is what it corrects, so it works before Start, after
// Shutdown, and while Ready reports false. The provider's error is returned
// unchanged.
func (s *Store) EnsureContainer(ctx context.Context) error {
	return s.client.EnsureContainer(ctx)
}

// Probe reports whether the provider is reachable, under the caller's
// context. It returns [ErrNotReady] before Start or after Shutdown.
func (s *Store) Probe(ctx context.Context) error {
	if !s.started.Load() {
		return ErrNotReady
	}
	return s.client.Probe(ctx)
}

// Capabilities returns the provider's key constraints. They are a static
// fact about the provider, so the call needs no readiness check.
func (s *Store) Capabilities() Capabilities {
	return s.client.Capabilities()
}

// Container returns the configured container name, copied from Config at
// New.
func (s *Store) Container() string {
	return s.container
}

// boundedReader lets exactly remain bytes through and fails on the first byte
// beyond them. It differs from io.LimitedReader, which reports io.EOF at the
// limit and so would make an oversize body look like a complete short one.
// err is the read error it returns once the body runs past the bound, and
// Put wraps it under [ErrTooLarge]. tripped is atomic so Put can read it after
// the provider returns even if the provider's reads ran on another goroutine.
type boundedReader struct {
	r       io.Reader
	remain  int64
	err     error
	tripped atomic.Bool
}

func newBoundedReader(r io.Reader, bound int64) *boundedReader {
	return &boundedReader{
		r:      r,
		remain: bound,
		err:    fmt.Errorf("body exceeds the configured max object size (%d bytes)", bound),
	}
}

func (b *boundedReader) Read(p []byte) (int, error) {
	if b.tripped.Load() {
		return 0, b.err
	}
	if b.remain <= 0 {
		// The bound is spent. One more byte from the source means the body
		// is oversize; none means it ended exactly at the bound.
		var probe [1]byte
		n, err := b.r.Read(probe[:])
		if n > 0 {
			b.tripped.Store(true)
			return 0, b.err
		}
		return 0, err
	}
	if int64(len(p)) > b.remain {
		p = p[:b.remain]
	}
	n, err := b.r.Read(p)
	b.remain -= int64(n)
	return n, err
}

// sizedReader holds a body to its declared size: it lets exactly size bytes
// through, fails on the first byte beyond them, and turns an io.EOF before
// size bytes into an error wrapping io.ErrUnexpectedEOF. Either failure is
// recorded in err and reported on every later Read, so a provider that reads
// again after the failure sees it again. tripped is atomic for the same
// reason as boundedReader's.
type sizedReader struct {
	r       io.Reader
	size    int64
	read    int64
	err     error
	tripped atomic.Bool
}

func newSizedReader(r io.Reader, size int64) *sizedReader {
	return &sizedReader{r: r, size: size}
}

func (s *sizedReader) Read(p []byte) (int, error) {
	if s.tripped.Load() {
		return 0, s.err
	}
	remain := s.size - s.read
	if remain <= 0 {
		// The declared size is spent. One more byte from the source means
		// the body is longer than declared; none means it ended on it.
		var probe [1]byte
		n, err := s.r.Read(probe[:])
		if n > 0 {
			return 0, s.trip(fmt.Errorf("body is longer than the declared size (%d bytes)", s.size))
		}
		return 0, err
	}
	if int64(len(p)) > remain {
		p = p[:remain]
	}
	n, err := s.r.Read(p)
	s.read += int64(n)
	if errors.Is(err, io.EOF) && s.read < s.size {
		return n, s.trip(fmt.Errorf("body ended after %d bytes, short of the declared size (%d bytes): %w", s.read, s.size, io.ErrUnexpectedEOF))
	}
	return n, err
}

// trip records err as the failure every later Read reports and returns it.
func (s *sizedReader) trip(err error) error {
	s.err = err
	s.tripped.Store(true)
	return err
}
