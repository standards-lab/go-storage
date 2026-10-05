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
  application supplies them. The two defaults bound what no caller controls: `RequestTimeout`
  bounds the calls `Store` makes on its own behalf in `Start` and `Ready`, and `ReadIdleTimeout`
  bounds each read of a `Get`'s body, so a stalled store is cut off while a slow caller is not.
- Storage gates readiness: `Store` registers with the process lifecycle through a `Start`, a
  `Shutdown`, and a `Ready` check that probes the provider live. `Start` ensures the configured
  container exists before it probes, so an empty store starts cleanly.
- A write is all or nothing. A failed `Put` stores nothing and leaves an existing object
  unchanged, and a declared size that disagrees with the body is such a failure. The
  `storagetest` suite proves each provider keeps this.

[docs/design.md](docs/design.md) explains why the library is shaped as it is.

## Packages

- `storage` (the base module's root package) — `Client`, the standard-tier interface, with
  `Capabilities` and the error sentinels; `Config`, on go-core's Merge-and-Finalize contract;
  and `Store`, the lifecycle wrapper that implements `Client` and enforces the provider's key
  rules, the size bound, and a declared body size.
- `storagetest` (a package of the base module) — an in-memory `Fake` client for hermetic tests
  and `Run` and `RunMissingContainer`, the conformance checks a provider runs against its own
  `Client`.
- `azureblob` (a sub-module) — the Azure Blob Storage provider, over the Azure SDK for Go's
  `azblob` module and authenticated with the account's shared key.

## Development

The repository uses a Go workspace and [mise](https://mise.jdx.dev):

```
mise run check      # build, vet, format, fix, tidy, test, and lint every module; writes nothing
mise run currency   # report requirements, Go, tools, actions, and images behind their latest
mise run upgrade    # upgrade every module's requirements and the tools to their latest
```

The unit tests need no service. `azureblob`'s acceptance tests run the conformance suite against
a real service when `AZUREBLOB_TEST_ENDPOINT` is set; CI runs them against Azurite, and the
package documentation of `azureblob` shows how to start it locally.

## License

[Apache License 2.0](LICENSE).
