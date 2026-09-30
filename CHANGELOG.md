# Changelog

All notable changes to `github.com/standards-lab/go-storage` are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). This changelog covers the base module
only; each provider sub-module keeps its own.

## [Unreleased]

## [v0.3.0] - 2026-09-30

### Added

- `Config.Finalized` reports whether `Finalize` has run, so a provider's constructor can detect an
  unfinalized configuration without relying on the base module's private rule.
- `storagetest.Fake.SetDown` starts or ends the Fake's outage.

### Changed

- **Breaking:** `Store.Shutdown` is terminal. `Start` after `Shutdown` returns `ErrNotReady` without
  calling the provider, and a `Shutdown` that lands while `Start` is probing leaves the store not
  started, with `Start` returning `ErrNotReady`.
- **Breaking:** `Store.Put` checks the key with the provider's `Capabilities().ValidateKey` before
  the provider sees it, and returns that error, and it rejects a negative `PutOptions.Size`.
- **Breaking:** a `Put` without a `ContentType` stores and reports `application/octet-stream`.
  `storagetest.Fake` does, and the conformance suite checks the `ContentType` of every `Put`,
  `Get`, and `Stat` it makes.
- **Breaking:** `storagetest.Fake`'s exported `Down` field is replaced by `SetDown`.
- The conformance suite fails a missing key whose error also matches `ErrContainerNotFound` or
  `ErrUnavailable`.
- Every `Config` validation error has the form `storage: <field> <problem>`.
- The package documentation states each contract fact once, beside the symbol it governs.

### Fixed

- `Store.Start` returns the caller's own cancellation as it is instead of classifying it as
  `ErrUnavailable`.
- `Config.Merge` copies `Options` before writing, so an overlay never reaches a map the caller
  shares.
- `storagetest.WithPageSize` with 0 or less keeps the default page size, where `Fake.List`
  panicked.
- `storagetest.Fake.Put` reads the body and records the call before it reports an outage or a
  missing container, as its documentation states.

## [v0.2.1] - 2026-09-29

### Added

- Provider options take environment overrides, so a deployment can tune a provider without a
  configuration file. Each `<PREFIX>_STORAGE_OPTIONS_<KEY>` variable sets the option named by the
  lower-cased `KEY`, replacing the configured value: `APP_STORAGE_OPTIONS_MAX_RETRIES` sets
  azureblob's `max_retries`. `KEY` consists of upper-case letters, digits, and underscores.
  `Env.Options` records the `<PREFIX>_STORAGE_OPTIONS` prefix. The provider parses the value when
  it is constructed, so a malformed value fails there, not in `Finalize`.

## [v0.2.0] - 2026-09-29

### Added

- `ErrContainerNotFound` — the sentinel for a missing container. Probe and every object
  operation, `Delete` included, return it while the configured container does not exist, and it
  never matches `ErrNotFound`, so a consumer can tell an absent object from a lost container.
- `storagetest.RunMissingContainer` — the conformance check a provider runs against a client wired
  to a container that does not exist. It creates nothing.

### Changed

- **Breaking:** a missing container no longer matches `ErrNotFound`. `storagetest.Fake` reports it
  as `ErrContainerNotFound` wrapping `ErrNoSuchContainer`, and providers classify it the same way.

## [v0.1.0] - 2026-09-18

### Added

- `Client` — the standard-tier interface over the five object operations Azure Blob and S3
  share (`Put`, `Get`, `Stat`, `Delete`, `List`), plus `EnsureContainer`, `Probe`, and
  `Capabilities`, with the `Object`, `Blob`, `PutOptions`, `GetOptions`, `ListOptions`, and
  `Page` types. It has no conditional writes and no object metadata beyond `ContentType`.
- `Client.EnsureContainer` — creates the configured container and succeeds when it already
  exists. It is idempotent and never deletes or reconfigures an existing container.
- `Store.EnsureContainer` — delegates to the provider under the caller's context with no
  readiness check, so a container deleted while the process runs can be recovered.
  `Store.Container` returns the configured container name.
- `Capabilities` — the key constraints a provider declares: a maximum length and a `ValidateKey`
  function.
- `ErrNotFound`, `ErrTooLarge`, `ErrNotReady`, and `ErrUnavailable` — the error sentinels a
  provider classifies into, wrapped in the dual form so `errors.Is` classifies while the
  provider's error stays recoverable.
- `Config` — the container, endpoint, account, key, options, size bound, page size, and probe
  timeout, on go-core's Merge-and-Finalize contract, with `Env` and `NewEnv` composing the
  override names. `Container` is the one required field. `MaxObjectSize` and `ListPageSize` have
  no default, and `RequestTimeout` defaults to 10 seconds.
- `Store` — the lifecycle wrapper that implements `Client`: an `EnsureContainer` and then a probe
  at `Start`, both under one context bounded by `RequestTimeout`, a live bounded probe in
  `Ready`, a `Shutdown` that closes the provider at most once, `ErrNotReady` from every
  object operation outside the started window, the configured page size on a `List` with no
  limit, and the `MaxObjectSize` bound on `Put` through a reader that fails on the first byte
  past it.
- The atomic `Put` contract. `Client.Put` is all or nothing: on any error nothing is written at
  the key and an existing object is unchanged, and on success the object holds exactly the
  bytes the body yielded through EOF. A `PutOptions.Size` greater than 0 must equal the body's
  length, and a body that is shorter or longer is an error that stores nothing. `Store.Put`
  enforces a declared `Size` on the body whether or not a bound is configured: a short body ends
  in an error wrapping `io.ErrUnexpectedEOF` and a long one fails on the first byte past
  `Size`, so the provider sees a read failure before it can commit. The bound is the inner
  reader, so a body past a `Size` equal to the bound still reports `ErrTooLarge`.
- `Object.ETag` is an HTTP entity tag: a quoted string, optionally prefixed `W/`, identical
  across `Put`, `Get`, `Stat`, and `List` for one version of an object.
- `storagetest` — the test support package. `Fake` is an in-memory `Client` for a consumer's
  hermetic tests, with an outage toggle, a container that `DropContainer` removes and
  `EnsureContainer` recreates, injected `Put` failures, and observation of the calls it received.
  `Run` is the conformance suite a provider runs against its own `Client`: it writes every key
  under a random prefix and deletes what it wrote, so it runs against a shared container.
- `storagetest` cases for the atomic `Put` contract and the entity-tag form. `PutBodyFailsMidway`
  and `PutSizeMismatch` assert that a body which fails partway and a `Size` that disagrees
  with the body each return an error, after which a fresh key is absent and an existing key
  holds its previous bytes, content type, and ETag. Every reported ETag is asserted to be in
  entity-tag form and equal across `Put`, `Get`, `Stat`, and `List`. `Fake` enforces `Size`
  and reports a quoted ETag.

[Unreleased]: https://github.com/standards-lab/go-storage/compare/v0.3.0...HEAD
[v0.3.0]: https://github.com/standards-lab/go-storage/releases/tag/v0.3.0
[v0.2.1]: https://github.com/standards-lab/go-storage/releases/tag/v0.2.1
[v0.2.0]: https://github.com/standards-lab/go-storage/releases/tag/v0.2.0
[v0.1.0]: https://github.com/standards-lab/go-storage/releases/tag/v0.1.0
