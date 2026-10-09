# Changelog

All notable changes to the S3 provider (`github.com/standards-lab/go-storage/s3`) are
documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). This
changelog covers this sub-module only; the base module keeps its own.

## [Unreleased]

## [v0.1.0] - 2026-10-09

### Added

- `New` constructs a `Client` from a finalized `storage.Config` without I/O, over aws-sdk-go-v2's
  `service/s3` and `feature/s3/transfermanager` with a static access key. `Container` is the
  bucket, `Account` the access key ID, and `Key` the secret access key. An empty `Endpoint`
  means AWS's endpoint for the region; a set `Endpoint` is used with path-style addressing,
  which reaches an S3-compatible gateway.
- The `region`, `max_retries`, and `part_size` options.
- `Client.EnsureContainer`, `Client.Probe`, `Client.Capabilities`, and the object operations,
  so `Client` implements `storage.Client` in full. `Put` is all or nothing, sending a body of
  up to one part as a single `PutObject` and a longer one as a multipart upload that is aborted
  on failure. Every failure a multipart `Put` returns begins "s3:", as its other failures do: a
  failed part, a failed completion, and the caller's cancellation midway, which maps to no
  sentinel.
- Error classification into the base module's sentinels in the dual-wrap form.
- Acceptance tests that run the `storagetest` conformance suite and a `storage.Store.Start`
  against a real gateway, gated on `S3_TEST_ENDPOINT` so they skip on the unit tier.
- The `try_timeout` option: a positive Go duration that bounds each try of a request, a Get's
  body read included, through the HTTP client's timeout. A stalled try is retried up to
  `max_retries` times, and a request whose every try stalls fails with `storage.ErrUnavailable`.
  Unset means no deadline.
- A Get body resumes a read that fails mid-stream, past `try_timeout` or on a lost connection,
  with a ranged `GetObject` from its offset conditioned with `If-Match` on the first answer's
  ETag, up to `max_retries` times per read, so a download outlasts `try_timeout`. An object
  replaced or deleted before a resumption fails the read with `storage.ErrNotFound`, and a body
  that stalls on every try fails it with `storage.ErrUnavailable`. A read that resumes is not
  validated by checksum.
- The `concurrency` option: the number of parts of one multipart upload in flight at once, an
  integer from 1 to 32. Unset means 4. At the defaults a multipart `Put` reads at most 48 MiB of
  its body ahead of the acknowledged parts and holds up to 64 MiB while the upload starts; the
  package documentation counts the memory.
- Requires `github.com/standards-lab/go-storage` v0.5.0.

[Unreleased]: https://github.com/standards-lab/go-storage/compare/s3/v0.1.0...HEAD
[v0.1.0]: https://github.com/standards-lab/go-storage/releases/tag/s3/v0.1.0
