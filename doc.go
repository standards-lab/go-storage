// Package storage is the object storage infrastructure service: the
// standard-tier [Client] interface over the operations Azure Blob and S3
// share, the [Store] lifecycle wrapper over a provider's Client, and the
// [Config] that bounds it. It depends on the standard library and go-core
// alone. A provider implements [Client] over its SDK in a sub-module that a
// consumer imports once, at the composition root; azureblob is the Azure
// Blob provider.
//
// # The standard tier
//
// [Client] has five object operations (Put, Get, Stat, Delete, and List)
// plus EnsureContainer, Probe, and Capabilities. It offers no conditional
// writes and no object metadata beyond ContentType, so the owning database
// row stays the authority for both; a feature only some providers offer is
// reached through the provider's own handle.
//
// # Store
//
// [New] wraps a provider's Client in a [Store], which implements [Client],
// gates it on [Store.Start] and [Store.Shutdown], and enforces the limits
// [Store.Put] states. A composition root registers it with go-core's
// lifecycle package:
//
//	lc.Add(lifecycle.Service{
//		Name:     "storage",
//		Stage:    0,
//		Start:    store.Start,
//		Shutdown: store.Shutdown,
//		Check:    store,
//	})
//
// [Store.Ready] probes the provider live, so readiness drops during an
// outage and recovers with the provider.
//
// # Configuration
//
// [Config] loads through go-core's Merge and Finalize contract. Container is
// required; see [Config] for defaults and [Env] for override names.
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
// [Store.Put] applies it before the provider sees a key. A provider
// classifies its errors into [ErrNotFound], [ErrContainerNotFound], and
// [ErrUnavailable] as [Client] documents, and [Store] adds [ErrTooLarge] and
// [ErrNotReady].
package storage
