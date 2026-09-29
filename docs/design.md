# Design

This page explains why go-storage is shaped as it is. The package documentation states what each
type does.

## The standard tier is a proposal

No formal standard exists for object storage, so the standard tier is derived from what the two
target APIs, Azure Blob Storage and Amazon S3, share, and kept narrow. An interface is the least
reversible thing a library ships: every provider implements it, and widening it breaks each one.
The tier becomes a validated standard only once a second provider passes the `storagetest` suite.
Until then it is a proposal, with `azureblob` its only provider.

Object storage is protocol-driven, not DSL-driven: a consumer calls operations with typed
arguments, and there is no text artifact and no dialect.

## What the interface leaves out

- **Conditional writes.** S3 has supported `If-Match` and `If-None-Match` on `PutObject` only
  since 2024, and S3-compatible stores cover them unevenly. The owning SQL row's version column
  controls a stored object's concurrency, never the object store's ETag.
- **Object metadata beyond `ContentType`.** Azure metadata keys must be C# identifiers and
  normalize case; S3's take an `x-amz-meta-` prefix and are lowercased. They don't round-trip
  identically, and every object already has a SQL row that is the metadata authority.
  `ContentType` stays because it is an HTTP header with identical semantics on both.
- **Copy, move, and signed URLs.** The tier is neither the intersection nor the union of the
  providers' features, only the operations real use needs. Presigned URLs and SAS tokens are
  native tier even though both providers have them, because no standard defines them, and the
  authorization posture forbids them on the request path: the service authorizes the request
  against the record and proxies the bytes itself.

`Probe` and `EnsureContainer` are on `Client` although they are not object operations, because
`Store`'s lifecycle and the admin surface need them from every provider, and adding a method once
a provider exists breaks it. `Capabilities` is on `Client` for the same reason: as an optional
interface, a provider could skip key validation silently. It is not called a dialect, a term kept
for SQL rendering.

`PutOptions.Size` is explicit because the SDKs disagree: Azure chunks an unknown-length reader on
its own, while the AWS SDK needs a seekable body or a known length to sign the request. Passing a
known `Content-Length` through keeps the common path free of buffering.

## `Store` is the call surface

`Store` implements `Client` itself rather than only wrapping the provider. Nothing sits above the
store to carry the calls, so `Store` is where the size bound, the declared size, and the
readiness gate are enforced for every consumer.

`Start` creates the container when it is missing, so an empty store starts cleanly and a process
never exits for want of a container nothing else would create. The cost is that the serving
credential needs permission to create the container. An opt-out for least-privilege deployments
is an additive `Config` field, added when a consumer needs it. A one-shot compose service that
creates the container was rejected: it adds a service with no operator-facing path. A
`go-storage/admin` package was rejected too: storage administration forwards one call and reads
three values, holding no policy or state, so the admin surface lives in the consuming service.

## Configuration

`Container`, not `Bucket`: the standard tier takes neither provider's vocabulary. Credentials are
the account's shared key, which keeps `azidentity` and MSAL out of `azureblob` until a deployment
needs managed identity. `MaxObjectSize` and `ListPageSize` have no default because a library ships
no policy numbers; 0 means unset, and pointer fields were rejected because an explicit zero means
nothing different. `RequestTimeout` has a default because it bounds only the calls `Store` makes
for itself; one timeout over every operation would cut off a large upload.

## Swapping providers

A provider swap is a configuration change, reviewed for three differences:

- **Consistency.** Azure Blob and Amazon S3 are strongly consistent for read-after-write and list;
  an S3-compatible store may not be.
- **Delete idempotency.** The adapter normalizes it, but the providers differ beneath it.
- **Chunking limits.** An Azure block is at most 4000 MiB; an S3 part is at most 5 GiB, and every
  part but the last at least 5 MiB.

Error classification is the adapter's most important job. Azure fails a missing key with
`BlobNotFound`, while S3's `DeleteObject` answers 204 for one. The adapter maps a missing key to
`ErrNotFound` on a read or a stat, and to a successful delete on a delete, on every provider. A
missing container is not a missing key: `Probe` and every object operation report it as
`ErrContainerNotFound`, which never matches `ErrNotFound`, so a consumer serves an absent object as
a 404 and a lost container as an outage. `storagetest.RunMissingContainer` proves the
classification for each provider.

## The write path

No transaction spans a database and an object store. `Put` is all or nothing, which lets a
consumer write in two phases: the owning row first, in a pending state; then the object; then the
row marked available. Delete mirrors it. Writing the object first was rejected, because a failed
insert then leaves an object that only a full container listing can find, while a pending row is
an ordinary query. A crash between the two phases leaves a pending row, and nothing reconciles it.
The row and its schema belong to the consumer.
