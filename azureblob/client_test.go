package azureblob_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
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
		name     string
		endpoint string
		strip    func(*storage.Config)
		want     string
	}{
		{"container", "http://127.0.0.1:10000/" + testAccount, func(c *storage.Config) { c.Container = "" }, "container required"},
		{"account", "", func(c *storage.Config) { c.Account = "" }, "account required"},
		{"account with endpoint", "http://127.0.0.1:10000/" + testAccount, func(c *storage.Config) { c.Account = "" }, "account required"},
		{"key", "http://127.0.0.1:10000/" + testAccount, func(c *storage.Config) { c.Key = "" }, "key required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Finalize first, since Finalize itself rejects an empty
			// container; the field is cleared afterwards so New's own check
			// is what fails.
			cfg := testConfig(t, tc.endpoint, nil)
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

func TestNew_RejectsBadTryTimeout(t *testing.T) {
	for _, v := range []string{"x", "10", "0s", "-1s", ""} {
		t.Run(v, func(t *testing.T) {
			cfg := testConfig(t, "http://127.0.0.1:10000/"+testAccount, map[string]string{"try_timeout": v})
			_, err := azureblob.New(cfg)
			if err == nil || !strings.Contains(err.Error(), "try_timeout") {
				t.Fatalf("New = %v, want an error naming try_timeout", err)
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

// A key the package does not list is ignored: the client it builds works.
func TestNew_IgnoresUnknownOptions(t *testing.T) {
	svc := newService(t, status(http.StatusOK))
	c := newClient(t, testConfig(t, svc.endpoint(), map[string]string{"region": "us-east-1"}))

	if err := c.Probe(t.Context()); err != nil {
		t.Fatalf("Probe = %v, want nil", err)
	}
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
	if !errors.Is(err, storage.ErrContainerNotFound) || errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Probe = %v, want ErrContainerNotFound and not ErrNotFound", err)
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

// try_timeout bounds each try, so a request the service never answers fails
// as unavailable even under a caller's context with no deadline.
func TestTryTimeout_BoundsAStalledRequest(t *testing.T) {
	svc := newService(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	c := newClient(t, testConfig(t, svc.endpoint(), map[string]string{"try_timeout": "100ms"}))

	start := time.Now()
	err := c.Probe(t.Context())
	if !errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("Probe of a stalled service = %v, want ErrUnavailable", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Probe took %v, want it cut off near the 100ms try timeout", elapsed)
	}
}

func TestProbe_ConnectionRefused(t *testing.T) {
	c := newClient(t, testConfig(t, closedEndpoint(t), nil))

	err := c.Probe(t.Context())
	if !errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("Probe against a closed port = %v, want ErrUnavailable", err)
	}
	if _, ok := errors.AsType[*azcore.ResponseError](err); ok {
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

// The rules ValidateKey applies are proved in keys_test.go.
func TestCapabilities_MaxKeyLength(t *testing.T) {
	c := newClient(t, testConfig(t, "http://127.0.0.1:10000/"+testAccount, nil))

	if got := c.Capabilities().MaxKeyLength; got != 1024 {
		t.Errorf("MaxKeyLength = %d, want 1024", got)
	}
}

// A set Endpoint is the service URL as given, and the client appends the
// container name to its path.
func TestNew_EndpointPath(t *testing.T) {
	for _, suffix := range []string{"/" + testAccount, "/" + testAccount + "/"} {
		t.Run(suffix, func(t *testing.T) {
			svc := newService(t, status(http.StatusOK))
			c := newClient(t, testConfig(t, svc.srv.URL+suffix, nil))

			if err := c.Probe(t.Context()); err != nil {
				t.Fatalf("Probe = %v, want nil", err)
			}
			if reqs := svc.Requests(); len(reqs) != 1 || reqs[0].Path != "/"+testAccount+"/"+testContainer {
				t.Fatalf("service saw %+v, want one request to /%s/%s", reqs, testAccount, testContainer)
			}
		})
	}
}

// defaultEndpointChild names the environment variable under which
// TestNew_DefaultEndpoint runs as the child process it starts.
const defaultEndpointChild = "AZUREBLOB_TEST_DEFAULT_ENDPOINT_CHILD"

// An empty Endpoint means the account's public service URL. The test reruns
// itself as a child whose HTTPS proxy is a listener here, so the host the
// client dials is observed without leaving the machine: the child's Probe
// tunnels through the proxy with a CONNECT to that host.
func TestNew_DefaultEndpoint(t *testing.T) {
	if os.Getenv(defaultEndpointChild) != "" {
		cfg := testConfig(t, "", nil)
		cfg.Account = "acct"
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		_ = newClient(t, cfg).Probe(ctx)
		return
	}

	proxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = proxy.Close() })
	connected := make(chan *http.Request, 1)
	go func() {
		conn, err := proxy.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			return
		}
		connected <- req
		_, _ = io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
	}()

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestNew_DefaultEndpoint$")
	cmd.Env = append(os.Environ(),
		defaultEndpointChild+"=1",
		"HTTPS_PROXY=http://"+proxy.Addr().String(),
		"NO_PROXY=", "no_proxy=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	select {
	case req := <-connected:
		if req.Method != http.MethodConnect || req.Host != "acct.blob.core.windows.net:443" {
			t.Errorf("client tunnelled %s %s, want CONNECT acct.blob.core.windows.net:443", req.Method, req.Host)
		}
	default:
		t.Fatal("the client never reached the proxy")
	}
}

// The upload option bounds the package documentation lists are inclusive.
// Their rejection one past each bound is in TestNew_RejectsBadUploadOptions.
func TestNew_AcceptsUploadOptionBounds(t *testing.T) {
	for _, opt := range []map[string]string{
		{"block_size": "1048576"},
		{"block_size": "104857600"},
		{"concurrency": "1"},
		{"concurrency": "32"},
	} {
		t.Run(fmt.Sprint(opt), func(t *testing.T) {
			if _, err := azureblob.New(testConfig(t, "http://127.0.0.1:10000/"+testAccount, opt)); err != nil {
				t.Fatalf("New with %v = %v, want nil", opt, err)
			}
		})
	}
}

// An unset max_retries keeps the SDK's default of three retries. The
// service's retry-after-ms answer stands in for the default backoff, so the
// test does not wait it out.
func TestMaxRetries_UnsetKeepsTheSDKDefault(t *testing.T) {
	svc := newService(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-ms-retry-after-ms", "1")
		azureError(w, http.StatusServiceUnavailable, "ServerBusy")
	})
	cfg := testConfig(t, svc.endpoint(), nil)
	delete(cfg.Options, "max_retries")
	c := newClient(t, cfg)

	if err := c.Probe(t.Context()); !errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("Probe = %v, want ErrUnavailable", err)
	}
	if n := len(svc.Requests()); n != 4 {
		t.Errorf("service saw %d requests, want 4: one try and three retries", n)
	}
}
