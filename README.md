# go-storage

go-storage is the object storage infrastructure library of Go Elemental, the Standards Lab
organization's Go implementation of the Elemental Architecture. It holds the standard-tier
interface over the operations Azure Blob and S3 share, the lifecycle wrapper that bounds and
gates it, the provider key constraints, the error sentinels a provider classifies into, and the
test support a provider proves itself against.

`github.com/standards-lab/go-storage` is the base module. Provider sub-modules are nested modules
that pin their SDKs and are released on their own tags. `azureblob` is the Azure Blob Storage
provider, and it is the only one.

## Standard

`go-storage` is an infrastructure library of
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md), the
minimal-dependency Go standard. This README and each package's `doc.go` document the repository;
the standard's principles it enhances are stated below. Its repository-level principles:

- The base module depends on the standard library and `go-core` alone. A provider's SDK lives in
  that provider's own sub-module, and the base module never imports it.
- Limits are application policy. `MaxObjectSize` and `ListPageSize` have no default, and the
  application supplies them; the one default, `RequestTimeout`, bounds only the calls `Store`
  makes on its own behalf in `Start` and `Ready`.
- Storage gates readiness: `Store` registers with the process lifecycle through a `Start`, a
  `Shutdown`, and a `Ready` check that probes the provider live. `Start` ensures the configured
  container exists before it probes, so an empty store starts cleanly.
- A write is all or nothing. A failed `Put` stores nothing and leaves an existing object
  unchanged, and a declared size that disagrees with the body is such a failure. The
  `storagetest` suite proves each provider keeps this.

## Packages

- `storage` (the base module's root package) — `Client`, the standard-tier interface, with
  `Capabilities` and the error sentinels; `Config`, on go-core's Merge-and-Finalize contract;
  and `Store`, the lifecycle wrapper that implements `Client` and enforces the size bound and a
  declared body size.
- `storagetest` (a package of the base module) — an in-memory `Fake` client for hermetic tests
  and `Run`, the conformance suite a provider runs against its own `Client`.
- `azureblob` (a sub-module) — the Azure Blob Storage provider, over the Azure SDK for Go's
  `azblob` module and authenticated with the account's shared key.

## Development

The repository uses a Go workspace and [mise](https://mise.jdx.dev):

```
mise run build   # build every module standalone, with the workspace off
mise run test    # test every module
```

The unit tests need no service. `azureblob`'s acceptance tests run the conformance suite against
a real service when `AZUREBLOB_TEST_ENDPOINT` is set; the package documentation of `azureblob`
shows how to start Azurite for them.

## License

[Apache License 2.0](LICENSE).
