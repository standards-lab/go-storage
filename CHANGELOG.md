# Changelog

All notable changes to `github.com/standards-lab/go-storage` are documented here. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). This changelog covers the base module
only; each provider sub-module keeps its own.

## [Unreleased]

### Added

- `Client` — the standard-tier interface over the five object operations Azure Blob and S3
  share (`Put`, `Get`, `Stat`, `Delete`, `List`), plus `Probe` and `Capabilities`, with the
  `Object`, `Blob`, `PutOptions`, `GetOptions`, `ListOptions`, and `Page` types. It has no
  conditional writes and no object metadata beyond `ContentType`.
- `Capabilities` — the key constraints a provider declares: a maximum length and a `ValidateKey`
  function.
- `ErrNotFound`, `ErrTooLarge`, `ErrNotReady`, and `ErrUnavailable` — the error sentinels a
  provider classifies into, wrapped in the dual form so `errors.Is` classifies while the
  provider's error stays recoverable.
- `Config` — the container, endpoint, account, key, options, size bound, page size, and probe
  timeout, on go-core's Merge-and-Finalize contract, with `Env` and `NewEnv` composing the
  override names. `Container` is the one required field. `MaxObjectSize` and `ListPageSize` have
  no default, and `RequestTimeout` defaults to 10 seconds.
- `Store` — the lifecycle wrapper that implements `Client`: a probe at `Start`, a live bounded
  probe in `Ready`, a `Shutdown` that closes the provider at most once, `ErrNotReady` from every
  object operation outside the started window, the configured page size on a `List` with no
  limit, and the `MaxObjectSize` bound on `Put` through a reader that fails on the first byte
  past it.
