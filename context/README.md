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
  types and interface. Built. `Client` carries `Capabilities()` so every provider declares its
  key constraints and a consumer reaches them through `Store`; the strategy record's first draft
  showed `Capabilities` only as a standalone type. `GetOptions` is empty on purpose, with a byte
  range as its anticipated first field. `MaxKeyLength` states no unit, because Azure and S3 may
  measure a key differently, so each provider's `doc.go` states its own.
- **`Config`** — the container, endpoint, credentials, size bound, page size, and probe timeout,
  on `go-core`'s Merge-and-Finalize contract. Built. `MaxObjectSize` and `ListPageSize` are plain
  values where 0 means unset, so an overlay file cannot lift a bound back to unbounded and the
  environment override can. `RequestTimeout` is the only pointer, so `Finalize` can tell an
  absent value from a configured one.
- **`Store`** — the lifecycle wrapper implementing `Client`. Built. Two cautions carry to the
  provider tasks. A provider that trusts `PutOptions.Size` and reads exactly that many bytes
  never trips the bound on a longer body; it stores at most `Size` bytes, so the bound holds, but
  it truncates silently, and the adapter should read to EOF or document that `Size` caps what it
  stores. With a bound configured, every `Put` body arrives wrapped and non-seekable, so a
  provider that needs a seekable body to sign a request (the S3 case) must sign by the declared
  length.
- **The in-memory fake** — `fake_test.go`, in the external test package, wrapped by `Store`'s
  tests. Built. It stays a test file because nothing in it is API. Publishing it as `storagetest`
  with a conformance suite for `Client` implementations waits for the `azureblob` provider,
  when a second test package needs it.
