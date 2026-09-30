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

	// ContentType is the object's media type.
	ContentType string

	// ETag is the provider's opaque version identifier for the object's
	// current content, in HTTP entity-tag form: a quoted string, optionally
	// prefixed with W/. Put, Get, Stat, and List report the identical string
	// for one version of an object, so a caller can send it as an ETag
	// header or compare two for equality, whichever call produced them.
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
	// ContentType is the media type stored with the object. Empty stores
	// application/octet-stream.
	ContentType string

	// Size is the body's length when known; 0 means unknown and asserts
	// nothing. A Size greater than 0 is enforced as [Client.Put] describes.
	Size int64
}

// GetOptions is reserved for options on Get, such as a byte range.
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

// Capabilities states what a provider's target API requires of a key.
type Capabilities struct {
	// MaxKeyLength is the longest key the provider accepts.
	MaxKeyLength int

	// ValidateKey reports whether the provider accepts key, returning a
	// non-nil error that says why when it does not. A nil ValidateKey
	// accepts every key.
	ValidateKey func(key string) error
}

// Client is the standard tier: the object operations common to every target
// API. A provider sub-module implements it over its own SDK, and Store
// implements it by delegating to the provider.
//
// Classifying a provider's errors into this package's sentinels is the
// provider adapter's job. Get and Stat return an error matching
// [ErrNotFound] for a missing key. Delete is idempotent: deleting a missing
// key is a no-op success, never [ErrNotFound]. Probe and every object
// operation, Delete included, return an error matching
// [ErrContainerNotFound] while the configured container does not exist. Any
// method, EnsureContainer and Probe included, may return an error matching
// [ErrUnavailable] when the store is unreachable.
type Client interface {
	// Put writes body as the object at key, replacing any existing object,
	// and returns the stored object's metadata. Put is all or nothing: on
	// success the object holds exactly the bytes body yielded through EOF,
	// and on any error, a failure of body included, nothing is written at
	// key and an object already stored there is unchanged. An opts.Size
	// greater than 0 must equal the body's length; a body that is shorter or
	// longer is an error, and the provider never truncates it to Size.
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

	// EnsureContainer creates the configured container and succeeds when it
	// already exists. It never deletes or reconfigures an existing container.
	EnsureContainer(ctx context.Context) error

	// Probe reports whether the configured credential and container are
	// reachable.
	Probe(ctx context.Context) error

	// Capabilities returns what the provider's target API requires of a
	// key.
	Capabilities() Capabilities
}
