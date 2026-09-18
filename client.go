package storage

import (
	"context"
	"io"
	"time"
)

// Object is the metadata a provider reports for one stored object.
type Object struct {
	// Key is the object's full key within the configured container.
	Key string

	// Size is the object's length in bytes.
	Size int64

	// ContentType is the object's media type, carried as an HTTP header with
	// identical semantics on every target API.
	ContentType string

	// ETag is the provider's opaque version identifier for the object's
	// current content.
	ETag string

	// ModifiedAt is the time the provider recorded for the object's last
	// write.
	ModifiedAt time.Time
}

// Blob is an open read: the object's metadata and its byte stream. The
// caller closes Body.
type Blob struct {
	Object

	// Body streams the object's content. The caller closes it, whether or
	// not the read completed.
	Body io.ReadCloser
}

// PutOptions carries what the caller knows about a Put body.
type PutOptions struct {
	// ContentType is the media type stored with the object.
	ContentType string

	// Size is the body's length when known; 0 means unknown. A provider
	// that must know the length to sign its request buffers the body only
	// when Size is 0.
	Size int64
}

// GetOptions is reserved for options on Get. It is empty on purpose: a byte
// range is the anticipated first field, and the parameter exists now so
// adding it later changes no signature.
type GetOptions struct{}

// ListOptions selects which objects a List call returns.
type ListOptions struct {
	// Prefix restricts the listing to keys that begin with it. An empty
	// Prefix lists the whole container.
	Prefix string

	// Token is an opaque continuation token from a previous Page.Next. An
	// empty Token starts from the beginning.
	Token string

	// Limit caps the number of objects in the returned page. 0 means the
	// configured or provider default.
	Limit int
}

// Page is one page of a listing.
type Page struct {
	// Objects holds the page's objects in the provider's listing order.
	Objects []Object

	// Next is the continuation token for the following page. It is empty
	// when no further page exists.
	Next string
}

// Capabilities states what a provider's target API requires of a key, and
// has room for further per-provider facts as they earn a place here. Every
// provider declares its key constraints, so a consumer never assumes them. A
// consumer that builds keys from a variable segment calls ValidateKey, read
// from Store.Capabilities, before Put. A key the provider would reject then
// fails at construction instead of at the object store.
type Capabilities struct {
	// MaxKeyLength is the longest key the provider accepts.
	MaxKeyLength int

	// ValidateKey reports whether the provider accepts key, returning a
	// non-nil error that says why when it does not.
	ValidateKey func(key string) error
}

// Client is the standard tier: the object operations common to every target
// API. A provider sub-module implements it over its own SDK, and Store
// implements it by delegating to the provider.
//
// Classifying a provider's errors into this package's sentinels is the
// provider adapter's job. Get and Stat return an error matching
// [ErrNotFound] for a missing key. Delete is idempotent: deleting a missing
// key is a no-op success, never [ErrNotFound]. Any method may return an
// error matching [ErrUnavailable] when the store is unreachable.
type Client interface {
	// Put writes body as the object at key, replacing any existing object,
	// and returns the stored object's metadata.
	Put(ctx context.Context, key string, body io.Reader, opts PutOptions) (Object, error)

	// Get opens the object at key for reading. The caller closes the
	// returned Blob's Body.
	Get(ctx context.Context, key string, opts GetOptions) (Blob, error)

	// Stat returns the metadata of the object at key without reading its
	// content.
	Stat(ctx context.Context, key string) (Object, error)

	// Delete removes the object at key. A missing key is a no-op success.
	Delete(ctx context.Context, key string) error

	// List returns one page of objects selected by opts.
	List(ctx context.Context, opts ListOptions) (Page, error)

	// Probe reports whether the configured credential and container are
	// reachable. It is not an object operation. [Store] calls it from Start
	// and Ready.
	Probe(ctx context.Context) error

	// Capabilities returns what the provider's target API requires of a
	// key.
	Capabilities() Capabilities
}
