# go-storage context

go-storage is the object storage infrastructure library of Go Elemental: the standard-tier
`Client` interface over the operations Azure Blob and S3 share, the `Store` lifecycle wrapper,
the `Capabilities` provider constraints, and the error sentinels, with each provider isolated in
its own sub-module.

The README and each package's `doc.go` document this repository. The
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md)
standard states the principles it follows. The settled design this repository builds to — the
standard-tier interface, the module layout, and the two-phase write — is
`standards-lab/context/design/storage-strategy.md`, the workspace coordinator's record; this
context records only working knowledge the code and the README do not express.

## Capability map

The built packages are authoritative through their code and `doc.go`. Detail for what is unbuilt
is added when it is about to be built.

- **Sentinels and the `Client` interface** — the four error sentinels and the standard-tier
  types and interface. Not yet built.
- **`Config`** — the container, endpoint, credentials, size bound, page size, and probe timeout,
  on `go-core`'s Merge-and-Finalize contract. Not yet built.
- **`Store`** — the lifecycle wrapper implementing `Client`. Not yet built.
