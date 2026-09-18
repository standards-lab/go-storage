package azureblob_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/standards-lab/go-storage"
)

// The well-known Azurite development account and its published key. The key
// is public, so it is safe in source; only the signature's shape is asserted.
const (
	testAccount   = "devstoreaccount1"
	testKey       = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
	testContainer = "unit"
)

// service is a scripted stand-in for the Blob service: an httptest server
// whose handler answers every request through respond and records what it
// received. Its endpoint is path-style, the way Azurite's is, so the account
// name is the first path segment.
type service struct {
	srv     *httptest.Server
	respond http.HandlerFunc

	mu       sync.Mutex
	requests []recorded
}

// recorded is what a test can assert about one request the service saw.
type recorded struct {
	Method string
	Path   string
	Query  string
	Header http.Header
}

// newService starts a service that answers with respond and stops it when
// the test ends.
func newService(t *testing.T, respond http.HandlerFunc) *service {
	t.Helper()
	s := &service{respond: respond}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *service) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, recorded{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Header: r.Header.Clone(),
	})
	s.mu.Unlock()
	s.respond(w, r)
}

// endpoint is the path-style service URL a Config's Endpoint takes.
func (s *service) endpoint() string {
	return s.srv.URL + "/" + testAccount
}

// Requests returns a copy of every request the service has recorded.
func (s *service) Requests() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recorded(nil), s.requests...)
}

// azureError writes the failure shape the Blob service uses: the status, the
// x-ms-error-code header, and the XML body that repeats the code.
func azureError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("x-ms-error-code", code)
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><Error><Code>%s</Code><Message>scripted by the test</Message></Error>`, code)
}

// status answers every request with one status and no body.
func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
	}
}

// failWith answers every request with one Azure error.
func failWith(status int, code string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		azureError(w, status, code)
	}
}

// closedEndpoint returns a path-style endpoint on a port nothing listens on:
// a listener is opened on port 0 to learn a free port and closed again.
func closedEndpoint(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return "http://" + addr + "/" + testAccount
}

// testConfig returns a finalized Config aimed at endpoint with retries off,
// so a scripted failure surfaces on the first try. options overlays further
// provider settings.
func testConfig(t *testing.T, endpoint string, options map[string]string) storage.Config {
	t.Helper()
	cfg := storage.Config{
		Endpoint:  endpoint,
		Container: testContainer,
		Account:   testAccount,
		Key:       testKey,
		Options:   map[string]string{"max_retries": "0"},
	}
	for k, v := range options {
		cfg.Options[k] = v
	}
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("finalize config: %v", err)
	}
	return cfg
}
