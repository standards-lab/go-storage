package storage

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/standards-lab/go-core/config"
)

// defaultRequestTimeout is RequestTimeout's default.
const defaultRequestTimeout = 10 * time.Second

// Config holds the identity, credential, and limits of one object storage
// container. Env records the override names Finalize composed and is
// excluded from JSON.
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

	// Key is the shared-key credential. It belongs in the secrets layer of
	// [config.Load], not a committed file.
	Key string `json:"key"`

	// Options carries provider-specific settings that the provider reads.
	// Each option takes an environment override; see [Env].
	Options map[string]string `json:"options"`

	// MaxObjectSize is the largest body [Store] accepts in a Put, in bytes.
	// It has no default; 0 means unbounded.
	MaxObjectSize int64 `json:"max_object_size"`

	// ListPageSize is the page size [Store] requests when a List call sets no
	// Limit. It has no default; 0 means the provider's own page size.
	ListPageSize int `json:"list_page_size"`

	// RequestTimeout bounds the calls [Store] makes on its own behalf: the
	// container check and probe in Start, together, and the probe in Ready.
	// It defaults to 10 seconds. The object operations take their timeouts
	// from the caller's context and the provider's transport.
	RequestTimeout *config.Duration `json:"request_timeout"`

	Env Env `json:"-"`

	// finalized records that the last Finalize succeeded; Config is copied
	// by value, so a finalized copy stays finalized.
	finalized bool
}

// Merge overlays src's set fields onto the receiver. Options merges key-wise
// into a copy, so an overlay can set one provider option without dropping the
// rest or writing to a map the caller shares. A zero size in src leaves the
// receiver's value, so an overlay file cannot lift a bound back to unbounded;
// the environment override can.
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

	if len(src.Options) > 0 {
		c.Options = maps.Clone(c.Options)
		if c.Options == nil {
			c.Options = make(map[string]string, len(src.Options))
		}
		maps.Copy(c.Options, src.Options)
	}
}

// Finalize composes the environment override names from envPrefix (an empty
// prefix disables overrides), applies defaults, applies the overrides, and
// validates. Container is the one required field; a malformed override fails
// with an error naming its variable.
func (c *Config) Finalize(envPrefix string) error {
	c.finalized = false
	c.Env = NewEnv(envPrefix)
	c.applyDefaults()
	if err := c.applyEnv(); err != nil {
		return err
	}
	if err := c.validate(); err != nil {
		return err
	}
	c.finalized = true
	return nil
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
	c.applyOptionsEnv()
	return config.SetDurationFromEnv(&c.RequestTimeout, c.Env.RequestTimeout)
}

// applyOptionsEnv sets provider options from the variables under the
// Env.Options prefix, as [Env] describes. It ignores a name whose suffix is
// not a valid option key and an empty value, and copies the map before the
// first write.
func (c *Config) applyOptionsEnv() {
	if c.Env.Options == "" {
		return
	}
	prefix := c.Env.Options + "_"
	copied := false
	for _, kv := range os.Environ() {
		name, v, ok := strings.Cut(kv, "=")
		if !ok || v == "" || !strings.HasPrefix(name, prefix) {
			continue
		}
		key := strings.TrimPrefix(name, prefix)
		if !optionKey(key) {
			continue
		}
		if !copied {
			c.Options = maps.Clone(c.Options)
			if c.Options == nil {
				c.Options = map[string]string{}
			}
			copied = true
		}
		c.Options[strings.ToLower(key)] = v
	}
}

// optionKey reports whether key is a valid option-variable suffix: non-empty
// and made only of A–Z, 0–9, and underscores.
func optionKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

// Finalized reports whether the last Finalize on c, or on the Config c was
// copied from, succeeded. [New] and a provider's constructor check it to
// catch a wiring defect.
func (c *Config) Finalized() bool {
	return c.finalized
}

// validate checks the finalized values. Every error has the form
// "storage: <field> <problem>".
func (c *Config) validate() error {
	if c.Container == "" {
		return errors.New("storage: container is required")
	}
	if c.MaxObjectSize < 0 {
		return fmt.Errorf("storage: max_object_size must not be negative, got %d", c.MaxObjectSize)
	}
	if c.ListPageSize < 0 {
		return fmt.Errorf("storage: list_page_size must not be negative, got %d", c.ListPageSize)
	}
	if *c.RequestTimeout <= 0 {
		return fmt.Errorf("storage: request_timeout must be positive, got %s", c.RequestTimeout)
	}
	return nil
}
