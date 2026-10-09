// Package storagetest is the test support for storage.Client: an in-memory
// [Fake] that a consumer's hermetic tests run over, and [Run] and
// [RunMissingContainer], the conformance checks a provider sub-module runs
// against its own adapter. It imports nothing outside the standard library
// and the base module.
//
// # The Fake
//
// [NewFake] returns a Fake whose container exists and that holds no objects.
// A storage.Store wraps a Fake the way it wraps a provider. Each [Option]
// changes one default at construction:
//
//   - [WithCapabilities] replaces the default key rules, whose length cap
//     is [DefaultMaxKeyLength].
//   - [WithClock] sets the clock that stamps ModifiedAt, in UTC.
//   - [WithPageSize] sets the page size of a List without a Limit.
//   - [WithoutContainer] starts the Fake with no container.
//
// Beyond the storage.Client methods, a Fake has methods that cause faults
// and report what it received:
//
//   - [Fake.SetDown] starts or ends an outage.
//   - [Fake.FailPut] makes every later Put fail with a given error.
//   - [Fake.DropContainer] deletes the container and its objects.
//   - [Fake.HasContainer] reports whether the container exists.
//   - [Fake.Puts], [Fake.Ensures], and [Fake.Probes] count calls.
//   - [Fake.LastPut] and [Fake.LastList] report the most recent call's
//     options, and [Fake.LastEnsure] and [Fake.LastProbe] its context
//     deadline.
//
// A failing Fake wraps a cause under the storage sentinel: [ErrDown] under
// ErrUnavailable, [ErrNoSuchKey] under ErrNotFound, and [ErrNoSuchContainer]
// under ErrContainerNotFound.
//
// # Conformance
//
// [Run] proves a Client implementation against the contract the interface
// documents. A provider calls it from its own test with a constructor that
// returns a client wired to a test container, which need not exist yet:
//
//	func TestConformance(t *testing.T) {
//		endpoint := os.Getenv("STORAGE_TEST_ENDPOINT")
//		if endpoint == "" {
//			t.Skip("STORAGE_TEST_ENDPOINT not set")
//		}
//		storagetest.Run(t, func(t *testing.T) storage.Client {
//			c, err := provider.New(providerConfig(endpoint))
//			if err != nil {
//				t.Fatalf("new client: %v", err)
//			}
//			return c
//		})
//	}
//
// A provider runs [RunMissingContainer] too, over a client wired to a
// container that does not exist.
package storagetest
