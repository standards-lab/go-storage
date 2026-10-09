# Provider assumptions

The claims below rest on documentation, not on a build. A build that contradicts one invalidates
the text that states it.

`azureblob` passes the conformance suite against Azurite, started with `--skipApiVersionCheck`,
and no live Azure account has run it:

- **`ValidateKey`'s rules and the container-name rules `New` applies.** They come from Azure's
  documentation. Azurite accepts every key that breaks them, so only unit tests cover them.
- **The listing ETag form.** Azure is documented to leave the tag unquoted in a listing's XML and
  quote it in headers, which is why the adapter quotes it.
- **The listing marker.** The adapter passes `NextMarker` back verbatim and never reads it.
  Azurite's is the last key of the page, and Azure's is documented as an opaque token.

Three claims the standard tier rests on concern S3. The `s3` provider handles each, and only
SeaweedFS has run it, so no live AWS account has tested them:

- **`CreateBucket`'s existing-bucket result.** `EnsureContainer` needs `CreateBucket` to report an
  existing bucket distinguishably.
- **Multipart visibility.** The all-or-nothing `Put` needs a multipart upload to stay invisible
  until it commits.
- **A missing bucket on `Stat`.** The contract has `Stat` report a missing container as
  `ErrContainerNotFound`. S3's `HeadObject` answers a missing bucket with a bare 404 whose code
  cannot be told from a missing key's, so an S3 adapter needs a `HeadBucket` on a 404 to classify
  it. `GetObject`, `DeleteObject`, and `ListObjectsV2` name `NoSuchBucket` themselves.
