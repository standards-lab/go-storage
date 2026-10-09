// Package s3 is a go-storage provider over the S3 API. Its [Client]
// implements storage.Client over one bucket, built on aws-sdk-go-v2's
// service/s3 and feature/s3/transfermanager and authenticated with a static
// access key, and [New] constructs it. It is validated against SeaweedFS's
// S3 gateway. The sections below state how the Client maps storage's
// contract onto S3.
//
// # Construction
//
// [New] builds a [Client] from a finalized storage.Config without I/O.
// Container, Account, and Key are required: Container is the bucket, Account
// the access key ID, and Key the secret access key. Container must be a valid
// general purpose bucket name: 3 to 63 lowercase letters, digits, dots, and
// hyphens, starting and ending with a letter or digit, with no two dots
// together, not shaped like an IPv4 address, and free of the prefixes and
// suffixes S3 reserves.
//
//	client, err := s3.New(cfg)
//	if err != nil {
//		return err
//	}
//	store := storage.New(client, cfg)
//
// # Endpoints
//
// An empty Endpoint means AWS's own endpoint for the region, addressed
// virtual-hosted style as the SDK resolves it. A set Endpoint must be an
// absolute http or https URL. It becomes the SDK's base endpoint with
// path-style addressing, so the bucket is the first path segment, which is
// what an S3-compatible gateway such as SeaweedFS's expects.
//
// # Options
//
// Config.Options carries the provider's own settings, each overridable
// through storage.Env. Any key not listed here is ignored.
//
//   - region: the region the requests are signed for and, against AWS, the
//     region the bucket is created in. Unset means us-east-1. An S3-compatible
//     gateway usually accepts any region in the signature.
//   - max_retries: how many times the SDK retries a request that failed with
//     a transport error or a retryable answer (a 5xx status, a throttling
//     code), as a non-negative integer. 0 means one try. Unset keeps the SDK
//     default: three attempts, with jittered exponential backoff. The
//     backoff and the retry quota are the SDK standard retryer's defaults,
//     which its AWS_NEW_RETRIES_2026 environment variable changes when set
//     to "true".
//   - part_size: the size of one part of a multipart upload, in bytes, from
//     5 MiB (5242880), S3's smallest part, to 5 GiB (5368709120), its
//     largest. Unset means 8 MiB. Put sends a body of up to one part as a
//     single PutObject and a longer one as a multipart upload. An upload
//     has at most 10,000 parts, so a body of unknown size can be at most
//     10,000 parts long, 80 GiB at the default; a declared Size raises the
//     part size as far as it needs.
//   - try_timeout: the deadline on each try of a request, as a positive Go
//     duration such as "30s", so a stalled service cannot hold a call
//     indefinitely. Unset means no deadline, the SDK default. It bounds one
//     try from its send to the last byte of its answer: one PutObject, one
//     part of a multipart upload, or one GetObject together with the read
//     of its body. A try that passes its deadline is retried like a failed
//     connection, up to max_retries times, and a request whose every try
//     stalls fails with storage.ErrUnavailable. When a Get body's try
//     passes its deadline mid-read, or its connection fails, the body
//     resumes from its offset with a ranged GetObject conditioned with
//     If-Match on the first answer's ETag, as many times per read as
//     max_retries allows a request, the SDK's default of two retries when
//     it is unset, and each resumption is a try with a deadline of its own.
//     A download therefore outlasts try_timeout however slowly the caller
//     reads. With max_retries 0 the body does not resume. A body that
//     stalls on every try fails the read with storage.ErrUnavailable once
//     its resumptions are spent. try_timeout must exceed the longest part
//     upload, and the time a Get body's reader may pause between reads
//     without spending a resumption.
//   - concurrency: the number of parts of one multipart upload in flight
//     at once, from 1 to 32. Unset means 4.
//
// A malformed value is a construction error. The SDK's default checksum
// behavior is kept: a request carries a checksum when the operation
// supports one.
//
// A multipart Put reads at most part_size × (concurrency + 2) bytes of its
// body ahead of the parts the service has acknowledged, 48 MiB at the
// defaults: transfermanager's concurrency + 1 part buffers and its copy of
// the first part. It holds more in memory while it starts: Put's own buffer
// of part_size + 1 bytes, which decides between one PutObject and a
// multipart upload, is still held when transfermanager has copied the
// first part out of it and allocated its part buffers, so a Put holds up
// to part_size × (concurrency + 3) bytes, 56 MiB at the defaults. For a
// body of unknown size that buffer grows by doubling, to as much as twice
// its length, so such a Put holds up to part_size × (concurrency + 4)
// bytes, 64 MiB at the defaults. Once the first part is sent, a Put holds
// its concurrency + 1 part buffers. A declared Size beyond 10,000 parts
// raises the part size, as the part_size entry says, and every part
// buffer but Put's own grows with it.
//
// # Containers
//
// [Client.EnsureContainer] sends CreateBucket, with the region as its
// location constraint everywhere but us-east-1, which takes none. It treats
// BucketAlreadyOwnedByYou as success. It treats BucketAlreadyExists as
// success only when a HeadBucket that follows it succeeds, because some
// gateways answer an owner's re-create with that code, while AWS uses it for
// a bucket another account owns. It never deletes or reconfigures a bucket.
// [Client.Probe] sends HeadBucket, which proves the endpoint, the
// credential, and the bucket together.
//
// # Keys
//
// [Client.Capabilities] declares the S3 key rules: a key is non-empty valid
// UTF-8 of at most 1,024 bytes. MaxKeyLength counts bytes, not runes. Put,
// Get, Stat, and Delete check their key against those rules before they
// send anything, and return the rule's error for a key that breaks one.
//
// # Objects
//
// [Client.Put] is all or nothing: it sends a body of at most one part as a
// single PutObject and a longer one as a multipart upload through
// transfermanager, aborted on failure; its doc comment states how it reads
// the body, aborts, and reports ModifiedAt. An upload in progress is not an
// object: Get, Stat, and List do not see it until it completes, and its
// ETag then has the multipart form "<hex>-<parts>".
//
// [Client.Get] sends GetObject and streams its body, resuming a failed read
// as the try_timeout entry describes. The SDK validates a body's checksum,
// when the answer carries one, only once the whole object's body has been
// read; a ranged answer carries none for the object. A read that resumes
// is therefore not validated by checksum, neither the bytes before the
// resumption nor those after it. [Client.Stat] sends HeadObject.
// [Client.Delete] sends DeleteObject, which S3 answers with success for a
// missing key. [Client.List] sends ListObjectsV2, one page per call.
//
// Every operation reports the ETag in HTTP entity-tag form, adding the
// quotes to a value a gateway sends without them, so Put, Get, Stat, and
// List agree on one version's ETag. A listing's LastModified is truncated
// to the whole second, the precision of the Last-Modified header the
// others read.
//
// # Dependencies
//
// The module pins aws-sdk-go-v2's service/s3, its credentials and smithy-go,
// and feature/s3/transfermanager for multipart upload. transfermanager is
// v0 (v0.4.15), held as a stated exception the way go-observability holds
// otelhttp: AWS has not released it past v0, it is the maintained successor
// to the deprecated feature/s3/manager, it is maintained by the project
// that defines the ecosystem, and it adds no dependency beyond the SDK
// modules the package already compiles. A v0 minor may change its API, so
// an upgrade is checked against Put's use of it.
//
// # Errors
//
// Every method classifies the SDK's error in the dual form, so errors.As
// still reaches the SDK's smithy.APIError and its HTTP response error.
// NoSuchBucket matches storage.ErrContainerNotFound, and so does a 404 on a
// bucket request, whose HEAD answer carries no code. NoSuchKey matches
// storage.ErrNotFound. [Client.Stat] tells a missing key from a missing
// bucket, which HeadObject answers with the same bare 404, by a HeadBucket
// that follows it. A Get body's failed read is classified as a request's
// failure is, but for one case: an object replaced before a resumption
// fails the resumption's If-Match with 412 PreconditionFailed, which
// matches storage.ErrNotFound, as a deleted object's NoSuchKey does,
// because the version being read is gone. A 5xx answer, the SlowDown,
// ServiceUnavailable, and InternalError codes, and a failure with no
// response, an expired deadline included, match storage.ErrUnavailable.
// The caller's cancellation and every other answer, an authentication
// failure included, pass through unclassified.
//
// # Acceptance against SeaweedFS
//
// The unit tests run against a scripted HTTP server. The acceptance tests
// run go-storage's conformance suite, storagetest.Run and
// storagetest.RunMissingContainer, storage.Store.Start, Probe,
// EnsureContainer, the object operations, multipart Put's visibility and
// abort, at 5 MiB parts, and a Get body's resumption past try_timeout and
// its failure when the object is replaced mid-read, against a real gateway
// when S3_TEST_ENDPOINT names its URL, each in a bucket of its own, with
// the access key admin and the secret secret. The repository's mise tasks
// start SeaweedFS with those credentials and run them:
//
//	mise run seaweedfs:start
//	mise run acceptance
//	mise run seaweedfs:stop
package s3
