// Package azureblob is the Azure Blob Storage provider for the storage
// package: a storage.Client over one container of one storage account,
// built on the Azure SDK for Go's azblob module. The SDK lives in this
// sub-module's go.mod, so it enters a consumer's graph only when this package
// is imported, once, at the composition root. The provider authenticates with
// the account's shared key; Azure AD credentials are not supported.
//
// # Construction
//
// [New] builds a [Client] from a finalized storage.Config without I/O.
// Container, Account, and Key are required. Key is the account's shared key,
// base64 encoded as the portal shows it. Account is needed even when an
// Endpoint is set, because the shared-key signature the SDK computes for
// every request carries the account name. An unfinalized config is a wiring
// defect and panics; a missing field or a malformed option is a content
// defect and returns an error.
//
// The result is wired into a storage.Store the way any provider is:
//
//	client, err := azureblob.New(cfg)
//	if err != nil {
//		return err
//	}
//	store := storage.New(client, cfg)
//
// # Endpoints
//
// An empty Endpoint means the account's public service URL,
// https://<Account>.blob.core.windows.net/. A set Endpoint is used as the
// service URL exactly as given, and the container name is appended to its
// path. Azurite's path-style URL carries the account name as the first path
// segment, so its endpoint is http://127.0.0.1:10000/devstoreaccount1 with
// Account devstoreaccount1 and the published development key. Azurite must
// run with --skipApiVersionCheck when the SDK's service version is newer than
// the image knows; the SDK this module pins sends x-ms-version 2026-12-06.
//
// # Options
//
// Config.Options carries the provider's own settings. Every key this package
// reads is listed here; any other key is ignored.
//
//   - max_retries: the number of times the SDK retries a request that failed
//     with a transport error or a retryable status (408, 429, 500, 502, 503,
//     504), as a non-negative integer. 0 means one try and no retries. Unset
//     means the SDK's own default, which is 3 retries with exponential
//     backoff starting at 800 milliseconds. A malformed value is a
//     construction error. A deployment that wants storage.Store's bounded
//     probes to fail fast sets a small value here, because the retry backoff
//     otherwise consumes most of the probe's timeout.
//   - block_size: the size in bytes of each block a Put stages when the body
//     is longer than one block, and of each buffer an upload worker holds.
//     The value is an integer between 1,048,576 (1 MiB, the SDK's floor) and
//     104,857,600 (100 MiB). Unset means [DefaultBlockSize], 4 MiB.
//   - concurrency: the number of workers that stage blocks for one Put, each
//     holding one block buffer, as an integer between 1 and 32. Unset means
//     [DefaultConcurrency], 4.
//
// A Put in flight holds at most block_size times concurrency bytes of buffer
// memory: 16 MiB at the defaults. The SDK allocates the buffers with an
// anonymous mmap rather than on the Go heap, one at a time as they are
// needed, so a body shorter than one block allocates one buffer. A process
// that serves many uploads at once multiplies that figure by its concurrent
// Puts. The service accepts at most 50,000 blocks per blob, so the largest
// object a Put can store is 50,000 blocks of block_size: about 195 GiB at
// the default.
//
// # Container operations
//
// [Client.EnsureContainer] creates the configured container and treats the
// service's ContainerAlreadyExists answer as success, so it is idempotent.
// [Client.Probe] reads the container's properties, which proves the
// endpoint, the credential, and the container together; a missing container
// matches storage.ErrNotFound.
//
// # Object operations
//
// [Client.Put] uploads the body through the SDK's streaming upload, which
// takes a plain io.Reader and needs neither the body's length nor a seek. A
// body shorter than one block is sent in one Put Blob request; a longer one
// is staged as blocks and committed with one Put Block List. Both requests
// replace the blob in one step, and the SDK sends either only after the body
// has ended, so Put is all or nothing: on success the blob holds exactly the
// bytes the body yielded through EOF, and on any error nothing is written at
// the key and a blob already stored there is unchanged. The Size the
// returned Object reports is the count of bytes read. PutOptions.ContentType
// is sent as the blob's Content-Type when set. The service resets a replaced
// blob's content type to application/octet-stream when the request carries
// none, so a caller that wants the type kept across a replace passes it on
// every Put.
//
// A PutOptions.Size greater than 0 is enforced on the body. A body that ends
// short of Size fails with an error wrapping io.ErrUnexpectedEOF, and one
// that runs past Size fails on the first byte beyond it, so the blob is
// never truncated to Size and never holds a body longer than it. A Size of 0
// asserts nothing. Either mismatch is a failure of the body, described next.
//
// A failure of the body itself is not a failure of the store. When the body
// returns an error other than io.EOF, Put returns that error wrapped and
// unclassified, so it never matches storage.ErrUnavailable and a sentinel
// the body carried stays matchable: storage.Store's size bound reaches the
// caller as storage.ErrTooLarge through this path. Nothing is committed
// then. A body that fit in one block was never sent, and the staged blocks
// of a longer one stay uncommitted. Uncommitted blocks are invisible to Get,
// Stat, and List, and the service discards them after about a week when no
// Put Block List has committed them; they are the one trace a failed upload
// leaves.
//
// [Client.Get] opens the blob with one Get Blob request and returns the
// response body as the stream; the caller closes it. [Client.Stat] reads the
// blob's properties. Both build the Object from the response headers, and a
// missing blob matches storage.ErrNotFound. [Client.Delete] treats the
// service's BlobNotFound answer as the idempotent success the contract asks
// for. A missing container is not swallowed: it matches storage.ErrNotFound,
// because it says the configured target is gone rather than that the key is.
//
// [Client.List] fetches one page of the container's flat listing per call.
// ListOptions.Prefix, Token, and Limit map to the request's prefix, marker,
// and maxresults; a Limit of 0 leaves maxresults unset, so the service's own
// page size (5,000) applies. Page.Next is the service's NextMarker, verbatim
// and never inspected: Azure returns an opaque token and Azurite returns the
// last key of the page, and either is passed back as given.
//
// The ETag is reported in HTTP entity-tag form on every call. The service
// quotes it in the response headers Put, Get, and Stat read ("0x8D...") and
// leaves it unquoted in the XML of a listing (0x8D...); the client adds the
// quotes a listing omits and leaves a quoted or W/"..." value as it is, so
// an Object from List carries the same string for a version as one from
// Stat. Azurite behaves the same way.
//
// # Acceptance against Azurite
//
// The package's unit tests run against a scripted HTTP server. Two further
// tests run against a real service and skip unless the environment variable
// AZUREBLOB_TEST_ENDPOINT names its URL: one runs storagetest.Run over the
// provider, the other drives storage.Store.Start against an empty service.
// Each creates a container of its own and deletes it afterwards. To run
// them against Azurite, start it with the version check off and point the
// variable at its path-style address:
//
//	docker run --rm -p 10000:10000 mcr.microsoft.com/azure-storage/azurite \
//		azurite-blob --blobHost 0.0.0.0 --skipApiVersionCheck
//	AZUREBLOB_TEST_ENDPOINT=http://127.0.0.1:10000/devstoreaccount1 go test ./...
//
// The tests use the published development account and key, so no
// configuration beyond the endpoint is needed.
//
// # Keys
//
// [Client.Capabilities] declares the blob-name rules of the Azure Blob
// service. Azure documents a 1,024-character limit without saying what it
// counts; this package counts runes, so [MaxKeyLength] is 1,024 runes and a
// key of multi-byte characters is measured by characters, not bytes. A key
// is non-empty, has at most [MaxKeySegments] (254) path segments separated by
// forward slashes, contains no control character (U+0000 through U+001F and
// U+007F through U+009F), and does not end in a dot, a forward slash, or a
// backslash; no path segment ends in a dot either. Azurite accepts a key that
// breaks each of these rules, so the validation is proved by this package's
// unit tests rather than against the emulator.
//
// # Errors
//
// Every method classifies the SDK's error into the storage package's
// sentinels in the dual form fmt.Errorf("%w: %w", sentinel, err), so
// errors.Is classifies while errors.As still reaches the SDK's
// *azcore.ResponseError. A BlobNotFound or ContainerNotFound answer matches
// storage.ErrNotFound. A 5xx answer, the retryable ServerBusy,
// OperationTimedOut, and InternalError codes, and any failure with no
// response at all (a refused connection, a DNS failure, or a deadline that
// expired while the SDK retried) match storage.ErrUnavailable; the wrapped
// error still matches context.DeadlineExceeded when that is what ended the
// call. A cancellation the caller raised passes through unclassified, since
// it says nothing about the store. Authentication and authorization failures
// and every other 4xx answer are returned unclassified with the SDK's error
// intact.
package azureblob
