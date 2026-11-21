package azureblob_test

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-storage"
)

const (
	testAccount   = "devstoreaccount1"
	testKey       = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
	testContainer = "unit"
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

func blobPath(key string) string {
	return "/" + testAccount + "/" + testContainer + "/" + key
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
	return s.srv.URL + "/" + testAccount
}

func (s *service) Requests() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recorded(nil), s.requests...)
}

func azureError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("x-ms-error-code", code)
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><Error><Code>%s</Code><Message>scripted by the test</Message></Error>`, code)
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
	}
}

func failWith(status int, code string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		azureError(w, status, code)
	}
}

func blobHeaders(w http.ResponseWriter, etag, lastModified string) {
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", lastModified)
}

func blobStored(etag, lastModified string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("restype") == "container" {
			if r.Method == http.MethodPut {
				w.WriteHeader(http.StatusCreated)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		blobHeaders(w, etag, lastModified)
		w.WriteHeader(http.StatusCreated)
	}
}

type listPage struct {
	Items      []listItem
	NextMarker string
}

type listItem struct {
	Name         string
	Size         int64
	ContentType  string
	ETag         string
	LastModified string
}

func writeListPage(w http.ResponseWriter, page listPage) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><EnumerationResults ServiceEndpoint="http://127.0.0.1/` + testAccount + `/" ContainerName="` + testContainer + `"><Blobs>`)
	for _, it := range page.Items {
		fmt.Fprintf(&b, `<Blob><Name>%s</Name><Properties><Last-Modified>%s</Last-Modified><Etag>%s</Etag><Content-Length>%d</Content-Length><Content-Type>%s</Content-Type><BlobType>BlockBlob</BlobType></Properties></Blob>`,
			it.Name, it.LastModified, it.ETag, it.Size, it.ContentType)
	}
	b.WriteString(`</Blobs><NextMarker>` + page.NextMarker + `</NextMarker></EnumerationResults>`)
	_, _ = io.WriteString(w, b.String())
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
	return "http://" + addr + "/" + testAccount
}

func testConfig(t *testing.T, endpoint string, options map[string]string) storage.Config {
	t.Helper()
	cfg := storage.Config{
		Endpoint:  endpoint,
		Container: testContainer,
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
