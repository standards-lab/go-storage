package azureblob

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/standards-lab/go-storage"
)

// The Options keys this package reads. See the package documentation for
// their values.
const (
	optionMaxRetries  = "max_retries"
	optionBlockSize   = "block_size"
	optionConcurrency = "concurrency"
)

// The upload sizing a Client applies when the block_size and concurrency
// options are unset, and the range each option accepts. A Put stages a body
// longer than one block in blocks of block_size bytes, and each of the
// concurrency workers holds one block buffer, so a Put in flight holds at
// most block_size times concurrency bytes: 16 MiB at the defaults.
//
// DefaultBlockSize is four times the SDK's 1 MiB minimum, so a multi-block
// body takes a quarter of the requests, while the service's 50,000-block
// limit still admits an object of about 195 GiB. DefaultConcurrency keeps
// four blocks in flight, enough to overlap request latency on a single
// upload, at 16 MiB per Put. minBlockSize is the SDK's own floor, which it
// applies silently to anything smaller. maxBlockSize is the largest block the
// service accepted before version 2019-12-12 and is already 100 MiB held per
// worker. maxConcurrency bounds the workers where 32 blocks of the default
// size reach 128 MiB per Put.
const (
	DefaultBlockSize   int64 = 4 << 20
	DefaultConcurrency       = 4
	minBlockSize       int64 = 1 << 20
	maxBlockSize       int64 = 100 << 20
	maxConcurrency           = 32
)

var _ storage.Client = (*Client)(nil)

// Client is the Azure Blob Storage provider: a storage.Client over one
// container of one storage account, authenticated with the account's shared
// key. It holds the SDK's container client, and every method classifies the
// SDK's error through classify before returning it.
//
// A Client is safe for concurrent use.
type Client struct {
	container   *container.Client
	blockSize   int64
	concurrency int
}

// New constructs a Client from a finalized config without I/O. Container,
// Account, and Key are required: the container names the target, and the
// shared-key signature the SDK computes for every request carries the
// account name, so Account is needed even when Endpoint replaces the default
// service URL. An empty Endpoint means the account's public service URL,
// https://<Account>.blob.core.windows.net/. A set Endpoint is the service URL
// as given, which is how Azurite's path-style
// http://127.0.0.1:10000/devstoreaccount1 is reached. Options carries the
// provider settings the package documentation lists; a malformed value is a
// construction error. An unfinalized config panics with the fix named.
func New(cfg storage.Config) (*Client, error) {
	if cfg.RequestTimeout == nil {
		panic("azureblob: Config not finalized: call Finalize before New")
	}
	if cfg.Container == "" {
		return nil, errors.New("azureblob: storage container required")
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

// uploadOptions reads the block_size and concurrency settings out of Options,
// applying the defaults for an unset key and rejecting a value outside the
// range the constants above state.
func uploadOptions(options map[string]string) (blockSize int64, concurrency int, err error) {
	blockSize, concurrency = DefaultBlockSize, DefaultConcurrency
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

// clientOptions reads the provider settings out of Options. Only the keys the
// package documents are read; any other key is ignored so an application can
// carry settings for another provider in the same block.
func clientOptions(options map[string]string) (*container.ClientOptions, error) {
	opts := &container.ClientOptions{}
	if v, ok := options[optionMaxRetries]; ok {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("azureblob: option %s: %q is not a non-negative integer", optionMaxRetries, v)
		}
		opts.Retry = policy.RetryOptions{MaxRetries: int32(n)}
		if n == 0 {
			// The SDK reads a zero MaxRetries as "apply the default" and a
			// negative one as "one try, no retries", so the configured 0
			// has to be passed as a negative value to mean what it says.
			opts.Retry.MaxRetries = -1
		}
	}
	return opts, nil
}

// EnsureContainer creates the configured container and succeeds when it
// already exists. The service answers a repeat Create with 409
// ContainerAlreadyExists, which is the idempotent success; every other error
// is classified.
func (c *Client) EnsureContainer(ctx context.Context) error {
	_, err := c.container.Create(ctx, nil)
	if bloberror.HasCode(err, bloberror.ContainerAlreadyExists) {
		return nil
	}
	return classify(err)
}

// Probe reads the container's properties, which proves that the endpoint is
// reachable, the credential signs, and the container exists. A missing
// container matches storage.ErrNotFound.
func (c *Client) Probe(ctx context.Context) error {
	_, err := c.container.GetProperties(ctx, nil)
	return classify(err)
}

// Capabilities returns the blob-name constraints of the Azure Blob service.
// MaxKeyLength is counted in runes; see the package documentation.
func (c *Client) Capabilities() storage.Capabilities {
	return storage.Capabilities{
		MaxKeyLength: MaxKeyLength,
		ValidateKey:  validateKey,
	}
}
