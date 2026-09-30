// Package storage is the object storage infrastructure service. It defines
// the standard-tier [Client] interface over the operations Azure Blob and S3
// share, the [Store] lifecycle wrapper over a provider's Client, and the
// [Config] that bounds it. It depends on the standard library and go-core
// alone. A provider implements Client over its SDK in a sub-module that a
// consumer imports once, at the composition root; the azureblob sub-module
// is the Azure Blob provider.
//
// # The standard tier
//
// [Client] has five object operations (Put, Get, Stat, Delete, and List)
// plus EnsureContainer, Probe, and Capabilities. It offers no conditional
// writes and no object metadata beyond ContentType, so the owning database
// row stays the authority for both; a consumer reaches a feature only some
// providers offer through the provider's own handle. The operations exchange
// these types:
//
//   - [Object] is the metadata of one stored object.
//   - [Blob] is an open read: an Object and its Body.
//   - [PutOptions] carries a Put body's content type and declared size.
//   - [GetOptions] is reserved for options on Get.
//   - [ListOptions] selects a listing by prefix, token, and limit.
//   - [Page] is one page of a listing and the token for the next.
//   - [Capabilities] states the provider's key rules.
//
// # Store
//
// [New] wraps a provider's Client in a [Store], which implements [Client],
// gates it on [Store.Start] and [Store.Shutdown], and enforces the limits
// [Store.Put] states. [Store.Ready] probes the provider live, so readiness
// drops during an outage and recovers with the provider. [Store.Container]
// returns the configured container name. A composition root registers the
// Store with go-core's lifecycle package:
//
//	lc.Add(lifecycle.Service{
//		Name:     "storage",
//		Stage:    0,
//		Start:    store.Start,
//		Shutdown: store.Shutdown,
//		Check:    store,
//	})
//
// # Configuration
//
// [Config] loads through go-core's Merge and Finalize contract:
// [Config.Merge] overlays one layer onto another, [Config.Finalize] applies
// defaults and environment overrides and validates, and [Config.Finalized]
// reports whether the last Finalize succeeded. Container is required; see
// [Config] for defaults. [Env] holds the override variable names, and
// [NewEnv] composes them from a prefix.
//
// # Writing objects
//
// [Client.Put] is all or nothing. A consumer that records an object in a
// database writes the owning row first in a pending state, then the object,
// then marks the row available.
//
// # Keys and errors
//
// [Capabilities.ValidateKey] states the provider's key rules, and
// [Store.Put] applies it before the provider sees a key. Five sentinels
// classify errors:
//
//   - [ErrNotFound]: no object exists at the key.
//   - [ErrContainerNotFound]: the configured container does not exist.
//   - [ErrUnavailable]: the store is unreachable.
//   - [ErrTooLarge]: a Put body exceeds Config.MaxObjectSize.
//   - [ErrNotReady]: a Store call came before a successful Start or after
//     Shutdown.
//
// A provider classifies its errors into the first three as [Client]
// documents, and [Store] adds the last two.
package storage
