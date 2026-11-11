// Package storagetest is the test support for storage.Client: an in-memory
// [Fake] that a consumer's hermetic tests run over, and [Run] and
// [RunMissingContainer], the conformance checks a provider sub-module runs
// against its own adapter. It imports nothing outside the standard library
// and the base module.
//
// # The Fake
//
// [NewFake] returns an empty Fake configured by an [Options] value. The zero
// Options gives a Fake whose container exists, and each field changes one
// default at construction:
//
//   - [Options.Capabilities] replaces the default key rules whole when its
//     MaxKeyLength or ValidateKey is set. The default caps a key's length at
//     [DefaultMaxKeyLength] runes and rejects an empty key.
//   - [Options.Clock] sets the clock that stamps ModifiedAt, in UTC.
//   - [Options.PageSize] sets the page size of a List without a Limit.
//   - [Options.NoContainer] starts the Fake with no container.
//
// A storage.Store wraps a Fake the way it wraps a provider.
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
