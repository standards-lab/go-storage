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
