package storage_test

import (
	"testing"

	"github.com/standards-lab/go-storage"
)

// Every name derives from the prefix through config.EnvName with the
// "storage" segment; this pins the full set a consumer binds to.
func TestNewEnv(t *testing.T) {
	env := storage.NewEnv("app")

	cases := []struct {
		field string
		got   string
		want  string
	}{
		{"Endpoint", env.Endpoint, "APP_STORAGE_ENDPOINT"},
		{"Container", env.Container, "APP_STORAGE_CONTAINER"},
		{"Account", env.Account, "APP_STORAGE_ACCOUNT"},
		{"Key", env.Key, "APP_STORAGE_KEY"},
		{"MaxObjectSize", env.MaxObjectSize, "APP_STORAGE_MAX_OBJECT_SIZE"},
		{"ListPageSize", env.ListPageSize, "APP_STORAGE_LIST_PAGE_SIZE"},
		{"RequestTimeout", env.RequestTimeout, "APP_STORAGE_REQUEST_TIMEOUT"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %s, want %s", tc.field, tc.got, tc.want)
		}
	}
}

func TestNewEnv_EmptyPrefixReturnsZeroEnv(t *testing.T) {
	if env := storage.NewEnv(""); env != (storage.Env{}) {
		t.Errorf("NewEnv(\"\") = %+v, want the zero Env (overrides disabled)", env)
	}
}
