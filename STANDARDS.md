# go-storage standards

The judgement calls the standards-reviewer applies to go-storage.

- A change that alters documented behavior updates the README, `docs/design.md` and the affected `doc.go` in the same change.
- `architecture/standards/go-elemental/principles/dependencies.md`: the bottom-up line and no provider in a base, across the base module's and `azureblob`'s `go.mod`.
- `architecture/standards/go-elemental/principles/tests-and-docs.md`: the doc.go inventory of `storage`, `storagetest` and `azureblob`, and `storagetest` as the hoisted helper package the provider modules' tests consume.
- `architecture/standards/go-elemental/principles/topology-and-naming.md`: the base module at the root and each provider a sub-module named for its target API, `azureblob`, tagged `azureblob/v*`.
- `architecture/standards/go-elemental/principles/release-and-ci.md`: the check, currency, the `acceptance` job, the root `go.work`, and a changelog per module.
- `architecture/standards/go-elemental/principles/timeouts.md`: `azureblob`'s `try_timeout` and the `Get` body that resumes past it, under `Config.ReadIdleTimeout`; the retry budget of `max_retries` tries, spent into `ErrUnavailable`; and `Put`'s unclassified body failure, read `block_size` times `concurrency` ahead of the store.
- `architecture/standards/go-elemental/principles/baseline-standards.md`: `Config`'s `MaxObjectSize`, `ListPageSize`, `RequestTimeout` and `ReadIdleTimeout`.
- `architecture/principles/service-tiers.md`: `Client` is the standard tier, the operations the target object stores share, and each provider sub-module an adapter beneath it.
- `architecture/principles/context-architecture.md`: the README, `docs/design.md` and each `doc.go` are the homes; `context/` records only what they do not express.
