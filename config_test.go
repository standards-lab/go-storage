package storage_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-core/config"
	"github.com/standards-lab/go-storage"
)

// validConfig satisfies the one required field; everything else exercises
// defaults under test.
func validConfig() storage.Config {
	return storage.Config{Container: "assets"}
}

func TestConfig_MergeOverlaysSetFields(t *testing.T) {
	base := storage.Config{
		Endpoint:      "http://127.0.0.1:10000",
		Container:     "assets",
		Account:       "devstoreaccount1",
		Key:           "base-key",
		MaxObjectSize: 1024,
		ListPageSize:  100,
	}
	overlay := storage.Config{
		Endpoint:       "https://blob.internal",
		Key:            "overlay-key",
		MaxObjectSize:  4096,
		RequestTimeout: new(config.Duration(3 * time.Second)),
	}

	base.Merge(&overlay)

	if base.Endpoint != "https://blob.internal" {
		t.Errorf("Endpoint = %s, want https://blob.internal", base.Endpoint)
	}
	if base.Key != "overlay-key" {
		t.Errorf("Key = %s, want overlay-key", base.Key)
	}
	if base.MaxObjectSize != 4096 {
		t.Errorf("MaxObjectSize = %d, want 4096", base.MaxObjectSize)
	}
	if base.RequestTimeout == nil || base.RequestTimeout.Duration() != 3*time.Second {
		t.Errorf("RequestTimeout = %v, want 3s", base.RequestTimeout)
	}
	// Fields the overlay leaves unset keep the base values.
	if base.Container != "assets" {
		t.Errorf("Container = %s, want assets", base.Container)
	}
	if base.Account != "devstoreaccount1" {
		t.Errorf("Account = %s, want devstoreaccount1", base.Account)
	}
	if base.ListPageSize != 100 {
		t.Errorf("ListPageSize = %d, want 100", base.ListPageSize)
	}
}

func TestConfig_MergeOptionsKeyWise(t *testing.T) {
	base := storage.Config{
		Container: "assets",
		Options:   map[string]string{"region": "us-east-1", "path_style": "true"},
	}
	overlay := storage.Config{
		Options: map[string]string{"region": "eu-west-1"},
	}

	base.Merge(&overlay)

	if got := base.Options["region"]; got != "eu-west-1" {
		t.Errorf("Options[region] = %s, want eu-west-1", got)
	}
	// An overlay key overrides without dropping the others.
	if got := base.Options["path_style"]; got != "true" {
		t.Errorf("Options[path_style] = %s, want true", got)
	}
}

func TestConfig_MergeOptionsOntoNilMap(t *testing.T) {
	base := storage.Config{Container: "assets"}
	overlay := storage.Config{Options: map[string]string{"region": "us-east-1"}}

	base.Merge(&overlay)

	if got := base.Options["region"]; got != "us-east-1" {
		t.Errorf("Options[region] = %s, want us-east-1", got)
	}
}

func TestConfig_FinalizeDefaults(t *testing.T) {
	cfg := validConfig()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if cfg.RequestTimeout == nil || cfg.RequestTimeout.Duration() != 10*time.Second {
		t.Errorf("RequestTimeout = %v, want 10s", cfg.RequestTimeout)
	}
	// The size limits are application policy and keep no base default.
	if cfg.MaxObjectSize != 0 {
		t.Errorf("MaxObjectSize = %d, want 0", cfg.MaxObjectSize)
	}
	if cfg.ListPageSize != 0 {
		t.Errorf("ListPageSize = %d, want 0", cfg.ListPageSize)
	}
}

func TestConfig_FinalizeKeepsExplicitRequestTimeout(t *testing.T) {
	cfg := validConfig()
	cfg.RequestTimeout = new(config.Duration(2 * time.Second))
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	if cfg.RequestTimeout.Duration() != 2*time.Second {
		t.Errorf("RequestTimeout = %v, want 2s", cfg.RequestTimeout)
	}
}

func TestConfig_FinalizeRequiresContainer(t *testing.T) {
	cfg := storage.Config{Account: "devstoreaccount1"}
	err := cfg.Finalize("")
	if err == nil {
		t.Fatal("Finalize accepted a config with no container")
	}
	if !strings.Contains(err.Error(), "container required") {
		t.Errorf("error = %v, want it to name the missing field", err)
	}
}

func TestConfig_FinalizeEnvOverrides(t *testing.T) {
	cfg := validConfig()
	cfg.Endpoint = "http://127.0.0.1:10000"
	cfg.Account = "devstoreaccount1"
	cfg.Key = "file-key"
	cfg.MaxObjectSize = 1024
	cfg.ListPageSize = 100
	cfg.RequestTimeout = new(config.Duration(2 * time.Second))

	t.Setenv("TEST_STORAGE_ENDPOINT", "https://blob.internal")
	t.Setenv("TEST_STORAGE_CONTAINER", "assets_test")
	t.Setenv("TEST_STORAGE_ACCOUNT", "tester")
	t.Setenv("TEST_STORAGE_KEY", "secret")
	t.Setenv("TEST_STORAGE_MAX_OBJECT_SIZE", "5368709120")
	t.Setenv("TEST_STORAGE_LIST_PAGE_SIZE", "500")
	t.Setenv("TEST_STORAGE_REQUEST_TIMEOUT", "3s")

	if err := cfg.Finalize("test"); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// Each override beats the value the file layer set.
	if cfg.Endpoint != "https://blob.internal" {
		t.Errorf("Endpoint = %s, want https://blob.internal", cfg.Endpoint)
	}
	if cfg.Container != "assets_test" {
		t.Errorf("Container = %s, want assets_test", cfg.Container)
	}
	if cfg.Account != "tester" {
		t.Errorf("Account = %s, want tester", cfg.Account)
	}
	if cfg.Key != "secret" {
		t.Errorf("Key = %s, want secret", cfg.Key)
	}
	// The value exceeds int32, so it proves the int64 parse path.
	if cfg.MaxObjectSize != 5368709120 {
		t.Errorf("MaxObjectSize = %d, want 5368709120", cfg.MaxObjectSize)
	}
	if cfg.ListPageSize != 500 {
		t.Errorf("ListPageSize = %d, want 500", cfg.ListPageSize)
	}
	if cfg.RequestTimeout.Duration() != 3*time.Second {
		t.Errorf("RequestTimeout = %s, want 3s", cfg.RequestTimeout)
	}
}

func TestConfig_FinalizeMalformedEnvFails(t *testing.T) {
	cases := []struct {
		name  string
		set   string
		value string
	}{
		{"max object size", "TEST_STORAGE_MAX_OBJECT_SIZE", "huge"},
		{"list page size", "TEST_STORAGE_LIST_PAGE_SIZE", "many"},
		{"request timeout", "TEST_STORAGE_REQUEST_TIMEOUT", "soon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			t.Setenv(tc.set, tc.value)

			err := cfg.Finalize("test")
			if err == nil {
				t.Fatalf("Finalize accepted %s=%q", tc.set, tc.value)
			}
			if !strings.Contains(err.Error(), tc.set) {
				t.Errorf("error = %v, want it to name %s", err, tc.set)
			}
		})
	}
}

func TestConfig_FinalizeZeroEnvDisablesOverrides(t *testing.T) {
	// The zero Env names no variables, so ambient values cannot leak in.
	t.Setenv("TEST_STORAGE_CONTAINER", "assets_test")
	t.Setenv("TEST_STORAGE_REQUEST_TIMEOUT", "3s")

	cfg := validConfig()
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if cfg.Container != "assets" {
		t.Errorf("Container = %s with zero Env, want assets", cfg.Container)
	}
	if cfg.RequestTimeout.Duration() != 10*time.Second {
		t.Errorf("RequestTimeout = %s with zero Env, want 10s", cfg.RequestTimeout)
	}
}

func TestConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*storage.Config)
		wantErr string
	}{
		{
			"missing container",
			func(c *storage.Config) { c.Container = "" },
			"container required",
		},
		{
			"negative max object size",
			func(c *storage.Config) { c.MaxObjectSize = -1 },
			"invalid max_object_size",
		},
		{
			"negative list page size",
			func(c *storage.Config) { c.ListPageSize = -1 },
			"invalid list_page_size",
		},
		{
			"zero request timeout",
			func(c *storage.Config) {
				c.RequestTimeout = new(config.Duration(0))
			},
			"request_timeout must be positive",
		},
		{
			"negative request timeout",
			func(c *storage.Config) {
				c.RequestTimeout = new(config.Duration(-time.Second))
			},
			"request_timeout must be positive",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig()
			tc.mutate(&cfg)

			err := cfg.Finalize("")
			if err == nil {
				t.Fatal("Finalize accepted an invalid config")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestConfig_ValidateAllowsUnsetSizes(t *testing.T) {
	// A MaxObjectSize of 0 means unbounded and a ListPageSize of 0 means the
	// provider's default; both survive validation.
	cfg := validConfig()
	cfg.MaxObjectSize = 0
	cfg.ListPageSize = 0

	if err := cfg.Finalize(""); err != nil {
		t.Errorf("Finalize rejected unset size settings: %v", err)
	}
}

// The go-core contract end to end: config.Load reads the base file and the
// secrets file, merges them, and finalizes the result with defaults applied.
func TestConfig_Load(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"),
		`{"container": "assets", "request_timeout": "30s"}`)
	writeFile(t, filepath.Join(dir, "secrets.json"),
		`{"key": "secret"}`)

	cfg, err := config.Load[storage.Config](config.Options{Dir: dir, EnvPrefix: "app"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Container != "assets" {
		t.Errorf("Container = %s, want assets", cfg.Container)
	}
	if cfg.RequestTimeout == nil || cfg.RequestTimeout.Duration() != 30*time.Second {
		t.Errorf("RequestTimeout = %v, want 30s", cfg.RequestTimeout)
	}
	// The secrets layer supplies the credential.
	if cfg.Key != "secret" {
		t.Errorf("Key = %s, want secret", cfg.Key)
	}
	// Finalize ran: the override names are composed and the size limits keep
	// no default.
	if cfg.Env.Container != "APP_STORAGE_CONTAINER" {
		t.Errorf("Env.Container = %s, want APP_STORAGE_CONTAINER", cfg.Env.Container)
	}
	if cfg.MaxObjectSize != 0 {
		t.Errorf("MaxObjectSize = %d, want 0", cfg.MaxObjectSize)
	}
	if cfg.ListPageSize != 0 {
		t.Errorf("ListPageSize = %d, want 0", cfg.ListPageSize)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
