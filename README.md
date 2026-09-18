# go-storage

go-storage is the object storage infrastructure library of Go Elemental, the Standards Lab
organization's Go implementation of the Elemental Architecture. It holds the standard-tier
interface over the operations Azure Blob and S3 share, the lifecycle wrapper that bounds and
gates it, the provider key constraints, and the error sentinels a provider classifies into.

`github.com/standards-lab/go-storage` is the base module. Provider sub-modules are nested modules
that pin their SDKs and are released on their own tags; none has been built yet.

## Standard

`go-storage` is an infrastructure library of
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md), the
minimal-dependency Go standard. This README and each package's `doc.go` document the repository;
the standard's principles it enhances are stated below. Its repository-level principles:

- The base module depends on the standard library and `go-core` alone. A provider's SDK lives in
  that provider's own sub-module, and the base module never imports it.

## Packages

- `storage` (the base module's root package) — under construction.

## Development

The repository uses a Go workspace and [mise](https://mise.jdx.dev):

```
mise run build   # build every module standalone, with the workspace off
mise run test    # test every module
```

## License

[Apache License 2.0](LICENSE).
