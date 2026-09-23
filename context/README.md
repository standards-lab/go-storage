# go-storage context

go-storage is the object storage infrastructure library of Go Elemental: the standard-tier
`Client` interface over the operations Azure Blob and S3 share, the `Store` lifecycle wrapper,
the `Capabilities` provider constraints, the error sentinels, and the `storagetest` conformance
suite, with each provider isolated in its own sub-module.

The README, `docs/design.md`, and each package's `doc.go` document this repository. The
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md)
standard states the principles it follows. This context records only working knowledge the code
and the documentation do not express. `provider-assumptions.md` lists the claims no build has
proven.

## Capability map

Every package below is built, and its code and `doc.go` are authoritative. Detail for what is
unbuilt is added when it is about to be built.

- **The standard tier, `Store`, and `Config`**: the base module.
- **`storagetest`**: the in-memory `Fake` and the `Run` conformance suite.
- **`azureblob`**: the Azure Blob Storage provider sub-module.

## Known limits of the adapter

- `Get` returns the SDK's raw response body, not its retrying reader, so a connection that drops
  mid-body fails the read and nothing retries it.
- `Delete` sends no snapshot option, so a blob that has snapshots fails with a 409. Nothing in
  this repository creates a snapshot.

## Not built

- **An S3 provider.** It waits for a consumer that earns it (`backlog.second-providers`).
- **Managed identity.** The provider authenticates with the account's shared key, and the
  deployment goal brings managed identity.
