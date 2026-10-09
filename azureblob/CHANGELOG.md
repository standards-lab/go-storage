# Changelog

All notable changes to the Azure Blob Storage provider
(`github.com/standards-lab/go-storage/azureblob`) are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). This changelog covers this
sub-module only; the base module keeps its own.

## [Unreleased]

## [v0.5.0] - 2026-10-09

### Changed

- **Breaking:** The `go-storage` requirement is v0.5.0, which requires `go-core` v0.6.0. An
  importer still on go-core's `lifecycle.Service`, `Add`, or stages breaks, since the
  requirement pulls go-core v0.6.0 into its build.
- The acceptance tests run locally with `mise run acceptance:azureblob`, against Azurite built
  from `compose/azurite/Dockerfile`, the same task CI runs.

## [v0.4.0] - 2026-09-30

### Changed

- `try_timeout` bounds one operation, never a whole transfer. When a try's deadline passes
  mid-read, a `Get`'s body resumes from its offset with a ranged request conditioned on the ETag,
  up to `max_retries` times per read, so a download outlasts `try_timeout` however slowly its
  caller reads. With `max_retries` 0 the body does not resume. Before, the caller had to read the
  whole body within `try_timeout` of the request.
- A `Get` body classifies its read failures: a try's deadline or a lost connection is
  `storage.ErrUnavailable`, and a blob deleted or replaced before a resumption is
  `storage.ErrNotFound`. Before, the deadline surfaced as an unclassified
  `context.DeadlineExceeded`.

## [v0.3.0] - 2026-09-30

### Added

- The `try_timeout` option sets a deadline on each try of a request, as a Go duration, through
  the SDK's `RetryOptions.TryTimeout`, so a stalled service cannot hold a call indefinitely. It
  covers the upload of each `Put` block and a `Get`'s whole body: the caller must read and close
  the body within `try_timeout` of the request, at its own pace, and a read past the deadline
  fails with an unclassified `context.DeadlineExceeded` from the body. Unset keeps the SDK
  default, no deadline.

### Changed

- The `go-storage` requirement is v0.3.0. v0.2.0 of this module required go-storage v0.2.0 while
  documenting the option overrides go-storage v0.2.1 added.
- **Breaking:** `New` rejects a container name the service would refuse (3 to 63 lowercase
  letters, digits, and single hyphens, starting and ending with a letter or digit), where
  `Store.Start` used to fail with `storage.ErrUnavailable`.
- **Breaking:** a `Put` without a `ContentType` sends and reports `application/octet-stream`, the
  type the service stores, where it reported an empty `ContentType`.
- `New` detects an unfinalized configuration through `storage.Config.Finalized`.
- CI runs the acceptance tests against Azurite.

### Removed

- **Breaking:** the exported constants `MaxKeyLength`, `MaxKeySegments`, `DefaultBlockSize`, and
  `DefaultConcurrency`. The package documentation states their values, and
  `Client.Capabilities().MaxKeyLength` reports the key limit.

## [v0.2.0] - 2026-09-29

### Changed

- The `go-storage` requirement is v0.2.0, which adds `storage.ErrContainerNotFound`.
- **Breaking:** a `ContainerNotFound` answer now matches `storage.ErrContainerNotFound` instead of
  `storage.ErrNotFound`, on `Probe` and every object operation, `Delete` included. A
  `BlobNotFound` answer still matches `storage.ErrNotFound`. The acceptance run adds
  `storagetest.RunMissingContainer` against Azurite.

## [v0.1.0] - 2026-09-18

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
- Requires `github.com/standards-lab/go-storage` v0.1.0.

[Unreleased]: https://github.com/standards-lab/go-storage/compare/azureblob/v0.5.0...HEAD
[v0.5.0]: https://github.com/standards-lab/go-storage/releases/tag/azureblob/v0.5.0
[v0.4.0]: https://github.com/standards-lab/go-storage/releases/tag/azureblob/v0.4.0
[v0.3.0]: https://github.com/standards-lab/go-storage/releases/tag/azureblob/v0.3.0
[v0.2.0]: https://github.com/standards-lab/go-storage/releases/tag/azureblob/v0.2.0
[v0.1.0]: https://github.com/standards-lab/go-storage/releases/tag/azureblob/v0.1.0
