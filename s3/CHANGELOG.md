# Changelog

All notable changes to the S3 provider (`github.com/standards-lab/go-storage/s3`) are
documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). This
changelog covers this sub-module only; the base module keeps its own.

## [Unreleased]

### Added

- The S3 provider arrives, ported from the spike that proved it against SeaweedFS's S3 gateway.
  `New` constructs a `Client` from a finalized `storage.Config` without I/O, over aws-sdk-go-v2's
  `service/s3` and `feature/s3/transfermanager` with a static access key. `Container` is the
  bucket, `Account` the access key ID, and `Key` the secret access key. An empty `Endpoint`
  means AWS's endpoint for the region; a set `Endpoint` is used with path-style addressing,
  which reaches an S3-compatible gateway.
- The `region`, `max_retries`, and `part_size` options.
- `Client.EnsureContainer`, `Client.Probe`, `Client.Capabilities`, and the object operations,
  so `Client` implements `storage.Client` in full. `Put` is all or nothing, sending a body of
  up to one part as a single `PutObject` and a longer one as a multipart upload that is aborted
  on failure.
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
  integer from 1 to 32. Unset means 4. The package documentation states the memory a multipart
  Put holds: at most `part_size` × (`concurrency` + 2) bytes of the body read ahead of the
  acknowledged parts, 48 MiB at the defaults, and up to `part_size` × (`concurrency` + 3) while
  the upload starts, or (`concurrency` + 4) for a body of unknown size, 56 or 64 MiB at the
  defaults. A declared `Size` that raises the part size raises them too.
- Requires `github.com/standards-lab/go-storage` v0.5.0.

### Fixed

- Every failure a multipart `Put` returns begins "s3:", as its other failures do: a failed part,
  a failed completion, and the caller's cancellation midway, whose message was transfermanager's
  own "upload multipart failed, …". Each keeps its classification, and a cancellation still maps
  to no sentinel.

[Unreleased]: https://github.com/standards-lab/go-storage/commits/HEAD/s3
