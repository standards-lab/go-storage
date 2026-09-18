// Package storage is the object storage infrastructure service: the
// standard-tier [Client] interface over the operations Azure Blob and S3
// share, a lifecycle-integrated wrapper over a provider's Client, and the
// configuration block that bounds it. The package depends on the standard
// library and go-core's config package alone. A provider implements [Client]
// over its own SDK in a separate sub-module, so a consumer imports its
// provider once, at the composition root, and the base module never imports
// a provider's SDK. No provider has been released yet.
//
// # The standard tier
//
// [Client] has five object operations (Put, Get, Stat, Delete, and List) plus
// EnsureContainer, Probe, and Capabilities. EnsureContainer creates the
// configured container and succeeds when it already exists; it never deletes
// or reconfigures one. [Client] offers no conditional writes and no object
// metadata beyond ContentType, so the owning database row remains the
// authority for an object's metadata and for concurrent updates. A feature
// that only some providers offer, such as leases, access tiers, and presigned
// URLs, is reached through the provider's own handle and never through
// [Client].
//
// # Store
//
// [New] wraps a provider's Client with a finalized [Config]. It performs no
// I/O. A nil Client or an unfinalized Config panics with the fix named,
// because a composition root wires both and no runtime condition produces
// either. [Store] implements [Client], so a consumer holds a *Store and calls
// the same operations. Every object operation returns [ErrNotReady] before a
// successful Start or after Shutdown, and otherwise delegates to the
// provider under the caller's context. Store applies no timeout of its own to
// an object operation, so the caller's context and the provider's transport
// govern each call.
//
// # Lifecycle wiring
//
// The package registers no lifecycle hooks of its own. [Store.Start] and
// [Store.Shutdown] carry the lifecycle package's hook signature, and
// [Store.Ready] satisfies lifecycle.ReadinessChecker structurally, so a
// composition root registers the store as a service:
//
//	lc.Add(lifecycle.Service{
//		Name:     "storage",
//		Stage:    0,
//		Start:    store.Start,
//		Shutdown: store.Shutdown,
//		Check:    store,
//	})
//
// Start ensures the configured container exists and then probes the provider,
// both bounded by the configured request timeout, so an empty store starts
// cleanly and an unreachable one fails startup rather than serving traffic
// unready. [Store.EnsureContainer] repeats the container step on demand,
// without a readiness check, so a container deleted while the process runs
// can be recovered. Shutdown clears readiness and closes the provider when it
// implements io.Closer. It closes the provider at most once across repeated
// calls, and it is safe before Start and after a failed Start.
//
// # Readiness
//
// [Store.Ready] reports live connectivity: false before Start or after
// Shutdown, and otherwise the result of a probe bounded by the request
// timeout. A readiness probe that aggregates the store therefore fails during
// an outage and recovers when the provider does, at the cost of one bounded
// round trip per call.
//
// # Configuration
//
// [Config] holds the container, endpoint, credential, limits, and probe
// timeout, and implements the config package's Merge and Finalize contract,
// so it loads as part of an application's configuration. Container is the one
// required field. Key is the shared-key credential and belongs in the secrets
// layer of config.Load. Endpoint, Account, and Options are provider facts that
// the base module leaves without defaults.
//
// A library ships no policy numbers, so MaxObjectSize and ListPageSize have no
// default. A value of 0 means unbounded for MaxObjectSize and the provider's
// own page size for ListPageSize. RequestTimeout is the one default, 10
// seconds, and it bounds only the calls Store makes on its own behalf in
// Start and Ready.
// Finalize composes the override names from the prefix it receives (through
// [NewEnv], recorded on [Env] for introspection), and an empty prefix
// disables the overrides.
//
// # Writing objects
//
// When MaxObjectSize is set, [Store.Put] rejects a declared size over the
// bound before calling the provider, and it reads every other body through a
// bound that fails on the first byte past it. An oversize body returns
// [ErrTooLarge]. The provider can have written part of the object before the
// body ran out, so a rejected upload can leave a partial object behind.
//
// No transaction spans a database and an object store. A consumer that
// records an object in a database writes the owning row first in a pending
// state, then the object, then marks the row available. A failed write then
// leaves a row that an ordinary query finds, where the reverse order would
// leave an object nothing references.
//
// # Keys
//
// [Capabilities] states what the provider requires of a key: a maximum length
// and a ValidateKey function. [Store.Capabilities] returns the provider's
// value without a readiness check. A consumer that builds keys from a
// variable segment calls ValidateKey before Put, so a key the provider would
// reject fails at construction instead of at the object store.
//
// # Errors
//
// [ErrNotFound], [ErrTooLarge], [ErrNotReady], and [ErrUnavailable] classify
// the service conditions. Each is wrapped in the dual form
// fmt.Errorf("%w: %w", sentinel, err), so errors.Is classifies while the
// provider's error stays recoverable. Classifying a provider's errors into
// the sentinels is the adapter's job. Get and Stat of a missing key match
// [ErrNotFound], and Delete of a missing key succeeds.
package storage
