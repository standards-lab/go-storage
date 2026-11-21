package storage_test

import (
	"errors"
	"testing"

	"github.com/standards-lab/go-storage"
)

func TestSentinels_Distinct(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrNotFound", storage.ErrNotFound},
		{"ErrContainerNotFound", storage.ErrContainerNotFound},
		{"ErrTooLarge", storage.ErrTooLarge},
		{"ErrNotReady", storage.ErrNotReady},
		{"ErrUnavailable", storage.ErrUnavailable},
	}
	for _, s := range sentinels {
		for _, other := range sentinels {
			if got, want := errors.Is(s.err, other.err), s.name == other.name; got != want {
				t.Errorf("errors.Is(%s, %s) = %t, want %t", s.name, other.name, got, want)
			}
		}
	}
}
