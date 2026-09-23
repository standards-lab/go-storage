# Provider assumptions

The claims below rest on documentation, not on a build. A build that contradicts one invalidates
the text that states it.

`azureblob` passes the conformance suite against Azurite, started with `--skipApiVersionCheck`,
and no live Azure account has run it:

- **`ValidateKey`'s rules** come from Azure's documentation. Azurite accepts every key that breaks
  them, so only unit tests cover them.
- **The listing ETag form.** Azure is documented to leave the tag unquoted in a listing's XML and
  quote it in headers, which is why the adapter quotes it.
- **The listing marker.** The adapter passes `NextMarker` back verbatim and never reads it.
  Azurite's is the last key of the page, and Azure's is documented as an opaque token.

No S3 provider exists, so two claims the standard tier rests on are unexercised:

- **`CreateBucket`** reports an existing bucket distinguishably, which `EnsureContainer` needs.
- **Multipart uploads** stay invisible until they commit, which the all-or-nothing `Put` needs.
