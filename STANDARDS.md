# go-storage standards

The judgement calls the standards-reviewer applies to go-storage, beyond what `mise run check` enforces.

- A change that alters documented behavior updates the README, `docs/design.md` and the affected `doc.go` in the same change.
- `architecture/standards/go-elemental/principles/dependencies.md`: the bottom-up line and no provider in a base, across the base module's, `azureblob`'s and `s3`'s `go.mod`, and the README's stated v0 exception for `s3`'s `transfermanager`.
- `architecture/standards/go-elemental/principles/tests-and-docs.md`: the doc.go inventory of `storage`, `storagetest`, `azureblob` and `s3`, and `storagetest` as the hoisted helper package the provider modules' tests consume.
- `architecture/standards/go-elemental/principles/topology-and-naming.md`: the base module with `storagetest`, and the `azureblob` and `s3` provider sub-modules and their tags.
- `architecture/standards/go-elemental/principles/release-and-ci.md`: the check, currency, the `acceptance` job and the `seaweedfs:start`, `seaweedfs:stop` and `acceptance` mise tasks, the root `go.work`, and a changelog per module.
- `architecture/standards/go-elemental/principles/lifecycle-and-context.md`: `Store`'s `Start`, `Shutdown` and `Ready`, and the constructors' panics on an unfinalized `Config` or a nil client.
- `architecture/standards/go-elemental/principles/timeouts.md`: `azureblob`'s and `s3`'s `try_timeout` and the `Get` body that resumes past it, under `Config.ReadIdleTimeout`; the retry budget of `max_retries` tries, spent into `ErrUnavailable`; and `Put`'s unclassified body failure, read `block_size` times `concurrency` ahead of the store on `azureblob` and `part_size` times (`concurrency` + 2) on `s3`.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: `Config`'s `MaxObjectSize`, `ListPageSize`, `RequestTimeout` and `ReadIdleTimeout`.
- `architecture/principles/service-tiers.md`: `Client` and the `azureblob` and `s3` adapters beneath it.
- `architecture/principles/context-architecture.md`: the README, `docs/design.md` and each `doc.go` are the homes.
