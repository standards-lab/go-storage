package azureblob

import (
	"testing"

	"github.com/standards-lab/go-storage"
)

func TestNew_ContainerURL(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		want     string
	}{
		{"default endpoint", "", "https://acct.blob.core.windows.net/files"},
		{"path-style endpoint", "http://127.0.0.1:10000/devstoreaccount1", "http://127.0.0.1:10000/devstoreaccount1/files"},
		{"path-style endpoint with trailing slash", "http://127.0.0.1:10000/devstoreaccount1/", "http://127.0.0.1:10000/devstoreaccount1/files"},
		{"custom host", "https://blob.example.test/", "https://blob.example.test/files"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := storage.Config{
				Endpoint:  tc.endpoint,
				Container: "files",
				Account:   "acct",
				Key:       "a2V5",
			}
			if err := cfg.Finalize(""); err != nil {
				t.Fatalf("finalize config: %v", err)
			}
			c, err := New(cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if got := c.container.URL(); got != tc.want {
				t.Errorf("container URL = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestUploadOptions(t *testing.T) {
	cases := []struct {
		name            string
		options         map[string]string
		wantBlockSize   int64
		wantConcurrency int
	}{
		{"unset applies the defaults", nil, DefaultBlockSize, DefaultConcurrency},
		{"block_size at the floor", map[string]string{"block_size": "1048576"}, 1 << 20, DefaultConcurrency},
		{"block_size at the cap", map[string]string{"block_size": "104857600"}, 100 << 20, DefaultConcurrency},
		{"concurrency at the floor", map[string]string{"concurrency": "1"}, DefaultBlockSize, 1},
		{"concurrency at the cap", map[string]string{"concurrency": "32"}, DefaultBlockSize, 32},
		{"both set", map[string]string{"block_size": "8388608", "concurrency": "2"}, 8 << 20, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blockSize, concurrency, err := uploadOptions(tc.options)
			if err != nil {
				t.Fatalf("uploadOptions: %v", err)
			}
			if blockSize != tc.wantBlockSize || concurrency != tc.wantConcurrency {
				t.Errorf("uploadOptions = (%d, %d), want (%d, %d)", blockSize, concurrency, tc.wantBlockSize, tc.wantConcurrency)
			}
		})
	}
}

func TestClientOptions_MaxRetries(t *testing.T) {
	cases := []struct {
		name    string
		options map[string]string
		want    int32
	}{
		{"unset leaves the SDK default", nil, 0},
		{"zero means one try", map[string]string{"max_retries": "0"}, -1},
		{"positive passes through", map[string]string{"max_retries": "4"}, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := clientOptions(tc.options)
			if err != nil {
				t.Fatalf("clientOptions: %v", err)
			}
			if opts.Retry.MaxRetries != tc.want {
				t.Errorf("MaxRetries = %d, want %d", opts.Retry.MaxRetries, tc.want)
			}
		})
	}
}
