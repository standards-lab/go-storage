# Changelog

All notable changes to the Azure Blob Storage provider
(`github.com/standards-lab/go-storage/azureblob`) are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). This changelog covers this
sub-module only; the base module keeps its own.

## [Unreleased]

### Added

- `New` constructs a `Client` from a finalized `storage.Config` without I/O, over the Azure SDK
  for Go's `azblob` module (v1.8.1) with a shared-key credential. `Container`, `Account`, and
  `Key` are required. An empty `Endpoint` means the account's public service URL; a set
  `Endpoint` is the service URL as given, which reaches Azurite's path-style address.
- The `max_retries` option bounds the SDK's retry count; unset keeps the SDK default, and `0`
  means one try.
- `Client.EnsureContainer` creates the configured container and treats `ContainerAlreadyExists`
  as success. `Client.Probe` reads the container's properties, and a missing container matches
  `storage.ErrNotFound`.
- `Client.Capabilities` declares the Azure blob-name rules: at most `MaxKeyLength` (1,024)
  runes, at most `MaxKeySegments` (254) path segments, no control characters, and no trailing
  dot, forward slash, or backslash, with no path segment ending in a dot.
- Error classification into the base module's sentinels in the dual-wrap form: `BlobNotFound`
  and `ContainerNotFound` match `ErrNotFound`; a 5xx answer, the retryable `ServerBusy`,
  `OperationTimedOut`, and `InternalError` codes, and a failure with no response match
  `ErrUnavailable`; a caller's cancellation and every other 4xx answer pass through
  unclassified.
- The object operations, so `Client` now implements `storage.Client` in full. `Put` uploads
  through the SDK's streaming upload, which takes a non-seekable body and reads it to EOF,
  sends `ContentType` as the blob's content type, and reports the bytes read and the service's
  ETag and last-modified time. `Put` is all or nothing: the SDK sends the one Put Blob or the
  Put Block List only after the body has ended, so a failed upload commits nothing and leaves
  an existing blob unchanged. A `PutOptions.Size` greater than 0 is enforced: a body that ends
  short of it fails with an error wrapping `io.ErrUnexpectedEOF`, and one that runs past it
  fails on the first byte beyond, so the blob is never truncated to `Size`. A failure of the
  body itself, a `Size` mismatch included, is returned unclassified, so it never matches
  `ErrUnavailable` and `storage.Store`'s `ErrTooLarge` stays matchable. `Get` streams the
  blob, `Stat` reads its properties, `Delete` treats `BlobNotFound` as success, and `List`
  returns one page of the flat listing with the service's `NextMarker` as the opaque
  `Page.Next`. The ETag is reported in HTTP entity-tag form on every call: the quotes a
  listing's XML omits are added, so `List` and `Stat` report the same string for one version.
- The `block_size` and `concurrency` options size a `Put`'s upload: blocks of `block_size`
  bytes (1 MiB to 100 MiB, `DefaultBlockSize` 4 MiB) staged by `concurrency` workers (1 to 32,
  `DefaultConcurrency` 4), each holding one block buffer, so a `Put` holds at most 16 MiB at
  the defaults. The SDK's CPU-scaled default concurrency is never used.
- Acceptance tests that run the `storagetest` conformance suite and a `storage.Store.Start`
  against a real service, gated on `AZUREBLOB_TEST_ENDPOINT` so they skip on the unit tier.
