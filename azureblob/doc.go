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
//
// # Container operations
//
// [Client.EnsureContainer] creates the configured container and treats the
// service's ContainerAlreadyExists answer as success, so it is idempotent.
// [Client.Probe] reads the container's properties, which proves the
// endpoint, the credential, and the container together; a missing container
// matches storage.ErrNotFound.
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
