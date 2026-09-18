package storage

import "github.com/standards-lab/go-core/config"

// Env names the environment variables [Config.Finalize] reads. With the
// prefix "app", the names are APP_STORAGE_ENDPOINT, APP_STORAGE_CONTAINER,
// APP_STORAGE_ACCOUNT, APP_STORAGE_KEY, APP_STORAGE_MAX_OBJECT_SIZE,
// APP_STORAGE_LIST_PAGE_SIZE, and APP_STORAGE_REQUEST_TIMEOUT. Options has no
// environment override. An empty name disables that one override. Finalize
// populates Env and exposes it for introspection.
type Env struct {
	Endpoint       string
	Container      string
	Account        string
	Key            string
	MaxObjectSize  string
	ListPageSize   string
	RequestTimeout string
}

// NewEnv composes the override names from prefix under the "storage" segment
// using [config.EnvName]. An empty prefix returns the zero Env, which disables
// every override.
func NewEnv(prefix string) Env {
	if prefix == "" {
		return Env{}
	}
	return Env{
		Endpoint:       config.EnvName(prefix, "storage", "endpoint"),
		Container:      config.EnvName(prefix, "storage", "container"),
		Account:        config.EnvName(prefix, "storage", "account"),
		Key:            config.EnvName(prefix, "storage", "key"),
		MaxObjectSize:  config.EnvName(prefix, "storage", "max", "object", "size"),
		ListPageSize:   config.EnvName(prefix, "storage", "list", "page", "size"),
		RequestTimeout: config.EnvName(prefix, "storage", "request", "timeout"),
	}
}
