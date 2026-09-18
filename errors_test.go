package storage_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/standards-lab/go-storage"
)

var sentinels = []struct {
	name string
	err  error
	text string
}{
	{"ErrNotFound", storage.ErrNotFound, "storage object not found"},
	{"ErrTooLarge", storage.ErrTooLarge, "storage object too large"},
	{"ErrNotReady", storage.ErrNotReady, "storage not ready"},
	{"ErrUnavailable", storage.ErrUnavailable, "storage unavailable"},
}

func TestSentinels(t *testing.T) {
	for _, s := range sentinels {
		if got := s.err.Error(); got != s.text {
			t.Errorf("%s = %q, want %q", s.name, got, s.text)
		}
	}
}

// Each sentinel matches itself and none of the others, so errors.Is
// classifies unambiguously.
func TestSentinels_Distinct(t *testing.T) {
	for _, s := range sentinels {
		if !errors.Is(s.err, s.err) {
			t.Errorf("errors.Is(%s, %s) = false", s.name, s.name)
		}
		for _, other := range sentinels {
			if other.name == s.name {
				continue
			}
			if errors.Is(s.err, other.err) {
				t.Errorf("errors.Is(%s, %s) = true, want the sentinels distinct", s.name, other.name)
			}
		}
	}
}

// The dual-wrap form is the package's error contract: errors.Is classifies by
// sentinel while the provider's cause stays wrapped and matchable.
func TestSentinels_DualWrap(t *testing.T) {
	for _, s := range sentinels {
		cause := errors.New("provider cause")
		err := fmt.Errorf("%w: %w", s.err, cause)

		if !errors.Is(err, s.err) {
			t.Errorf("errors.Is(err, %s) = false", s.name)
		}
		if !errors.Is(err, cause) {
			t.Errorf("errors.Is(err, cause) = false for %s, want the cause to stay matchable", s.name)
		}
		for _, other := range sentinels {
			if other.name == s.name {
				continue
			}
			if errors.Is(err, other.err) {
				t.Errorf("errors.Is(err, %s) = true for %s, want the sentinels distinct", other.name, s.name)
			}
		}
	}
}
