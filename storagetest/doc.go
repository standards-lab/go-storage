// Package storagetest is the test support for storage.Client: an in-memory
// [Fake] a consumer's hermetic tests run over, and [Run] and
// [RunMissingContainer], the conformance checks a provider sub-module runs
// against its own adapter. It imports nothing outside the standard library
// and the base module.
//
// [NewFake] returns a Fake whose container exists and that holds no objects.
// A storage.Store wraps it the way it wraps a provider. Its methods report
// the calls it received and let a test cause an outage, a failed Put, or a
// lost container.
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
