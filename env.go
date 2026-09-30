package storage

import "github.com/standards-lab/go-core/config"

// Env names the variables [Config.Finalize] reads, such as APP_STORAGE_KEY
// under the prefix "app". Options is a prefix: APP_STORAGE_OPTIONS_MAX_RETRIES
// sets the provider option max_retries. An empty name disables that override.
type Env struct {
	Endpoint       string
	Container      string
	Account        string
	Key            string
	MaxObjectSize  string
	ListPageSize   string
	RequestTimeout string
	Options        string
}

// NewEnv composes the override names from prefix under the "storage" segment
// using [config.EnvName]. An empty prefix returns the zero Env, which disables
// every override.
func NewEnv(prefix string) Env {
	return Env{
		Endpoint:       config.EnvName(prefix, "storage", "endpoint"),
		Container:      config.EnvName(prefix, "storage", "container"),
		Account:        config.EnvName(prefix, "storage", "account"),
		Key:            config.EnvName(prefix, "storage", "key"),
		MaxObjectSize:  config.EnvName(prefix, "storage", "max", "object", "size"),
		ListPageSize:   config.EnvName(prefix, "storage", "list", "page", "size"),
		RequestTimeout: config.EnvName(prefix, "storage", "request", "timeout"),
		Options:        config.EnvName(prefix, "storage", "options"),
	}
}
