package azureblob_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/azureblob"
)

func newClient(t *testing.T, cfg storage.Config) *azureblob.Client {
	t.Helper()
	c, err := azureblob.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNew_RequiresContainerAccountAndKey(t *testing.T) {
	cases := []struct {
		name  string
		strip func(*storage.Config)
		want  string
	}{
		{"container", func(c *storage.Config) { c.Container = "" }, "container required"},
		{"account", func(c *storage.Config) { c.Account = "" }, "account required"},
		{"account with endpoint", func(c *storage.Config) { c.Account = "" }, "account required"},
		{"key", func(c *storage.Config) { c.Key = "" }, "key required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Finalize first, since Finalize itself rejects an empty
			// container; the field is cleared afterwards so New's own check
			// is what fails.
			cfg := testConfig(t, "http://127.0.0.1:10000/"+testAccount, nil)
			if tc.name == "account" {
				cfg.Endpoint = ""
			}
			tc.strip(&cfg)
			_, err := azureblob.New(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("New = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestNew_RejectsBadMaxRetries(t *testing.T) {
	for _, v := range []string{"-1", "x", "1.5", ""} {
		t.Run(v, func(t *testing.T) {
			cfg := testConfig(t, "http://127.0.0.1:10000/"+testAccount, map[string]string{"max_retries": v})
			_, err := azureblob.New(cfg)
			if err == nil || !strings.Contains(err.Error(), "max_retries") {
				t.Fatalf("New = %v, want an error naming max_retries", err)
			}
		})
	}
}

func TestNew_RejectsBadKey(t *testing.T) {
	cfg := testConfig(t, "http://127.0.0.1:10000/"+testAccount, nil)
	cfg.Key = "not base64!"
	if _, err := azureblob.New(cfg); err == nil {
		t.Fatal("New with a key that is not base64 = nil, want an error")
	}
}

func TestNew_IgnoresUnknownOptions(t *testing.T) {
	cfg := testConfig(t, "http://127.0.0.1:10000/"+testAccount, map[string]string{"region": "us-east-1"})
	newClient(t, cfg)
}

func TestNew_PanicsOnUnfinalizedConfig(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("New with an unfinalized config did not panic")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "Finalize") {
			t.Fatalf("panic = %v, want the fix named", r)
		}
	}()
	_, _ = azureblob.New(storage.Config{Container: testContainer, Account: testAccount, Key: testKey})
}

func TestProbe_Success(t *testing.T) {
	svc := newService(t, status(http.StatusOK))
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	if err := c.Probe(t.Context()); err != nil {
		t.Fatalf("Probe = %v, want nil", err)
	}

	reqs := svc.Requests()
	if len(reqs) != 1 {
		t.Fatalf("service saw %d requests, want 1", len(reqs))
	}
	r := reqs[0]
	if r.Method != http.MethodGet {
		t.Errorf("method = %s, want GET", r.Method)
	}
	if want := "/" + testAccount + "/" + testContainer; r.Path != want {
		t.Errorf("path = %s, want %s", r.Path, want)
	}
	if !strings.Contains(r.Query, "restype=container") {
		t.Errorf("query = %q, want restype=container", r.Query)
	}
	if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "SharedKey "+testAccount+":") {
		t.Errorf("Authorization = %q, want a shared-key signature for %s", auth, testAccount)
	}
}

func TestProbe_ContainerNotFound(t *testing.T) {
	svc := newService(t, failWith(http.StatusNotFound, "ContainerNotFound"))
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	err := c.Probe(t.Context())
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Probe = %v, want ErrNotFound", err)
	}
	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) || respErr.ErrorCode != "ContainerNotFound" {
		t.Fatalf("Probe = %v, want the SDK's ResponseError matchable with its code", err)
	}
}

func TestEnsureContainer_Created(t *testing.T) {
	svc := newService(t, status(http.StatusCreated))
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	if err := c.EnsureContainer(t.Context()); err != nil {
		t.Fatalf("EnsureContainer = %v, want nil", err)
	}
	reqs := svc.Requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPut {
		t.Fatalf("service saw %+v, want one PUT", reqs)
	}
	if !strings.Contains(reqs[0].Query, "restype=container") {
		t.Errorf("query = %q, want restype=container", reqs[0].Query)
	}
}

func TestEnsureContainer_AlreadyExists(t *testing.T) {
	svc := newService(t, failWith(http.StatusConflict, "ContainerAlreadyExists"))
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	if err := c.EnsureContainer(t.Context()); err != nil {
		t.Fatalf("EnsureContainer over an existing container = %v, want nil", err)
	}
}

func TestEnsureContainer_ServerError(t *testing.T) {
	svc := newService(t, failWith(http.StatusInternalServerError, "InternalError"))
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	err := c.EnsureContainer(t.Context())
	if !errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("EnsureContainer = %v, want ErrUnavailable", err)
	}
	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) || respErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("EnsureContainer = %v, want the SDK's ResponseError matchable with status 500", err)
	}
	// max_retries=0 means one try: the scripted 500 is retryable to the SDK,
	// and the option is what stopped it.
	if n := len(svc.Requests()); n != 1 {
		t.Errorf("service saw %d requests, want 1 with max_retries=0", n)
	}
}

func TestEnsureContainer_Forbidden(t *testing.T) {
	svc := newService(t, failWith(http.StatusForbidden, "AuthorizationFailure"))
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	err := c.EnsureContainer(t.Context())
	if err == nil {
		t.Fatal("EnsureContainer on 403 = nil, want an error")
	}
	if errors.Is(err, storage.ErrUnavailable) || errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("EnsureContainer on 403 = %v, want it unclassified", err)
	}
	var respErr *azcore.ResponseError
	if !errors.As(err, &respErr) || respErr.StatusCode != http.StatusForbidden {
		t.Fatalf("EnsureContainer = %v, want the SDK's ResponseError with status 403", err)
	}
}

func TestMaxRetries_BoundsTheSDKRetryCount(t *testing.T) {
	svc := newService(t, failWith(http.StatusServiceUnavailable, "ServerBusy"))
	c := newClient(t, testConfig(t, svc.endpoint(), map[string]string{"max_retries": "1"}))

	err := c.Probe(t.Context())
	if !errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("Probe = %v, want ErrUnavailable", err)
	}
	if n := len(svc.Requests()); n != 2 {
		t.Errorf("service saw %d requests, want 2 with max_retries=1", n)
	}
}

func TestProbe_ConnectionRefused(t *testing.T) {
	c := newClient(t, testConfig(t, closedEndpoint(t), nil))

	err := c.Probe(t.Context())
	if !errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("Probe against a closed port = %v, want ErrUnavailable", err)
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		t.Fatalf("Probe against a closed port = %v, want no ResponseError in the chain", err)
	}
}

func TestProbe_CallerCancelled(t *testing.T) {
	arrived := make(chan struct{})
	svc := newService(t, func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		<-r.Context().Done()
	})
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-arrived
		cancel()
	}()
	err := c.Probe(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Probe under a cancelled context = %v, want context.Canceled", err)
	}
	if errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("Probe under a cancelled context = %v, want it unclassified", err)
	}
}

func TestProbe_CallerDeadline(t *testing.T) {
	svc := newService(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	err := c.Probe(ctx)
	if !errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("Probe past its deadline = %v, want ErrUnavailable", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Probe past its deadline = %v, want context.DeadlineExceeded still matchable", err)
	}
}

func TestCapabilities(t *testing.T) {
	c := newClient(t, testConfig(t, "http://127.0.0.1:10000/"+testAccount, nil))

	caps := c.Capabilities()
	if caps.MaxKeyLength != azureblob.MaxKeyLength {
		t.Errorf("MaxKeyLength = %d, want %d", caps.MaxKeyLength, azureblob.MaxKeyLength)
	}
	if caps.ValidateKey == nil {
		t.Fatal("ValidateKey is nil")
	}
	if err := caps.ValidateKey("storagetest/0123abcd/x.txt"); err != nil {
		t.Errorf("ValidateKey(valid) = %v", err)
	}
	if err := caps.ValidateKey(""); err == nil {
		t.Error("ValidateKey(\"\") = nil, want an error")
	}
}
