# go-storage context

go-storage is the object storage infrastructure library of Go Elemental: the standard-tier
`Client` interface over the operations Azure Blob and S3 share, the `Store` lifecycle wrapper,
the `Capabilities` provider constraints, the error sentinels, and the `storagetest` conformance
suite, with each provider isolated in its own sub-module.

The README and each package's `doc.go` document this repository. The
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md)
standard states the principles it follows. The settled design this repository builds to — the
standard-tier interface, the module layout, and the two-phase write — is
`standards-lab/context/design/storage-strategy.md`, the workspace coordinator's record; this
context records only working knowledge the code and the README do not express.

## Capability map

The built packages are authoritative through their code and `doc.go`. Detail for what is unbuilt
is added when it is about to be built.

- **The standard tier, `Store`, and `Config`** — the base module. Built and released.
- **`storagetest`** — the in-memory `Fake` and the `Run` conformance suite. Built and released.
- **`azureblob`** — the Azure Blob Storage provider sub-module. Built and released.

## What no build has proven

`azureblob` passes the conformance suite against Azurite, started with `--skipApiVersionCheck`,
and no live Azure account has run it. Three things rest on Azure's documentation alone. A build
against a live account that contradicts one invalidates the text that states it.

- **`ValidateKey`'s rules.** Azurite accepts every key that breaks them, so only unit tests cover
  them.
- **The listing ETag form.** Azure is documented to leave the tag unquoted in a listing's XML and
  quote it in headers, which is why the adapter quotes it.
- **The listing marker.** The adapter passes `NextMarker` back verbatim and never reads it.
  Azurite's is the last key of the page, and Azure's is documented as an opaque token.

## Known limits of the adapter

- `Get` returns the SDK's raw response body, not its retrying reader, so a connection that drops
  mid-body fails the read and nothing retries it.
- `Delete` sends no snapshot option, so a blob that has snapshots fails with a 409. Nothing in
  this repository creates a snapshot.

## Not built

- **An S3 provider.** It waits for a consumer that earns it (`backlog.second-providers`).
- **Managed identity.** The provider authenticates with the account's shared key, and the
  deployment goal brings managed identity.
