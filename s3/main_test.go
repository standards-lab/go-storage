package s3_test

import (
	"os"
	"testing"
	"time"
	_ "time/tzdata" // Europe/London loads on a host without a zone database.
)

// TestMain runs the package's tests with time.Local set to Europe/London,
// so a time the package returned in time.Local, not time.UTC, fails the
// tests' == comparisons whatever zone the host runs in. London abbreviates
// GMT, so Go's time.Parse places a date that names GMT in time.Local there
// in winter, and in a fixed "GMT" zone otherwise; == catches either.
func TestMain(m *testing.M) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		panic(err)
	}
	time.Local = london
	os.Exit(m.Run())
}
