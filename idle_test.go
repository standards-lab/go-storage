package storage_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"
)

// idleLimit is the ReadIdleTimeout the idle tests' store carries.
const idleLimit = 50 * time.Millisecond

// stallingClient serves a Get whose body sends head and then stalls: its
// next read blocks until the Get's context is done, as a provider's body
// read unblocks when its request is cancelled. It records that context.
type stallingClient struct {
	*storagetest.Fake
	head string
	ctx  context.Context
}

func (c *stallingClient) Get(ctx context.Context, key string, _ storage.GetOptions) (storage.Blob, error) {
	c.ctx = ctx
	return storage.Blob{
		Object: storage.Object{Key: key, Size: int64(len(c.head)) + 1},
		Body:   io.NopCloser(io.MultiReader(strings.NewReader(c.head), stall{ctx})),
	}, nil
}

type stall struct{ ctx context.Context }

func (s stall) Read([]byte) (int, error) {
	<-s.ctx.Done()
	return 0, s.ctx.Err()
}

func idleStore(t *testing.T, c storage.Client) *storage.Store {
	t.Helper()
	cfg := finalizedConfig(t, 0, 0)
	cfg.ReadIdleTimeout = new(config.Duration(idleLimit))
	s := storage.New(c, cfg)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return s
}

// A body that stops sending is cut off once a read has waited the idle
// limit: the read fails with ErrUnavailable and the Get's request is
// cancelled, after the bytes that arrived were read.
func TestStore_GetCutsOffAStalledBody(t *testing.T) {
	c := &stallingClient{Fake: storagetest.NewFake(), head: "partial"}
	blob, err := idleStore(t, c).Get(context.Background(), "k", storage.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = blob.Body.Close() }()
	start := time.Now()
	got, err := io.ReadAll(blob.Body)
	if string(got) != "partial" || !errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("ReadAll = %q, %v; want the head, then ErrUnavailable", got, err)
	}
	if waited := time.Since(start); waited < idleLimit || waited > 20*idleLimit {
		t.Errorf("cut off after %s; want about the %s limit", waited, idleLimit)
	}
	if c.ctx.Err() == nil {
		t.Error("the Get's request was not cancelled")
	}
}

// A caller that reads slowly is never cut off: the limit times a read in
// progress, not the time between reads.
func TestStore_GetLetsACallerReadSlowly(t *testing.T) {
	f := storagetest.NewFake()
	s := idleStore(t, f)
	ctx := context.Background()
	if _, err := s.Put(ctx, "k", strings.NewReader("abc"), storage.PutOptions{Size: 3}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	blob, err := s.Get(ctx, "k", storage.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = blob.Body.Close() }()
	var got []byte
	p := make([]byte, 1)
	for {
		time.Sleep(2 * idleLimit)
		n, err := blob.Body.Read(p)
		got = append(got, p[:n]...)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read after %q: %v", got, err)
		}
	}
	if string(got) != "abc" {
		t.Errorf("read %q; want abc", got)
	}
}

// Closing the body releases the Get's request.
func TestStore_GetCloseCancelsTheRequest(t *testing.T) {
	c := &stallingClient{Fake: storagetest.NewFake()}
	blob, err := idleStore(t, c).Get(context.Background(), "k", storage.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := blob.Body.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if c.ctx.Err() == nil {
		t.Error("Close left the Get's request open")
	}
}
