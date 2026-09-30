package azureblob

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/standards-lab/go-storage"
)

// The Options keys this package reads. See the package documentation for
// their values.
const (
	optionMaxRetries  = "max_retries"
	optionTryTimeout  = "try_timeout"
	optionBlockSize   = "block_size"
	optionConcurrency = "concurrency"
)

// The upload sizing defaults and the range each upload option accepts.
const (
	defaultBlockSize   int64 = 4 << 20
	defaultConcurrency       = 4
	minBlockSize       int64 = 1 << 20
	maxBlockSize       int64 = 100 << 20
	maxConcurrency           = 32
)

var _ storage.Client = (*Client)(nil)

// Client is the Azure Blob Storage provider: a storage.Client over one
// container of one storage account, authenticated with the account's shared
// key. A Client is safe for concurrent use.
type Client struct {
	container   *container.Client
	blockSize   int64
	concurrency int
}

// New constructs a Client from a finalized config without I/O, as the
// package documentation describes. A missing field, an invalid container
// name, or a malformed option is an error; an unfinalized config panics.
func New(cfg storage.Config) (*Client, error) {
	if !cfg.Finalized() {
		panic("azureblob: Config not finalized: call Finalize before New")
	}
	if cfg.Container == "" {
		return nil, errors.New("azureblob: storage container required")
	}
	if err := validateContainer(cfg.Container); err != nil {
		return nil, err
	}
	if cfg.Account == "" {
		return nil, errors.New("azureblob: storage account required")
	}
	if cfg.Key == "" {
		return nil, errors.New("azureblob: storage key required")
	}

	opts, err := clientOptions(cfg.Options)
	if err != nil {
		return nil, err
	}
	blockSize, concurrency, err := uploadOptions(cfg.Options)
	if err != nil {
		return nil, err
	}

	cred, err := container.NewSharedKeyCredential(cfg.Account, cfg.Key)
	if err != nil {
		return nil, fmt.Errorf("azureblob: shared key credential: %w", err)
	}

	serviceURL := cfg.Endpoint
	if serviceURL == "" {
		serviceURL = "https://" + cfg.Account + ".blob.core.windows.net/"
	}
	containerURL := runtime.JoinPaths(serviceURL, cfg.Container)

	cc, err := container.NewClientWithSharedKeyCredential(containerURL, cred, opts)
	if err != nil {
		return nil, fmt.Errorf("azureblob: container client: %w", err)
	}
	return &Client{container: cc, blockSize: blockSize, concurrency: concurrency}, nil
}

// uploadOptions reads the block_size and concurrency options, applying the
// defaults for an unset key.
func uploadOptions(options map[string]string) (blockSize int64, concurrency int, err error) {
	blockSize, concurrency = defaultBlockSize, defaultConcurrency
	if v, ok := options[optionBlockSize]; ok {
		n, perr := strconv.ParseInt(v, 10, 64)
		if perr != nil || n < minBlockSize || n > maxBlockSize {
			return 0, 0, fmt.Errorf("azureblob: option %s: %q is not an integer between %d and %d bytes", optionBlockSize, v, minBlockSize, maxBlockSize)
		}
		blockSize = n
	}
	if v, ok := options[optionConcurrency]; ok {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 1 || n > maxConcurrency {
			return 0, 0, fmt.Errorf("azureblob: option %s: %q is not an integer between 1 and %d", optionConcurrency, v, maxConcurrency)
		}
		concurrency = n
	}
	return blockSize, concurrency, nil
}

// clientOptions reads the max_retries and try_timeout options into the SDK's
// retry policy. A key it does not know is ignored.
func clientOptions(options map[string]string) (*container.ClientOptions, error) {
	opts := &container.ClientOptions{}
	if v, ok := options[optionMaxRetries]; ok {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("azureblob: option %s: %q is not a non-negative integer", optionMaxRetries, v)
		}
		opts.Retry.MaxRetries = int32(n)
		if n == 0 {
			// The SDK reads a zero MaxRetries as "apply the default" and a
			// negative one as "one try, no retries".
			opts.Retry.MaxRetries = -1
		}
	}
	if v, ok := options[optionTryTimeout]; ok {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("azureblob: option %s: %q is not a positive duration", optionTryTimeout, v)
		}
		opts.Retry.TryTimeout = d
	}
	return opts, nil
}

// EnsureContainer creates the configured container and treats the service's
// ContainerAlreadyExists answer as success.
func (c *Client) EnsureContainer(ctx context.Context) error {
	_, err := c.container.Create(ctx, nil)
	if bloberror.HasCode(err, bloberror.ContainerAlreadyExists) {
		return nil
	}
	return classify(err)
}

// Probe reads the container's properties, which proves the endpoint, the
// credential, and the container together.
func (c *Client) Probe(ctx context.Context) error {
	_, err := c.container.GetProperties(ctx, nil)
	return classify(err)
}

// Capabilities returns the blob-name rules the package documentation lists.
func (c *Client) Capabilities() storage.Capabilities {
	return storage.Capabilities{
		MaxKeyLength: maxKeyLength,
		ValidateKey:  validateKey,
	}
}
