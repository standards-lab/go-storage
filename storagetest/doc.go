// Package storagetest is the test support for storage.Client: an in-memory
// [Fake] a consumer's hermetic tests run over, and [Run], the conformance
// suite a provider sub-module runs against its own adapter. It imports the
// testing package, as a test-support package does, and nothing outside the
// standard library and the base module.
//
// [NewFake] returns a Fake whose container exists and that holds no objects.
// It honors the Client contract, so a storage.Store wraps it the way it wraps
// a provider, and it records what it received: LastPut, LastList, LastProbe,
// and LastEnsure report the most recent call, and Puts, Probes, and Ensures
// count them. Its Down toggle fails every method with storage.ErrUnavailable,
// FailPut fails the next Puts after their body is read, and DropContainer
// removes the container so a test can watch EnsureContainer recover it. A
// test that needs one method to misbehave embeds *Fake in its own type and
// overrides that method.
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
// Every key the suite writes sits under a random prefix, and each subtest
// deletes what it wrote, so the suite runs against a shared container that
// holds other objects. The Fake passes the suite, and the package's own tests
// prove that a client which breaks the contract fails it.
package storagetest
