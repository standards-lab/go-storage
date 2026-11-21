package s3_test

import (
	"os"
	"testing"
	"time"
	_ "time/tzdata"
)

// See docs/design.md.
func TestMain(m *testing.M) {
	london, err := time.LoadLocation("Europe/London")
	if err != nil {
		panic(err)
	}
	time.Local = london
	os.Exit(m.Run())
}
