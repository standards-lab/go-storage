package azureblob_test

import (
	"os"
	"testing"
	"time"
	_ "time/tzdata" // Europe/London loads on a host without a zone database.
)

// TestMain runs the package's tests with time.Local set to Europe/London,
// a zone that abbreviates GMT. There Go's time.Parse places a date that
// names GMT in time.Local, and elsewhere in a fixed "GMT" zone, so a
// ModifiedAt the provider failed to convert to time.UTC fails the tests'
// == comparisons whatever zone the host runs in.
func TestMain(m *testing.M) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		panic(err)
	}
	time.Local = london
	os.Exit(m.Run())
}
