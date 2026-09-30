package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Store wraps a provider's [Client] with the lifecycle and the limits from
// [Config]; every object operation returns [ErrNotReady] outside a successful
// Start and Shutdown. A Store is safe for concurrent use.
type Store struct {
	client         Client
	container      string
	maxObjectSize  int64
	listPageSize   int
	requestTimeout time.Duration

	// started gates the object operations. mu serializes the transitions
	// Start and Shutdown make, and shut records that Shutdown has run.
	started atomic.Bool
	mu      sync.Mutex
	shut    bool
}

// New wraps c with cfg's limits. It performs no I/O, and it panics on a nil c
// or a cfg that was not finalized.
func New(c Client, cfg Config) *Store {
	if c == nil {
		panic("storage: nil client")
	}
	if !cfg.Finalized() {
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
// both under one context bounded by RequestTimeout, and marks the store
// started. A failure matches [ErrUnavailable] with the provider's error kept,
// except that the caller's own cancellation is returned as it is. Start after
// Shutdown, one that lands during Start's probe included, returns
// [ErrNotReady].
func (s *Store) Start(ctx context.Context) error {
	if s.isShut() {
		return ErrNotReady
	}
	startCtx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()
	if err := s.client.EnsureContainer(startCtx); err != nil {
		return startFailure(ctx, err)
	}
	if err := s.client.Probe(startCtx); err != nil {
		return startFailure(ctx, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shut {
		return ErrNotReady
	}
	s.started.Store(true)
	return nil
}

func (s *Store) isShut() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shut
}

// startFailure classifies a Start failure under [ErrUnavailable]. The
// caller's cancellation and an error the provider already classified pass
// through unchanged.
func startFailure(ctx context.Context, err error) error {
	if ctx.Err() != nil || errors.Is(err, ErrUnavailable) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrUnavailable, err)
}

// Shutdown clears readiness and, when the Client implements io.Closer,
// closes it. The first call returns the Close error and later calls return
// nil. Shutdown is safe before Start and after a failed Start.
func (s *Store) Shutdown(context.Context) error {
	s.mu.Lock()
	already := s.shut
	s.shut = true
	s.started.Store(false)
	s.mu.Unlock()

	if already {
		return nil
	}
	if closer, ok := s.client.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Ready reports live connectivity: false outside a successful Start and
// Shutdown, and otherwise the result of a probe bounded by RequestTimeout.
func (s *Store) Ready() bool {
	if !s.started.Load() {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.requestTimeout)
	defer cancel()
	return s.client.Probe(ctx) == nil
}

// Put writes body at key after checking key with the provider's ValidateKey.
// A declared opts.Size or a body over MaxObjectSize fails with [ErrTooLarge];
// a body shorter or longer than a declared Size fails with an error (short
// wraps io.ErrUnexpectedEOF). Both checks fail the body's read, so the
// provider commits nothing. A negative Size is an error the provider never
// sees.
func (s *Store) Put(ctx context.Context, key string, body io.Reader, opts PutOptions) (Object, error) {
	if !s.started.Load() {
		return Object{}, ErrNotReady
	}
	if validate := s.client.Capabilities().ValidateKey; validate != nil {
		if err := validate(key); err != nil {
			return Object{}, err
		}
	}
	if opts.Size < 0 {
		return Object{}, fmt.Errorf("invalid put size: %d", opts.Size)
	}
	if s.maxObjectSize > 0 && opts.Size > s.maxObjectSize {
		return Object{}, fmt.Errorf("%w: declared size %d exceeds the configured max object size (%d bytes)", ErrTooLarge, opts.Size, s.maxObjectSize)
	}

	// The bound is the inner reader, so a body past a Size equal to the
	// bound reports ErrTooLarge.
	var bounded, sized *limitReader
	if s.maxObjectSize > 0 {
		bounded = newLimitReader(body, s.maxObjectSize, false,
			fmt.Errorf("body exceeds the configured max object size (%d bytes)", s.maxObjectSize))
		body = bounded
	}
	if opts.Size > 0 {
		sized = newLimitReader(body, opts.Size, true,
			fmt.Errorf("body is longer than the declared size (%d bytes)", opts.Size))
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

// readFailure combines the error a limitReader recorded with what the
// provider returned, so the provider's cause stays matchable.
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
// configured ListPageSize; a negative Limit is an error the provider never
// sees.
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

// EnsureContainer delegates to the provider under the caller's context with
// no readiness check, so it recovers a container deleted while the process
// runs.
func (s *Store) EnsureContainer(ctx context.Context) error {
	return s.client.EnsureContainer(ctx)
}

// Probe reports whether the provider is reachable, under the caller's
// context.
func (s *Store) Probe(ctx context.Context) error {
	if !s.started.Load() {
		return ErrNotReady
	}
	return s.client.Probe(ctx)
}

// Capabilities returns the provider's key constraints, with no readiness
// check.
func (s *Store) Capabilities() Capabilities {
	return s.client.Capabilities()
}

// Container returns the configured container name.
func (s *Store) Container() string {
	return s.container
}

// limitReader lets limit bytes through and fails on the first byte past
// them with long. When exact is set, an io.EOF before limit bytes fails too,
// with an error wrapping io.ErrUnexpectedEOF. A failure is repeated on every
// later Read, and tripped is atomic so Put can read it after the provider
// returns.
type limitReader struct {
	r       io.Reader
	limit   int64
	read    int64
	exact   bool
	long    error
	err     error
	tripped atomic.Bool
}

func newLimitReader(r io.Reader, limit int64, exact bool, long error) *limitReader {
	return &limitReader{r: r, limit: limit, exact: exact, long: long}
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.tripped.Load() {
		return 0, l.err
	}
	remain := l.limit - l.read
	if remain <= 0 {
		// One more byte from the source means the body runs past the limit.
		var probe [1]byte
		n, err := l.r.Read(probe[:])
		if n > 0 {
			return 0, l.trip(l.long)
		}
		return 0, err
	}
	if int64(len(p)) > remain {
		p = p[:remain]
	}
	n, err := l.r.Read(p)
	l.read += int64(n)
	if l.exact && errors.Is(err, io.EOF) && l.read < l.limit {
		return n, l.trip(fmt.Errorf("body ended after %d bytes, short of the declared size (%d bytes): %w", l.read, l.limit, io.ErrUnexpectedEOF))
	}
	return n, err
}

// trip records err as the failure every later Read reports and returns it.
func (l *limitReader) trip(err error) error {
	l.err = err
	l.tripped.Store(true)
	return err
}
