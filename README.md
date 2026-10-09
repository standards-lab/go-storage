# go-storage

go-storage is the object storage infrastructure library of Go Elemental, the Standards Lab
organization's Go implementation of the Elemental Architecture. It holds the standard-tier
interface over the operations Azure Blob and S3 share, the lifecycle wrapper that bounds and
gates it, the provider key constraints, the error sentinels a provider classifies into, and the
test support a provider proves itself against.

`github.com/standards-lab/go-storage` is the base module. Provider sub-modules are nested modules
that pin their SDKs and are released on their own tags: `azureblob` is the Azure Blob Storage
provider, and `s3`, the second provider, is the S3 provider. Both pass the conformance suite,
which validates the standard tier.

## Standard

`go-storage` is an infrastructure library of
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md), the
minimal-dependency Go standard. This README and each package's `doc.go` document the repository;
the standard's principles it enhances are stated below. Its repository-level principles:

- The base module depends on the standard library and `go-core` alone. A provider's SDK lives in
  that provider's own sub-module, and the base module never imports it.
- `s3` pins aws-sdk-go-v2's `feature/s3/transfermanager`, the S3 SDK's transfer companion, for
  multipart upload, as a stated v0 exception. AWS has not released it past v0, and it passes
  every other marker of an industry-standard library. It solves a specification, S3's multipart
  upload protocol. `Put` hides it behind `io.Reader` and `context.Context`, so it can be removed
  without touching callers. It adds no module beyond the SDK modules `s3` already compiles. The
  project that defines the ecosystem maintains it, as the successor to the deprecated
  `feature/s3/manager`. A v0 minor may change its API, so an upgrade is checked against `Put`.
- Limits are application policy. `MaxObjectSize` and `ListPageSize` have no default, and the
  application supplies them. The two defaults bound what no caller controls: `RequestTimeout`
  bounds the calls `Store` makes on its own behalf in `Start` and `Ready`, and `ReadIdleTimeout`
  bounds each read of a `Get`'s body, so a stalled store is cut off while a slow caller is not.
- Storage gates readiness. `Store` is the value of a node in a go-core `graph`. The `lifecycle`
  Coordinator starts it with `Start`, shuts it down with `Shutdown`, and lists its `Ready` check,
  which probes the provider live, among its checks. The Coordinator infers all three from
  `Store`'s methods, so `Store` needs no adapter. `Start` ensures the configured container exists
  before it probes, so an empty store starts cleanly.
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
- `s3` (a sub-module) — the S3 provider, over aws-sdk-go-v2 and authenticated with a static
  access key.

## Development

The repository uses a Go workspace and [mise](https://mise.jdx.dev):

```
mise run check      # build, vet, format, fix, tidy, test, and lint every module; writes nothing
mise run currency   # report requirements, Go, tools, actions, and images behind their latest
mise run upgrade    # upgrade every module's go directive and requirements, and the tools, to their latest
```

The unit tests need no service. Each provider's acceptance tests run the conformance suite against
a real service when its endpoint variable is set, and skip otherwise: `azureblob`'s read
`AZUREBLOB_TEST_ENDPOINT` and `s3`'s read `S3_TEST_ENDPOINT`. The acceptance harness is a compose
stack with one service per provider, Azurite and SeaweedFS, each keeping its data in memory. Each
service's image is defined by `compose/<service>/Dockerfile`, whose `FROM` line is its one pin, and
CI runs the same `acceptance` task:

```
mise run up                    # start every harness service and wait until each is healthy
mise run down                  # stop the harness, discarding its data
mise run acceptance            # run every provider's acceptance tests, each against a fresh service
mise run acceptance:s3         # run s3's tests, acceptance included, against SeaweedFS
mise run acceptance:azureblob  # run azureblob's tests, acceptance included, against Azurite
```

## License

[Apache License 2.0](LICENSE).
