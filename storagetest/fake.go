package storagetest

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
	"time"
	"unicode/utf8"

	"github.com/standards-lab/go-storage"
)

var _ storage.Client = (*Fake)(nil)

// DefaultMaxKeyLength is the MaxKeyLength a Fake declares when no
// WithCapabilities option replaces it.
const DefaultMaxKeyLength = 1024

// defaultPageSize is the page size List uses when Limit is 0 and no
// WithPageSize option replaces it. It is small so a test that lists more than
// a handful of objects pages.
const defaultPageSize = 3

var (
	// ErrDown is the cause a Fake wraps under storage.ErrUnavailable while
	// Down is set.
	ErrDown = errors.New("storagetest: connection refused")

	// ErrNoSuchKey is the cause a Fake wraps under storage.ErrNotFound for a
	// key it does not hold.
	ErrNoSuchKey = errors.New("storagetest: no such key")

	// ErrNoSuchContainer is the cause a Fake wraps under storage.ErrNotFound
	// while its container does not exist.
	ErrNoSuchContainer = errors.New("storagetest: no such container")
)

// Fake is an in-memory storage.Client. It honors the Client contract, so a
// test can wrap it in a storage.Store or hand it to any consumer of the
// interface, and it records what it received so a test can assert what was
// passed through. Put stores a copy of the whole body, Get returns a copy,
// and the ETag is a hash of the content in quoted entity-tag form, so equal
// bodies share an ETag.
//
// Down is the outage toggle: while it is set, every method, EnsureContainer
// and Probe included, fails with an error matching storage.ErrUnavailable
// that wraps ErrDown. The container is modelled too: while it does not
// exist, Probe and every object operation fail with an error matching
// storage.ErrNotFound that wraps ErrNoSuchContainer, and EnsureContainer
// creates it. A Fake starts with its container in place unless
// WithoutContainer says otherwise, and DropContainer removes it again.
//
// A Fake is safe for concurrent use. A test that embeds *Fake in its own
// type can override one method and keep the rest.
type Fake struct {
	// Down is the outage toggle. Every method consults it on entry.
	Down atomic.Bool

	// hasContainer is the container toggle. WithoutContainer and
	// DropContainer clear it, and EnsureContainer sets it.
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
	objects map[string]object

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

type object struct {
	meta storage.Object
	data []byte
}

// Option configures a Fake at construction.
type Option func(*Fake)

// WithClock supplies the time stamped on every Put as ModifiedAt. The default
// is time.Now.
func WithClock(now func() time.Time) Option {
	return func(f *Fake) { f.now = now }
}

// WithPageSize sets the page size List uses when Limit is 0. The default is
// 3, so a listing of a few objects already pages.
func WithPageSize(n int) Option {
	return func(f *Fake) { f.pageSize = n }
}

// WithCapabilities replaces what Capabilities returns. The default declares
// DefaultMaxKeyLength and a ValidateKey that rejects an empty key and a key
// longer than that, counted in runes.
func WithCapabilities(c storage.Capabilities) Option {
	return func(f *Fake) { f.caps = c }
}

// WithoutContainer starts the Fake with no container, so a test can watch
// EnsureContainer create it.
func WithoutContainer() Option {
	return func(f *Fake) { f.hasContainer.Store(false) }
}

// NewFake returns an empty Fake whose container exists, configured by opts.
func NewFake(opts ...Option) *Fake {
	f := &Fake{
		now:      time.Now,
		pageSize: defaultPageSize,
		caps: storage.Capabilities{
			MaxKeyLength: DefaultMaxKeyLength,
			ValidateKey:  validateKey,
		},
		objects: make(map[string]object),
	}
	f.hasContainer.Store(true)
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// validateKey is the default ValidateKey: a key is non-empty and at most
// DefaultMaxKeyLength runes.
func validateKey(key string) error {
	if key == "" {
		return errors.New("storagetest: empty key")
	}
	if n := utf8.RuneCountInString(key); n > DefaultMaxKeyLength {
		return fmt.Errorf("storagetest: key is %d characters, the limit is %d", n, DefaultMaxKeyLength)
	}
	return nil
}

// FailPut sets the error every subsequent Put returns after it has read its
// body, which is when a real provider learns the outcome of its request. A
// nil err restores normal Puts.
func (f *Fake) FailPut(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.putErr = err
}

// DropContainer removes the container and every object in it, as if it had
// been deleted out from under the client. Probe and the object operations
// then fail with storage.ErrNotFound until EnsureContainer creates it again,
// empty.
func (f *Fake) DropContainer() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hasContainer.Store(false)
	clear(f.objects)
}

// HasContainer reports whether the container exists.
func (f *Fake) HasContainer() bool {
	return f.hasContainer.Load()
}

// Puts reports how many Put calls the Fake has received, successful or not.
func (f *Fake) Puts() int64 {
	return f.puts.Load()
}

// Ensures reports how many EnsureContainer calls the Fake has received,
// successful or not.
func (f *Fake) Ensures() int64 {
	return f.ensures.Load()
}

// Probes reports how many Probe calls the Fake has received, successful or
// not.
func (f *Fake) Probes() int64 {
	return f.probes.Load()
}

// LastPut reports the options of the most recent Put and how many body bytes
// it read before it returned.
func (f *Fake) LastPut() (storage.PutOptions, int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastPutOpts, f.lastPutConsumed
}

// LastEnsure reports the deadline of the context the most recent
// EnsureContainer received, and whether that context carried one.
func (f *Fake) LastEnsure() (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastEnsureDeadline, f.lastEnsureBounded
}

// LastProbe reports the deadline of the context the most recent Probe
// received, and whether that context carried one.
func (f *Fake) LastProbe() (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastProbeDeadline, f.lastProbeBounded
}

// LastList reports the options of the most recent List call.
func (f *Fake) LastList() storage.ListOptions {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastListOpts
}

func (f *Fake) unavailable() error {
	return fmt.Errorf("%w: %w", storage.ErrUnavailable, ErrDown)
}

func (f *Fake) noContainer() error {
	return fmt.Errorf("%w: %w", storage.ErrNotFound, ErrNoSuchContainer)
}

// Put reads body to EOF and stores a copy under key, replacing any existing
// object and its ContentType. It counts the call and records opts and the
// bytes read before it checks Down, the container, or a FailPut error. A Put
// that fails, whether on the body, on a Size that disagrees with the bytes
// read, or on a FailPut error, stores nothing and leaves an existing object
// at key as it was.
func (f *Fake) Put(_ context.Context, key string, body io.Reader, opts storage.PutOptions) (storage.Object, error) {
	f.puts.Add(1)
	if f.Down.Load() {
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
	if n := int64(len(data)); opts.Size > 0 && n != opts.Size {
		if n < opts.Size {
			return storage.Object{}, fmt.Errorf("fake put: body ended after %d bytes, short of the declared size (%d bytes): %w", n, opts.Size, io.ErrUnexpectedEOF)
		}
		return storage.Object{}, fmt.Errorf("fake put: body is %d bytes, longer than the declared size (%d bytes)", n, opts.Size)
	}
	if f.putErr != nil {
		return storage.Object{}, fmt.Errorf("fake put: %w", f.putErr)
	}

	sum := sha256.Sum256(data)
	obj := object{
		meta: storage.Object{
			Key:         key,
			Size:        int64(len(data)),
			ContentType: opts.ContentType,
			ETag:        `"` + hex.EncodeToString(sum[:]) + `"`,
			ModifiedAt:  f.now(),
		},
		data: data,
	}
	f.objects[key] = obj
	return obj.meta, nil
}

// Get returns the object's metadata and a reader over a copy of its content.
func (f *Fake) Get(_ context.Context, key string, _ storage.GetOptions) (storage.Blob, error) {
	if f.Down.Load() {
		return storage.Blob{}, f.unavailable()
	}
	if !f.hasContainer.Load() {
		return storage.Blob{}, f.noContainer()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	obj, ok := f.objects[key]
	if !ok {
		return storage.Blob{}, fmt.Errorf("%w: %w", storage.ErrNotFound, ErrNoSuchKey)
	}
	return storage.Blob{
		Object: obj.meta,
		Body:   io.NopCloser(bytes.NewReader(bytes.Clone(obj.data))),
	}, nil
}

// Stat returns the object's metadata.
func (f *Fake) Stat(_ context.Context, key string) (storage.Object, error) {
	if f.Down.Load() {
		return storage.Object{}, f.unavailable()
	}
	if !f.hasContainer.Load() {
		return storage.Object{}, f.noContainer()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	obj, ok := f.objects[key]
	if !ok {
		return storage.Object{}, fmt.Errorf("%w: %w", storage.ErrNotFound, ErrNoSuchKey)
	}
	return obj.meta, nil
}

// Delete removes the object at key. A missing key is a no-op success.
func (f *Fake) Delete(_ context.Context, key string) error {
	if f.Down.Load() {
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
func (f *Fake) List(_ context.Context, opts storage.ListOptions) (storage.Page, error) {
	if f.Down.Load() {
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
// idempotent: a container that already exists is left as it is, objects
// included.
func (f *Fake) EnsureContainer(ctx context.Context) error {
	f.ensures.Add(1)

	deadline, bounded := ctx.Deadline()
	f.mu.Lock()
	f.lastEnsureDeadline = deadline
	f.lastEnsureBounded = bounded
	f.mu.Unlock()

	if f.Down.Load() {
		return f.unavailable()
	}
	f.hasContainer.Store(true)
	return nil
}

// Probe succeeds while the Fake is not Down and its container exists.
func (f *Fake) Probe(ctx context.Context) error {
	f.probes.Add(1)

	deadline, bounded := ctx.Deadline()
	f.mu.Lock()
	f.lastProbeDeadline = deadline
	f.lastProbeBounded = bounded
	f.mu.Unlock()

	if f.Down.Load() {
		return f.unavailable()
	}
	if !f.hasContainer.Load() {
		return f.noContainer()
	}
	return nil
}

// Capabilities returns the configured capabilities.
func (f *Fake) Capabilities() storage.Capabilities {
	return f.caps
}
