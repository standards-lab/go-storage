# go-storage context

go-storage is the object storage infrastructure library of Go Elemental: the standard-tier
`Client` interface over the operations Azure Blob and S3 share, the `Store` lifecycle wrapper,
the `Capabilities` provider constraints, the error sentinels, and the `storagetest` conformance
suite, with each provider isolated in its own sub-module.

The README, `docs/design.md`, and each package's `doc.go` document this repository. The
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md)
standard states the principles it follows. This context records only working knowledge the code
and the documentation do not express. [`docs/provider-assumptions.md`](../docs/provider-assumptions.md)
lists the provider claims no run against the live service has proven, and the tests that cover
each.

## Capability map

- **The standard tier, `Store`, and `Config`** (built): the base module ([doc.go](../doc.go)).
- **`storagetest`** (built): the in-memory `Fake` and the `Run` and `RunMissingContainer`
  conformance checks ([storagetest/doc.go](../storagetest/doc.go)).
- **`azureblob`** (built): the Azure Blob Storage provider sub-module
  ([azureblob/doc.go](../azureblob/doc.go)).
- **`s3`** (built): the S3 provider sub-module ([s3/doc.go](../s3/doc.go)).
- **Managed identity** (planned): `azureblob` authenticates with the account's shared key, and
  the deployment goal brings managed identity.
