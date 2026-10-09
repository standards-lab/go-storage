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
- Requires `github.com/standards-lab/go-storage` v0.5.0.

[Unreleased]: https://github.com/standards-lab/go-storage/commits/HEAD/s3
