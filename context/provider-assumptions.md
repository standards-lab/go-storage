# Provider assumptions

The claims below rest on documentation or on an emulator, not on the live service. A run against
the service that contradicts one invalidates the text that states it.

`azureblob` passes the conformance suite against Azurite, started with `--skipApiVersionCheck`,
and no live Azure account has run it:

- **`ValidateKey`'s rules and the container-name rules `New` applies.** They come from Azure's
  documentation. Azurite accepts every key that breaks them, so only unit tests cover them.
- **The listing ETag form.** Azure is documented to leave the tag unquoted in a listing's XML and
  quote it in headers, which is why the adapter quotes it.
- **The listing marker.** The adapter passes `NextMarker` back verbatim and never reads it.
  Azurite's is the last key of the page, and Azure's is documented as an opaque token.

Four claims the standard tier rests on concern S3. `s3` passes the conformance suite against
SeaweedFS 4.48's S3 gateway, the image `compose/seaweedfs/Dockerfile` pins, started with
`-s3.autoCreateBucket=false`, and each claim has a test against it; no live AWS account has run
them:

- **`CreateBucket`'s existing-bucket result.** `EnsureContainer` needs `CreateBucket` to report an
  existing bucket distinguishably. SeaweedFS answers an owner's re-create with 409
  `BucketAlreadyOwnedByYou`, which `TestAcceptance_EnsureContainerExisting` proves
  `EnsureContainer` treats as success, twice, over a versioned bucket holding an object, changing
  none of it.
  `TestEnsureContainer_AlreadyOwnedByYou`, `TestEnsureContainer_AlreadyExistsAndReachable`, and
  `TestEnsureContainer_AlreadyExistsElsewhere` cover the codes against a scripted service. AWS is
  documented to answer an owner's re-create with 200 in us-east-1, untested.
- **Multipart visibility.** The all-or-nothing `Put` needs a multipart upload to stay invisible
  until it commits. `TestAcceptance_MultipartInvisibleUntilComplete` holds a 12 MiB `Put` at 5 MiB
  parts open: the gateway lists the upload while `Stat` answers `ErrNotFound` and `List` lacks
  the key, and the completed object reports a `"<hex>-3"` ETag.
  `TestAcceptance_MultipartFailureLeavesNothing` fails one midway, by its body, a short declared
  size, and cancellation, and finds no object and no open upload. `TestPut_MultipartFailuresAbort`,
  `TestPut_CancelledMultipartStillAborts`, and `TestPut_FailedAbortIsRepeated` cover the aborts
  against a scripted service.
- **A missing bucket on `Stat`.** The contract has `Stat` report a missing container as
  `ErrContainerNotFound`. S3's `HeadObject` answers a missing bucket with a bare 404 whose code
  cannot be told from a missing key's, so the adapter sends a `HeadBucket` on a 404 to classify
  it. `GetObject`, `DeleteObject`, and `ListObjectsV2` name `NoSuchBucket` themselves.
  `TestAcceptance_StatMissingBucket` and `TestAcceptance_MissingContainer` prove it against
  SeaweedFS, whose answers match AWS's documented ones here, and
  `TestStat_HeadBucketDisambiguatesA404` and `TestStat_OtherFailuresSkipTheBucketCheck` against a
  scripted service.
- **A conditional ranged `GetObject`.** A `Get`'s body resumes with `Range: bytes=<offset>-` and
  `If-Match` on the first answer's ETag, so it needs the service to honor both: to answer from the
  offset, and to fail with 412 once the object is replaced.
  `TestAcceptance_GetResumesPastTheTryTimeout` and `TestAcceptance_GetOfAReplacedObjectIsNotFound`
  prove both against SeaweedFS through a recording proxy. `TestGet_ResumesABodyPastTheTryTimeout`,
  `TestGet_AnObjectChangedMidReadIsNotFound`, `TestGet_AStalledBodyFailsOnceItsRetriesAreSpent`,
  and `TestGet_AResumptionThatIgnoresTheRangeFails` cover the resumption against a scripted
  service. AWS documents the same `Range` and `If-Match` semantics, untested.

SeaweedFS differs from AWS where the tests had to accommodate it or where the adapter relies on
neither behavior. Each SeaweedFS side was observed against 4.48, in the s3 spike or the acceptance
tests; each AWS side is from AWS's documentation and untested:

- **A write to a missing bucket.** SeaweedFS creates the bucket on an admin's upload unless
  `-s3.autoCreateBucket=false`; AWS answers `NoSuchBucket`. With the default on, the
  missing-container checks cannot pass, so the harness turns it off.
- **Readiness.** SeaweedFS's gateway answers unsigned requests before it has loaded its admin
  credential, so the harness waits for a signed listing.
- **The signature region.** SeaweedFS accepts any region in a signature; AWS requires the
  bucket's. `TestProbe_RegionOption` proves the option signs the request, but no run shows a
  wrong region failing.
- **Reserved bucket names.** SeaweedFS accepts names AWS reserves, so only `New`'s unit tests
  cover the rejection.
- **A listing's `LastModified`.** SeaweedFS gives whole seconds and AWS milliseconds; the adapter
  truncates to the second everywhere, which only the AWS side would exercise.
- **The continuation token.** SeaweedFS's is the page's last key, and AWS's is documented as
  opaque. The adapter passes it back verbatim and never reads it.
- **A repeated abort.** SeaweedFS answers an abort of an aborted or completed upload with success,
  and AWS with `NoSuchUpload`. The adapter's repeated abort treats `NoSuchUpload` as success, and
  no test answers it so.
- **The declared object size.** SeaweedFS ignores a wrong `x-amz-mp-object-size` that AWS rejects;
  the adapter checks a declared `Size` itself and relies on neither.

One constraint is the protocol's, not the gateway's: SigV4 signs `Host`, so the recording proxy
the resumption tests read through forwards the incoming `Host` rather than rewriting it. Azure's
shared key leaves the host unsigned, so a proxy in front of Azurite may rewrite it.
