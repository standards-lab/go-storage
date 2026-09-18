# go-storage

This repository holds the object storage infrastructure library for Standards Lab's Go Elemental
standard. It is managed with the marathon workflow; start from `context/README.md`.

## Documentation lives in the repository

This repository documents its own implementation: the README states its place in the standard
and the principles it enhances, and each package's `doc.go` is the authority for its API. The
organization's [architecture repository](https://github.com/standards-lab/architecture) states
the principles this repository follows and holds nothing a reader can infer from this source.
`context/` records only working knowledge the code and the README do not express; do not restate
documented design here. A change that alters documented behavior updates the README and the
package documentation in the same effort, and a design note that generalizes past this repository
is promoted to the architecture repository through its `context/`.

## Repository specifics

- **Module layout.** One base module is rooted at `github.com/standards-lab/go-storage`, holding
  `Config`, the `Store` lifecycle wrapper, the standard-tier `Client` interface, `Capabilities`,
  and the error sentinels at its root. The base module depends on the standard library and
  `go-core` alone and never imports a provider's SDK. Provider sub-modules, each with its own
  `go.mod`, are added as they are built.
- **Local development.** Development uses the committed root `go.work`. Pinned `require` versions
  are the committed steady state; a `replace` directive is only a transient bridge while a
  sub-module builds against unreleased base changes.
- **Standard conformance.** Dependencies, releases, CI, tests, and tasks follow the Go Elemental
  standard principles in the architecture repository (base `v*` and per-sub-module `<path>/v*`
  tags, per-module CI matrix, mise tasks looping over the modules).
- **Public repo.** Modules resolve through the public Go proxy; CI carries no private-module
  config.
