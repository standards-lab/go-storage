package storage

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/standards-lab/go-core/config"
)

// defaultRequestTimeout bounds the readiness probe [Store] makes on its own
// behalf. It is the one default this package ships; see [Config].
const defaultRequestTimeout = 10 * time.Second

// Config holds the identity, credential, and limits of one object storage
// container. Env records the environment-variable names Finalize composed and
// read; it is excluded from JSON.
//
// A library ships no policy numbers, so MaxObjectSize and ListPageSize have no
// default: the application supplies them, and each stays 0 after Finalize when
// not configured. RequestTimeout is the one default this package ships, and
// its pointer type lets Finalize tell a configured value from an absent one.
type Config struct {
	// Endpoint is the provider's service address. The base module sets no
	// default; each provider states what it requires.
	Endpoint string `json:"endpoint"`

	// Container is the standard tier's name for what S3 calls a bucket. A
	// provider maps it to its own vocabulary. It is required.
	Container string `json:"container"`

	// Account identifies the credential's owner. The base module sets no
	// default; each provider states what it requires.
	Account string `json:"account"`

	// Key is the shared-key credential. It rides the secrets layer of
	// [config.Load] rather than a committed file, and it also takes an
	// environment override.
	Key string `json:"key"`

	// Options carries provider-specific settings that the provider reads.
	Options map[string]string `json:"options"`

	// MaxObjectSize is the largest body [Store] accepts in a Put, in bytes.
	// 0 means unbounded.
	MaxObjectSize int64 `json:"max_object_size"`

	// ListPageSize is the page size [Store] requests when a List call sets no
	// Limit. 0 means the provider's own page size.
	ListPageSize int `json:"list_page_size"`

	// RequestTimeout bounds the readiness probe [Store] makes on its own
	// behalf in Start and Ready. It defaults to 10 seconds. The object
	// operations take their timeouts from the caller's context and the
	// provider's transport.
	RequestTimeout *config.Duration `json:"request_timeout"`

	Env Env `json:"-"`
}

// Merge overlays src's set fields onto the receiver. Options merges key-wise,
// so an overlay can set one provider option without dropping the rest. A zero
// size in src leaves the receiver's value, so an overlay file cannot lift a
// bound back to unbounded; the environment override can.
func (c *Config) Merge(src *Config) {
	if src.Endpoint != "" {
		c.Endpoint = src.Endpoint
	}
	if src.Container != "" {
		c.Container = src.Container
	}
	if src.Account != "" {
		c.Account = src.Account
	}
	if src.Key != "" {
		c.Key = src.Key
	}
	if src.MaxObjectSize != 0 {
		c.MaxObjectSize = src.MaxObjectSize
	}
	if src.ListPageSize != 0 {
		c.ListPageSize = src.ListPageSize
	}
	if src.RequestTimeout != nil {
		c.RequestTimeout = src.RequestTimeout
	}

	for k, v := range src.Options {
		if c.Options == nil {
			c.Options = make(map[string]string, len(src.Options))
		}
		c.Options[k] = v
	}
}

// Finalize composes the environment override names from envPrefix (an empty
// prefix disables overrides), applies defaults, applies the overrides, and
// validates. Container is the one required field; a malformed override fails
// with an error naming its variable.
func (c *Config) Finalize(envPrefix string) error {
	c.Env = NewEnv(envPrefix)
	c.applyDefaults()
	if err := c.applyEnv(); err != nil {
		return err
	}
	return c.validate()
}

func (c *Config) applyDefaults() {
	if c.RequestTimeout == nil {
		c.RequestTimeout = new(config.Duration(defaultRequestTimeout))
	}
}

func (c *Config) applyEnv() error {
	if v := os.Getenv(c.Env.Endpoint); v != "" {
		c.Endpoint = v
	}
	if v := os.Getenv(c.Env.Container); v != "" {
		c.Container = v
	}
	if v := os.Getenv(c.Env.Account); v != "" {
		c.Account = v
	}
	if v := os.Getenv(c.Env.Key); v != "" {
		c.Key = v
	}
	if v := os.Getenv(c.Env.MaxObjectSize); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Env.MaxObjectSize, err)
		}
		c.MaxObjectSize = n
	}
	if v := os.Getenv(c.Env.ListPageSize); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Env.ListPageSize, err)
		}
		c.ListPageSize = n
	}
	return config.SetDurationFromEnv(&c.RequestTimeout, c.Env.RequestTimeout)
}

// finalized reports whether Finalize ran. RequestTimeout is the one pointer
// Finalize defaults, so its presence is the evidence.
func (c *Config) finalized() bool {
	return c.RequestTimeout != nil
}

func (c *Config) validate() error {
	if c.Container == "" {
		return errors.New("storage container required")
	}
	if c.MaxObjectSize < 0 {
		return fmt.Errorf("invalid max_object_size: %d", c.MaxObjectSize)
	}
	if c.ListPageSize < 0 {
		return fmt.Errorf("invalid list_page_size: %d", c.ListPageSize)
	}
	if *c.RequestTimeout <= 0 {
		return fmt.Errorf("request_timeout must be positive, got %s", c.RequestTimeout)
	}
	return nil
}
