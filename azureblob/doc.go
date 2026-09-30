// Package azureblob is the Azure Blob Storage provider for the storage
// package. Its [Client] implements storage.Client over one container of one
// storage account, built on the Azure SDK for Go's azblob module and
// authenticated with the account's shared key, and [New] constructs it. The
// SDK enters a consumer's graph only when this package is imported, once, at
// the composition root. The sections below state how the Client maps
// storage's contract onto the service.
//
// # Construction
//
// [New] builds a [Client] from a finalized storage.Config without I/O.
// Container, Account, and Key are required. Key is the account's shared key,
// base64 encoded as the portal shows it, and Account is needed even when an
// Endpoint is set, because the shared-key signature carries it. Container
// must be a valid Azure container name: 3 to 63 lowercase letters, digits,
// and hyphens, starting and ending with a letter or digit, with no two
// hyphens together.
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
// https://<Account>.blob.core.windows.net/. A set Endpoint is the service URL
// as given, and the Client appends the container name to its path, so it can
// name Azurite's path-style URL http://127.0.0.1:10000/devstoreaccount1.
// Azurite must run with --skipApiVersionCheck when the SDK's service version
// is newer than the image knows; the SDK this module pins sends x-ms-version
// 2026-12-06.
//
// # Options
//
// Config.Options carries the provider's own settings, each overridable
// through storage.Env. Any key not listed here is ignored.
//
//   - max_retries: how many times the SDK retries a request that failed with
//     a transport error or a retryable status (408, 429, 500, 502, 503, 504),
//     as a non-negative integer. 0 means one try. Unset keeps the SDK
//     default, 3 retries with exponential backoff from 800 milliseconds.
//   - try_timeout: the deadline on each try of a request, as a positive Go
//     duration such as "30s", so a stalled service cannot hold a call
//     indefinitely. Unset means no deadline, the SDK default. It covers one
//     operation, never a whole transfer: the upload of each Put block, and
//     the part of a Get's body read within the try. A Get's body whose try
//     deadline passes mid-read resumes from its offset with a ranged request
//     conditioned on the ETag, max_retries times per read, so a download
//     outlasts try_timeout however slowly the caller reads; a body that
//     stalls on every try fails the read with an unclassified
//     context.DeadlineExceeded. It must exceed the longest block upload.
//   - block_size: the size in bytes of each block a Put stages when the body
//     is longer than one block, from 1,048,576 (1 MiB) to 104,857,600
//     (100 MiB). Unset means 4 MiB.
//   - concurrency: the number of workers that stage blocks for one Put, from
//     1 to 32. Unset means 4.
//
// A malformed value is a construction error. A Put holds at most block_size
// times concurrency bytes of buffer (16 MiB at the defaults), and the service
// accepts at most 50,000 blocks per blob: about 195 GiB at the default size.
//
// # Object operations
//
// A Put without a ContentType resets a replaced blob's type to
// application/octet-stream, so a caller that wants the type kept across a
// replace passes it on every Put. [Client.List] returns the service's
// NextMarker as Page.Next verbatim: Azure's is an opaque token, and
// Azurite's is the page's last key. The service quotes an ETag in response
// headers and leaves it unquoted in a listing's XML; the Client adds the
// quotes, so List and Stat report the same string.
//
// # Keys
//
// [Client.Capabilities] declares the Azure blob-name rules: a key is
// non-empty, at most 1,024 runes, at most 254 path segments separated by
// forward slashes, free of control characters (U+0000 through U+001F and
// U+007F through U+009F), and does not end in a dot, a forward slash, or a
// backslash; no path segment ends in a dot either. Azurite accepts a key that
// breaks each rule, so only this package's unit tests prove them.
//
// # Errors
//
// Every method classifies the SDK's error in the dual form, so errors.As
// still reaches the SDK's *azcore.ResponseError. BlobNotFound matches
// storage.ErrNotFound and ContainerNotFound storage.ErrContainerNotFound. A
// 5xx answer, the ServerBusy, OperationTimedOut, and InternalError codes, and
// a failure with no response, an expired deadline included, match
// storage.ErrUnavailable. The caller's cancellation and every other answer,
// an authentication failure included, pass through unclassified.
//
// # Acceptance against Azurite
//
// The unit tests run against a scripted HTTP server. The acceptance tests run
// storagetest.Run, storagetest.RunMissingContainer, and a storage.Store.Start
// against a real service when AZUREBLOB_TEST_ENDPOINT names its URL, each in
// a container of its own, with the published development account and key:
//
//	docker run --rm -p 10000:10000 \
//		mcr.microsoft.com/azure-storage/azurite:3.37.0 \
//		azurite-blob --blobHost 0.0.0.0 --skipApiVersionCheck --loose
//	AZUREBLOB_TEST_ENDPOINT=http://127.0.0.1:10000/devstoreaccount1 go test ./...
package azureblob
