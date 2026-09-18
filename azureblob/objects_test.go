package azureblob_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/standards-lab/go-storage"
	"github.com/standards-lab/go-storage/azureblob"
)

// The metadata the scripted service answers with, in the header form Azure
// uses (a quoted ETag and an RFC 1123 date) and the value it parses to.
const (
	testETag         = `"0x8DDF0E1C2B3A4D5"`
	testLastModified = "Fri, 18 Sep 2026 10:11:12 GMT"
)

var testModifiedAt = time.Date(2026, time.September, 18, 10, 11, 12, 0, time.UTC)

// readOnly hides every method of the underlying reader except Read, so the
// SDK sees a body it can neither seek nor measure.
type readOnly struct{ r io.Reader }

func (r readOnly) Read(p []byte) (int, error) { return r.r.Read(p) }

// failAfter yields n bytes and then fails with err, standing in for a body
// whose source broke mid-upload.
type failAfter struct {
	r   io.Reader
	err error
}

func (f *failAfter) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if errors.Is(err, io.EOF) {
		return n, f.err
	}
	return n, err
}

func newFailAfter(n int, err error) *failAfter {
	return &failAfter{r: bytes.NewReader(bytes.Repeat([]byte("x"), n)), err: err}
}

// wantObject asserts obj carries the scripted metadata for key and size.
func wantObject(t *testing.T, what string, obj storage.Object, key string, size int64, contentType string) {
	t.Helper()
	if obj.Key != key {
		t.Errorf("%s Key = %q, want %q", what, obj.Key, key)
	}
	if obj.Size != size {
		t.Errorf("%s Size = %d, want %d", what, obj.Size, size)
	}
	if obj.ContentType != contentType {
		t.Errorf("%s ContentType = %q, want %q", what, obj.ContentType, contentType)
	}
	if obj.ETag != testETag {
		t.Errorf("%s ETag = %q, want %q as the service sent it", what, obj.ETag, testETag)
	}
	if !obj.ModifiedAt.Equal(testModifiedAt) {
		t.Errorf("%s ModifiedAt = %v, want %v", what, obj.ModifiedAt, testModifiedAt)
	}
}

// blobRequests returns the recorded requests that addressed a blob rather
// than the container.
func blobRequests(reqs []recorded) []recorded {
	var out []recorded
	for _, r := range reqs {
		if !strings.Contains(r.Query, "restype=container") {
			out = append(out, r)
		}
	}
	return out
}

func TestPut_SendsBodyAndContentType(t *testing.T) {
	svc := newService(t, blobStored(testETag, testLastModified))
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	obj, err := c.Put(t.Context(), "dir/hello.txt", strings.NewReader("hello"), storage.PutOptions{ContentType: "text/plain", Size: 5})
	if err != nil {
		t.Fatalf("Put = %v, want nil", err)
	}
	wantObject(t, "Put", obj, "dir/hello.txt", 5, "text/plain")

	reqs := svc.Requests()
	if len(reqs) != 1 {
		t.Fatalf("service saw %d requests, want one Put Blob", len(reqs))
	}
	r := reqs[0]
	if r.Method != http.MethodPut || r.Path != blobPath("dir/hello.txt") {
		t.Errorf("request = %s %s, want PUT %s", r.Method, r.Path, blobPath("dir/hello.txt"))
	}
	if got := r.Header.Get("x-ms-blob-type"); got != "BlockBlob" {
		t.Errorf("x-ms-blob-type = %q, want BlockBlob", got)
	}
	if got := r.Header.Get("x-ms-blob-content-type"); got != "text/plain" {
		t.Errorf("x-ms-blob-content-type = %q, want text/plain", got)
	}
	if string(r.Body) != "hello" {
		t.Errorf("body = %q, want %q", r.Body, "hello")
	}
}

func TestPut_NoContentTypeSendsNoHeader(t *testing.T) {
	svc := newService(t, blobStored(testETag, testLastModified))
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	obj, err := c.Put(t.Context(), "k", strings.NewReader("hello"), storage.PutOptions{})
	if err != nil {
		t.Fatalf("Put = %v, want nil", err)
	}
	wantObject(t, "Put", obj, "k", 5, "")
	if _, ok := svc.Requests()[0].Header["X-Ms-Blob-Content-Type"]; ok {
		t.Error("x-ms-blob-content-type was sent for a Put without a ContentType")
	}
}

func TestPut_NonSeekableBody(t *testing.T) {
	svc := newService(t, blobStored(testETag, testLastModified))
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	content := []byte("delivered one byte per Read, with no Seek or Len")
	body := readOnly{iotest.OneByteReader(bytes.NewReader(content))}
	obj, err := c.Put(t.Context(), "k", body, storage.PutOptions{})
	if err != nil {
		t.Fatalf("Put = %v, want nil", err)
	}
	wantObject(t, "Put", obj, "k", int64(len(content)), "")
	if reqs := svc.Requests(); len(reqs) != 1 || !bytes.Equal(reqs[0].Body, content) {
		t.Fatalf("service saw %d requests, want one carrying the whole body", len(reqs))
	}
}

func TestPut_EmptyBody(t *testing.T) {
	svc := newService(t, blobStored(testETag, testLastModified))
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	obj, err := c.Put(t.Context(), "empty", bytes.NewReader(nil), storage.PutOptions{ContentType: "text/plain"})
	if err != nil {
		t.Fatalf("Put = %v, want nil", err)
	}
	wantObject(t, "Put", obj, "empty", 0, "text/plain")
	reqs := svc.Requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPut || len(reqs[0].Body) != 0 {
		t.Fatalf("service saw %+v, want one PUT with an empty body", reqs)
	}
}

func TestPut_MultipleBlocks(t *testing.T) {
	svc := newService(t, blobStored(testETag, testLastModified))
	c := newClient(t, testConfig(t, svc.endpoint(), map[string]string{"block_size": "1048576", "concurrency": "2"}))

	const size = 5 << 19 // 2.5 MiB: two full 1 MiB blocks and a half one
	content := bytes.Repeat([]byte{0xA5}, size)
	obj, err := c.Put(t.Context(), "big", readOnly{bytes.NewReader(content)}, storage.PutOptions{ContentType: "application/octet-stream"})
	if err != nil {
		t.Fatalf("Put = %v, want nil", err)
	}
	wantObject(t, "Put", obj, "big", size, "application/octet-stream")

	var blocks []int
	var commits []recorded
	for _, r := range svc.Requests() {
		q, err := url.ParseQuery(r.Query)
		if err != nil {
			t.Fatalf("parse query %q: %v", r.Query, err)
		}
		switch q.Get("comp") {
		case "block":
			if q.Get("blockid") == "" {
				t.Errorf("Put Block request %q carries no blockid", r.Query)
			}
			blocks = append(blocks, len(r.Body))
		case "blocklist":
			commits = append(commits, r)
		default:
			t.Errorf("unexpected request %s %s?%s", r.Method, r.Path, r.Query)
		}
	}
	// Blocks are staged concurrently, so only the multiset of sizes is fixed.
	var full, half int
	for _, n := range blocks {
		switch n {
		case 1 << 20:
			full++
		case 1 << 19:
			half++
		default:
			t.Errorf("staged a block of %d bytes, want 1 MiB or 512 KiB", n)
		}
	}
	if full != 2 || half != 1 {
		t.Errorf("staged %d full and %d half blocks, want 2 and 1", full, half)
	}
	if len(commits) != 1 {
		t.Fatalf("service saw %d Put Block List requests, want 1", len(commits))
	}
	commit := commits[0]
	if got := commit.Header.Get("x-ms-blob-content-type"); got != "application/octet-stream" {
		t.Errorf("Put Block List x-ms-blob-content-type = %q, want application/octet-stream", got)
	}
	if n := strings.Count(string(commit.Body), "<Latest>"); n != 3 {
		t.Errorf("block list commits %d blocks, want 3: %s", n, commit.Body)
	}
}

// wantNoCommit asserts none of reqs committed a blob: no Put Blob (a blob PUT
// with no comp) and no Put Block List.
func wantNoCommit(t *testing.T, reqs []recorded) {
	t.Helper()
	for _, r := range blobRequests(reqs) {
		if strings.Contains(r.Query, "comp=blocklist") || !strings.Contains(r.Query, "comp=") {
			t.Errorf("service saw %s %s?%s: a failed body must commit nothing", r.Method, r.Path, r.Query)
		}
	}
}

// wantCommit asserts reqs committed a blob exactly once, with a Put Blob when
// blocks is 0 and otherwise with that many staged blocks and one Put Block
// List.
func wantCommit(t *testing.T, reqs []recorded, blocks int) {
	t.Helper()
	var staged, commits, puts int
	for _, r := range blobRequests(reqs) {
		switch {
		case strings.Contains(r.Query, "comp=block&") || strings.HasSuffix(r.Query, "comp=block"):
			staged++
		case strings.Contains(r.Query, "comp=blocklist"):
			commits++
		case !strings.Contains(r.Query, "comp="):
			puts++
		}
	}
	if blocks == 0 && (puts != 1 || staged != 0 || commits != 0) {
		t.Errorf("service saw %d Put Blob, %d Put Block, %d Put Block List; want one Put Blob alone", puts, staged, commits)
	}
	if blocks > 0 && (puts != 0 || staged != blocks || commits != 1) {
		t.Errorf("service saw %d Put Blob, %d Put Block, %d Put Block List; want %d blocks and one Put Block List", puts, staged, commits, blocks)
	}
}

func TestPut_DeclaredSize(t *testing.T) {
	shapes := []struct {
		name    string
		length  int
		options map[string]string
		// blocks is the number of staged blocks a successful upload of the
		// body takes; 0 means one Put Blob.
		blocks int
	}{
		{"within one block", 32, nil, 0},
		{"across several blocks", 5 << 19, map[string]string{"block_size": "1048576"}, 3},
	}
	cases := []struct {
		name string
		// size is the declared Size for a body of n bytes.
		size func(n int) int64
		// wantShort expects the body to be reported short of its Size and
		// wantLong past it; neither means the Put succeeds.
		wantShort, wantLong bool
	}{
		{"Size equal to the body", func(n int) int64 { return int64(n) }, false, false},
		{"Size unknown", func(int) int64 { return 0 }, false, false},
		{"Size one byte longer than the body", func(n int) int64 { return int64(n) + 1 }, true, false},
		{"Size one byte shorter than the body", func(n int) int64 { return int64(n) - 1 }, false, true},
		{"Size half the body", func(n int) int64 { return int64(n) / 2 }, false, true},
	}
	for _, shape := range shapes {
		for _, tc := range cases {
			t.Run(shape.name+"/"+tc.name, func(t *testing.T) {
				svc := newService(t, blobStored(testETag, testLastModified))
				c := newClient(t, testConfig(t, svc.endpoint(), shape.options))
				content := bytes.Repeat([]byte{0x5A}, shape.length)
				size := tc.size(shape.length)

				obj, err := c.Put(t.Context(), "k", readOnly{bytes.NewReader(content)}, storage.PutOptions{Size: size})
				switch {
				case tc.wantShort:
					if !errors.Is(err, io.ErrUnexpectedEOF) {
						t.Fatalf("Put = %v, want io.ErrUnexpectedEOF wrapped", err)
					}
					want := fmt.Sprintf("body ended after %d bytes, short of the declared size (%d bytes)", shape.length, size)
					if !strings.Contains(err.Error(), want) {
						t.Errorf("Put = %v, want the message to contain %q", err, want)
					}
				case tc.wantLong:
					if err == nil || errors.Is(err, io.ErrUnexpectedEOF) {
						t.Fatalf("Put = %v, want an error for a body past its declared size", err)
					}
					want := fmt.Sprintf("body is longer than the declared size (%d bytes)", size)
					if !strings.Contains(err.Error(), want) {
						t.Errorf("Put = %v, want the message to contain %q", err, want)
					}
				default:
					if err != nil {
						t.Fatalf("Put = %v, want nil", err)
					}
					wantObject(t, "Put", obj, "k", int64(shape.length), "")
					wantCommit(t, svc.Requests(), shape.blocks)
					return
				}
				if errors.Is(err, storage.ErrUnavailable) {
					t.Errorf("Put = %v, want a size mismatch not to match ErrUnavailable", err)
				}
				if !strings.Contains(err.Error(), "read body") {
					t.Errorf("Put = %v, want the message to name the body read", err)
				}
				wantNoCommit(t, svc.Requests())
			})
		}
	}
}

func TestPut_BodyReadErrorIsNotUnavailable(t *testing.T) {
	errBody := errors.New("body source broke")
	cases := []struct {
		name    string
		bytes   int
		options map[string]string
	}{
		{"within one block", 16, nil},
		{"after a staged block", 3 << 19, map[string]string{"block_size": "1048576"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newService(t, blobStored(testETag, testLastModified))
			c := newClient(t, testConfig(t, svc.endpoint(), tc.options))

			_, err := c.Put(t.Context(), "k", newFailAfter(tc.bytes, errBody), storage.PutOptions{})
			if !errors.Is(err, errBody) {
				t.Fatalf("Put = %v, want the body's error matchable", err)
			}
			if errors.Is(err, storage.ErrUnavailable) {
				t.Fatalf("Put = %v, want a body failure not to match ErrUnavailable", err)
			}
			if !strings.Contains(err.Error(), "read body") {
				t.Errorf("Put = %v, want the message to name the body read", err)
			}
			wantNoCommit(t, svc.Requests())
		})
	}
}

func TestStore_PutSizeMismatch(t *testing.T) {
	svc := newService(t, blobStored(testETag, testLastModified))
	cfg := testConfig(t, svc.endpoint(), nil)
	store := storage.New(newClient(t, cfg), cfg)
	if err := store.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, err := store.Put(t.Context(), "short", strings.NewReader("abc"), storage.PutOptions{Size: 5})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("Store.Put of a short body = %v, want io.ErrUnexpectedEOF", err)
	}
	_, err = store.Put(t.Context(), "long", strings.NewReader("abcdef"), storage.PutOptions{Size: 5})
	if err == nil || !strings.Contains(err.Error(), "longer than the declared size (5 bytes)") {
		t.Errorf("Store.Put of a long body = %v, want the size error", err)
	}
	if reqs := blobRequests(svc.Requests()); len(reqs) != 0 {
		t.Errorf("service saw %d blob requests, want none for bodies that failed within one block", len(reqs))
	}
}

func TestStore_PutPastMaxObjectSizeIsTooLarge(t *testing.T) {
	svc := newService(t, blobStored(testETag, testLastModified))
	cfg := testConfig(t, svc.endpoint(), nil)
	cfg.MaxObjectSize = 16
	store := storage.New(newClient(t, cfg), cfg)
	if err := store.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	_, err := store.Put(t.Context(), "k", strings.NewReader(strings.Repeat("x", 32)), storage.PutOptions{})
	if !errors.Is(err, storage.ErrTooLarge) {
		t.Fatalf("Store.Put past the bound = %v, want ErrTooLarge", err)
	}
	if errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("Store.Put past the bound = %v, want it not to match ErrUnavailable", err)
	}
	if reqs := blobRequests(svc.Requests()); len(reqs) != 0 {
		t.Errorf("service saw %d blob requests, want none for a body that failed within one block", len(reqs))
	}
}

func TestPut_ServerError(t *testing.T) {
	svc := newService(t, failWith(http.StatusInternalServerError, "InternalError"))
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	_, err := c.Put(t.Context(), "k", strings.NewReader("x"), storage.PutOptions{})
	if !errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("Put on 500 = %v, want ErrUnavailable", err)
	}
}

func TestGet_MapsHeadersAndBody(t *testing.T) {
	svc := newService(t, func(w http.ResponseWriter, r *http.Request) {
		blobHeaders(w, testETag, testLastModified)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "5")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "hello")
	})
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	blob, err := c.Get(t.Context(), "dir/hello.txt", storage.GetOptions{})
	if err != nil {
		t.Fatalf("Get = %v, want nil", err)
	}
	wantObject(t, "Get", blob.Object, "dir/hello.txt", 5, "text/plain")
	data, err := io.ReadAll(blob.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := blob.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("body = %q, want %q", data, "hello")
	}
	reqs := svc.Requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodGet || reqs[0].Path != blobPath("dir/hello.txt") {
		t.Fatalf("service saw %+v, want one GET of the blob", reqs)
	}
}

func TestGet_NotFound(t *testing.T) {
	svc := newService(t, failWith(http.StatusNotFound, "BlobNotFound"))
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	blob, err := c.Get(t.Context(), "missing", storage.GetOptions{})
	if err == nil {
		_ = blob.Body.Close()
	}
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Get of a missing blob = %v, want ErrNotFound", err)
	}
}

func TestStat_MapsHeaders(t *testing.T) {
	svc := newService(t, func(w http.ResponseWriter, r *http.Request) {
		blobHeaders(w, testETag, testLastModified)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "1234")
		w.WriteHeader(http.StatusOK)
	})
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	obj, err := c.Stat(t.Context(), "doc.json")
	if err != nil {
		t.Fatalf("Stat = %v, want nil", err)
	}
	wantObject(t, "Stat", obj, "doc.json", 1234, "application/json")
	reqs := svc.Requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodHead || reqs[0].Path != blobPath("doc.json") {
		t.Fatalf("service saw %+v, want one HEAD of the blob", reqs)
	}
}

func TestStat_NotFound(t *testing.T) {
	svc := newService(t, failWith(http.StatusNotFound, "BlobNotFound"))
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	if _, err := c.Stat(t.Context(), "missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Stat of a missing blob = %v, want ErrNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	cases := []struct {
		name    string
		respond http.HandlerFunc
		want    error // the sentinel the error must match; nil means success
	}{
		{"accepted", status(http.StatusAccepted), nil},
		{"blob not found", failWith(http.StatusNotFound, "BlobNotFound"), nil},
		{"container not found", failWith(http.StatusNotFound, "ContainerNotFound"), storage.ErrNotFound},
		{"server error", failWith(http.StatusInternalServerError, "InternalError"), storage.ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newService(t, tc.respond)
			c := newClient(t, testConfig(t, svc.endpoint(), nil))

			err := c.Delete(t.Context(), "gone")
			if tc.want == nil && err != nil {
				t.Fatalf("Delete = %v, want nil", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("Delete = %v, want %v", err, tc.want)
			}
			reqs := svc.Requests()
			if len(reqs) != 1 || reqs[0].Method != http.MethodDelete || reqs[0].Path != blobPath("gone") {
				t.Fatalf("service saw %+v, want one DELETE of the blob", reqs)
			}
		})
	}
}

func TestList_Paging(t *testing.T) {
	pages := map[string]listPage{
		"": {
			Items: []listItem{
				{Name: "p/a", Size: 3, ContentType: "text/plain", ETag: "0x1", LastModified: testLastModified},
				{Name: "p/b", Size: 4, ContentType: "application/json", ETag: "0x2", LastModified: testLastModified},
			},
			NextMarker: "2!76!token-for-page-two",
		},
		"2!76!token-for-page-two": {
			Items: []listItem{{Name: "p/c", Size: 5, ETag: "0x3", LastModified: testLastModified}},
		},
	}
	svc := newService(t, func(w http.ResponseWriter, r *http.Request) {
		page, ok := pages[r.URL.Query().Get("marker")]
		if !ok {
			azureError(w, http.StatusBadRequest, "OutOfRangeInput")
			return
		}
		writeListPage(w, page)
	})
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	first, err := c.List(t.Context(), storage.ListOptions{Prefix: "p/", Limit: 2})
	if err != nil {
		t.Fatalf("List(first page) = %v, want nil", err)
	}
	if first.Next != "2!76!token-for-page-two" {
		t.Errorf("first Next = %q, want the service's NextMarker verbatim", first.Next)
	}
	if len(first.Objects) != 2 {
		t.Fatalf("first page holds %d objects, want 2", len(first.Objects))
	}
	want := storage.Object{Key: "p/a", Size: 3, ContentType: "text/plain", ETag: `"0x1"`, ModifiedAt: testModifiedAt}
	if got := first.Objects[0]; got != want {
		t.Errorf("first object = %+v, want %+v", got, want)
	}
	if got := first.Objects[1]; got.Key != "p/b" || got.Size != 4 || got.ContentType != "application/json" || got.ETag != `"0x2"` {
		t.Errorf("second object = %+v, want p/b, 4 bytes, application/json, \"0x2\"", got)
	}

	second, err := c.List(t.Context(), storage.ListOptions{Prefix: "p/", Token: first.Next, Limit: 2})
	if err != nil {
		t.Fatalf("List(second page) = %v, want nil", err)
	}
	if second.Next != "" {
		t.Errorf("last page Next = %q, want empty", second.Next)
	}
	if len(second.Objects) != 1 || second.Objects[0].Key != "p/c" {
		t.Errorf("second page = %+v, want the one object p/c", second.Objects)
	}

	reqs := svc.Requests()
	if len(reqs) != 2 {
		t.Fatalf("service saw %d requests, want exactly one per List call", len(reqs))
	}
	for i, r := range reqs {
		q, err := url.ParseQuery(r.Query)
		if err != nil {
			t.Fatalf("parse query %q: %v", r.Query, err)
		}
		if r.Method != http.MethodGet || q.Get("restype") != "container" || q.Get("comp") != "list" {
			t.Errorf("request %d = %s %s?%s, want a List Blobs GET", i+1, r.Method, r.Path, r.Query)
		}
		if q.Get("prefix") != "p/" {
			t.Errorf("request %d prefix = %q, want p/", i+1, q.Get("prefix"))
		}
		if q.Get("maxresults") != "2" {
			t.Errorf("request %d maxresults = %q, want 2", i+1, q.Get("maxresults"))
		}
	}
	if _, ok := reqs[0].Header["Marker"]; ok || strings.Contains(reqs[0].Query, "marker=") {
		t.Errorf("first request carried a marker: %q", reqs[0].Query)
	}
	if q, _ := url.ParseQuery(reqs[1].Query); q.Get("marker") != "2!76!token-for-page-two" {
		t.Errorf("second request marker = %q, want the token passed back verbatim", q.Get("marker"))
	}
}

func TestList_ETagMatchesStat(t *testing.T) {
	// The service quotes the ETag in the Get Blob Properties header and leaves
	// it unquoted in the listing's XML; the client reports one form.
	svc := newService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("comp") == "list" {
			writeListPage(w, listPage{Items: []listItem{{Name: "k", Size: 1, ETag: strings.Trim(testETag, `"`), LastModified: testLastModified}}})
			return
		}
		blobHeaders(w, testETag, testLastModified)
		w.Header().Set("Content-Length", "1")
		w.WriteHeader(http.StatusOK)
	})
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	page, err := c.List(t.Context(), storage.ListOptions{})
	if err != nil {
		t.Fatalf("List = %v, want nil", err)
	}
	if len(page.Objects) != 1 {
		t.Fatalf("List returned %d objects, want 1", len(page.Objects))
	}
	stat, err := c.Stat(t.Context(), "k")
	if err != nil {
		t.Fatalf("Stat = %v, want nil", err)
	}
	if got := page.Objects[0].ETag; got != testETag {
		t.Errorf("List ETag = %q, want the quoted %q", got, testETag)
	}
	if page.Objects[0].ETag != stat.ETag {
		t.Errorf("List ETag %q differs from Stat ETag %q for the same version", page.Objects[0].ETag, stat.ETag)
	}
}

func TestList_NoLimitLeavesMaxResultsUnset(t *testing.T) {
	svc := newService(t, func(w http.ResponseWriter, _ *http.Request) { writeListPage(w, listPage{}) })
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	page, err := c.List(t.Context(), storage.ListOptions{})
	if err != nil {
		t.Fatalf("List = %v, want nil", err)
	}
	if len(page.Objects) != 0 || page.Next != "" {
		t.Errorf("List of an empty container = %+v, want no objects and an empty Next", page)
	}
	q, _ := url.ParseQuery(svc.Requests()[0].Query)
	if _, ok := q["maxresults"]; ok {
		t.Errorf("query = %q, want no maxresults for a Limit of 0", svc.Requests()[0].Query)
	}
	if _, ok := q["prefix"]; ok {
		t.Errorf("query = %q, want no prefix for an empty Prefix", svc.Requests()[0].Query)
	}
}

func TestList_ClampsLimitToInt32(t *testing.T) {
	svc := newService(t, func(w http.ResponseWriter, _ *http.Request) { writeListPage(w, listPage{}) })
	c := newClient(t, testConfig(t, svc.endpoint(), nil))

	if _, err := c.List(t.Context(), storage.ListOptions{Limit: 1 << 40}); err != nil {
		t.Fatalf("List = %v, want nil", err)
	}
	q, _ := url.ParseQuery(svc.Requests()[0].Query)
	if got := q.Get("maxresults"); got != strconv.Itoa(1<<31-1) {
		t.Errorf("maxresults = %q, want the int32 maximum", got)
	}
}

func TestList_Errors(t *testing.T) {
	cases := []struct {
		name    string
		respond http.HandlerFunc
		want    error
	}{
		{"container not found", failWith(http.StatusNotFound, "ContainerNotFound"), storage.ErrNotFound},
		{"server error", failWith(http.StatusInternalServerError, "InternalError"), storage.ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newService(t, tc.respond)
			c := newClient(t, testConfig(t, svc.endpoint(), nil))
			if _, err := c.List(t.Context(), storage.ListOptions{}); !errors.Is(err, tc.want) {
				t.Fatalf("List = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNew_RejectsBadUploadOptions(t *testing.T) {
	cases := []struct {
		key    string
		values []string
	}{
		{"block_size", []string{"0", "-1", "x", "1048575", "104857601", ""}},
		{"concurrency", []string{"0", "-1", "x", "1.5", "33", ""}},
	}
	for _, tc := range cases {
		for _, v := range tc.values {
			t.Run(tc.key+"="+v, func(t *testing.T) {
				cfg := testConfig(t, "http://127.0.0.1:10000/"+testAccount, map[string]string{tc.key: v})
				_, err := azureblob.New(cfg)
				if err == nil || !strings.Contains(err.Error(), tc.key) {
					t.Fatalf("New = %v, want an error naming %s", err, tc.key)
				}
			})
		}
	}
}
