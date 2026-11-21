package azureblob_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/storagetest"
)

const endpointEnv = "AZUREBLOB_TEST_ENDPOINT"

func acceptanceConfig(t *testing.T) storage.Config {
	t.Helper()
	endpoint := os.Getenv(endpointEnv)
	if endpoint == "" {
		t.Skipf("%s not set", endpointEnv)
	}

	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("random container name: %v", err)
	}
	name := "acceptance-" + hex.EncodeToString(b[:])

	cfg := storage.Config{
		Endpoint:  endpoint,
		Container: name,
		Account:   testAccount,
		Key:       testKey,
		Options:   map[string]string{"block_size": "1048576"},
	}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("finalize config: %v", err)
	}

	t.Cleanup(func() {
		cred, err := container.NewSharedKeyCredential(testAccount, testKey)
		if err != nil {
			return
		}
		cc, err := container.NewClientWithSharedKeyCredential(runtime.JoinPaths(endpoint, name), cred, nil)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = cc.Delete(ctx, nil)
	})
	return cfg
}

func TestAcceptance_Conformance(t *testing.T) {
	cfg := acceptanceConfig(t)
	storagetest.Run(t, func(t *testing.T) storage.Client {
		return newClient(t, cfg)
	})
}

func TestAcceptance_MissingContainer(t *testing.T) {
	cfg := acceptanceConfig(t)
	storagetest.RunMissingContainer(t, func(t *testing.T) storage.Client {
		return newClient(t, cfg)
	})
}

func TestAcceptance_StoreStart(t *testing.T) {
	cfg := acceptanceConfig(t)
	store := storage.New(newClient(t, cfg), cfg)
	if err := store.Start(t.Context()); err != nil {
		t.Fatalf("Start against an empty service: %v", err)
	}
	if !store.Ready() {
		t.Fatal("Ready() = false after Start, want true")
	}
	t.Cleanup(func() {
		if err := store.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})

	ctx := t.Context()
	key := "store/hello.txt"
	content := []byte("hello through the store")
	put, err := store.Put(ctx, key, bytes.NewReader(content), storage.PutOptions{ContentType: "text/plain", Size: int64(len(content))})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	t.Logf("Put:  ETag=%q ModifiedAt=%v", put.ETag, put.ModifiedAt)

	blob, err := store.Get(ctx, key, storage.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	data, err := io.ReadAll(blob.Body)
	_ = blob.Body.Close()
	if err != nil {
		t.Fatalf("read Get body: %v", err)
	}
	t.Logf("Get:  ETag=%q ModifiedAt=%v ContentType=%q Size=%d", blob.ETag, blob.ModifiedAt, blob.ContentType, blob.Size)
	if !bytes.Equal(data, content) {
		t.Errorf("Get returned %q, want %q", data, content)
	}

	stat, err := store.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	t.Logf("Stat: ETag=%q ModifiedAt=%v ContentType=%q Size=%d", stat.ETag, stat.ModifiedAt, stat.ContentType, stat.Size)

	page, err := store.List(ctx, storage.ListOptions{Prefix: "store/"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, obj := range page.Objects {
		t.Logf("List: Key=%q ETag=%q ModifiedAt=%v ContentType=%q Size=%d", obj.Key, obj.ETag, obj.ModifiedAt, obj.ContentType, obj.Size)
	}
	if len(page.Objects) != 1 || page.Objects[0].Key != key {
		t.Errorf("List = %+v, want the one key %q", page.Objects, key)
	}

	if put.ETag != stat.ETag || put.ETag != blob.ETag {
		t.Errorf("ETag differs across Put %q, Get %q, Stat %q", put.ETag, blob.ETag, stat.ETag)
	}
	if put.ModifiedAt != stat.ModifiedAt || put.ModifiedAt != blob.ModifiedAt {
		t.Errorf("ModifiedAt differs across Put %v, Get %v, Stat %v", put.ModifiedAt, blob.ModifiedAt, stat.ModifiedAt)
	}
	if stat.ContentType != "text/plain" || !strings.EqualFold(blob.ContentType, "text/plain") {
		t.Errorf("ContentType = Get %q, Stat %q, want text/plain", blob.ContentType, stat.ContentType)
	}

	for range 2 {
		if err := store.Delete(ctx, key); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}
	if _, err := store.Stat(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat after Delete = %v, want ErrNotFound", err)
	}
}
