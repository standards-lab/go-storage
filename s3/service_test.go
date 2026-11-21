package s3_test

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/standards-lab/go-storage"
)

const (
	testAccount = "admin"
	testKey     = "secret"
	testBucket  = "unit"
)

type service struct {
	srv     *httptest.Server
	respond http.HandlerFunc

	mu       sync.Mutex
	requests []recorded
}

type recorded struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   []byte
}

func newService(t *testing.T, respond http.HandlerFunc) *service {
	t.Helper()
	s := &service{respond: respond}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *service) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	s.mu.Lock()
	s.requests = append(s.requests, recorded{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Header: r.Header.Clone(),
		Body:   body,
	})
	s.mu.Unlock()
	s.respond(w, r)
}

func (s *service) endpoint() string {
	return s.srv.URL
}

func (s *service) Requests() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recorded(nil), s.requests...)
}

func s3Error(w http.ResponseWriter, r *http.Request, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>scripted by the test</Message><RequestId>unit</RequestId></Error>`, code)
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
	}
}

func failWith(status int, code string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s3Error(w, r, status, code)
	}
}

func byMethod(handlers map[string]http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h, ok := handlers[r.Method]
		if !ok {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		h(w, r)
	}
}

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
	return "http://" + addr
}

func testConfig(t *testing.T, endpoint string, options map[string]string) storage.Config {
	t.Helper()
	cfg := storage.Config{
		Endpoint:  endpoint,
		Container: testBucket,
		Account:   testAccount,
		Key:       testKey,
		Options:   map[string]string{"max_retries": "0"},
	}
	maps.Copy(cfg.Options, options)
	if err := cfg.Finalize(""); err != nil {
		t.Fatalf("finalize config: %v", err)
	}
	return cfg
}
