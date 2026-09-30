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
known `Content-Length` through keeps the common path free of buffering: a provider that must know
the length buffers the body only when `Size` is 0.

An empty `ContentType` stores `application/octet-stream` because that is what Azure stores for a
blob written without one. A provider sends the type itself rather than trusting its service's
default, so the object `Put` reports equals what `Stat` later reads on every provider.

## `Store` is the call surface

`Store` implements `Client` itself rather than only wrapping the provider. Nothing sits above the
store to carry the calls, so `Store` is where the size bound, the declared size, and the
readiness gate are enforced for every consumer.

`Store.Put` checks a key with the provider's `ValidateKey` before the provider sees it, so a bad
key is a local error rather than a provider's 400. It keeps its own size and declared-size checks
as defence in depth, although a conforming provider enforces the declared size too.

`Shutdown` is terminal. A provider may close its transport on `Shutdown`, so a later `Start` would
run over a closed client; `Start` returns `ErrNotReady` instead, including when a `Shutdown` lands
while its probe is in flight.

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
for itself; one timeout over every operation would cut off a large upload. `ReadIdleTimeout` has
a default because it bounds only the store's side of a download: the clock runs while a read of a
`Get`'s body is in progress, never between reads, so a slow client's download is never cut off.
A per-request deadline on the object operations belongs to the provider's transport instead:
`azureblob`'s `try_timeout` is that deadline, so a stalled store cannot hold a request
indefinitely. It bounds each try of a `Put`, a metadata call, or a `Get`'s body, not the whole
call with its retries. The SDK's retry reader resumes a `Get`'s body past a try's deadline, so
`try_timeout` is sized for one operation while a download runs as long as its caller reads.
`ReadIdleTimeout` bounds each read including its resumptions, so it is set above `try_timeout`,
which lets a stalled try resume once before the store cuts the read off.

`azureblob`'s upload defaults trade memory for requests. A 4 MiB block is four times the SDK's
1 MiB floor, so a multi-block body takes a quarter of the requests while the service's
50,000-block limit still admits about 195 GiB, and four workers overlap request latency on one
upload at 16 MiB per `Put`. The block ceiling is 100 MiB, the largest the service accepted before
version 2019-12-12, and the worker ceiling of 32 already reaches 128 MiB per `Put` at the default
block. The SDK allocates each block buffer with an anonymous mmap as it is needed, so a body
shorter than one block holds one buffer. A process holds up to that per-`Put` figure once for each
of its concurrent `Put`s.

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
