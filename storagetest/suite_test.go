package storagetest_test

import (
	"testing"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"
)

func TestRun_Fake(t *testing.T) {
	storagetest.Run(t, func(*testing.T) storage.Client {
		return storagetest.NewFake()
	})
}

func TestRun_FakeWithoutContainer(t *testing.T) {
	storagetest.Run(t, func(*testing.T) storage.Client {
		return storagetest.NewFake(storagetest.WithoutContainer())
	})
}

func TestRun_Store(t *testing.T) {
	storagetest.Run(t, func(t *testing.T) storage.Client {
		cfg := storage.Config{Container: "conformance"}
		if err := cfg.Finalize(""); err != nil {
			t.Fatalf("finalize config: %v", err)
		}
		s := storage.New(storagetest.NewFake(storagetest.WithoutContainer()), cfg)
		if err := s.Start(t.Context()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		return s
	})
}

func TestRunMissingContainer_Fake(t *testing.T) {
	storagetest.RunMissingContainer(t, func(*testing.T) storage.Client {
		return storagetest.NewFake(storagetest.WithoutContainer())
	})
}

// A started Store whose container was dropped behind it passes the
// provider's classification through.
func TestRunMissingContainer_Store(t *testing.T) {
	storagetest.RunMissingContainer(t, func(t *testing.T) storage.Client {
		cfg := storage.Config{Container: "gone"}
		if err := cfg.Finalize(""); err != nil {
			t.Fatalf("finalize config: %v", err)
		}
		f := storagetest.NewFake()
		s := storage.New(f, cfg)
		if err := s.Start(t.Context()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		f.DropContainer()
		return s
	})
}
