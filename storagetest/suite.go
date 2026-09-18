package storagetest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand/v2"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/standards-lab/go-storage"
)

// cleanupTimeout bounds each delete the suite issues while a subtest cleans
// up, because the subtest's own context is already cancelled by then.
const cleanupTimeout = 30 * time.Second

// largeBodySize is the length of the one body the suite stores that is
// larger than a typical single upload block, so a provider that splits a body
// into blocks is exercised. 3 MiB clears the 1 MiB default block of the
// Azure SDK's streaming upload with room for a second and a third block.
const largeBodySize = 3 << 20

// maxPages bounds a listing walk so a provider that never returns an empty
// Next fails the test instead of hanging it.
const maxPages = 100

// Run is the conformance suite for a storage.Client. It runs one subtest per
// contract behavior, calling newClient for each one and EnsureContainer on
// the client it returns, so the container newClient's client is wired to may
// not exist yet. Every key the suite writes sits under a random prefix of its
// own, and every subtest deletes what it wrote when it ends, so the suite can
// run against a shared container that holds other objects.
//
// The suite asserts the contract [storage.Client] documents and nothing a
// provider is free to choose: List results are compared as sets, so listing
// order is not asserted, and ModifiedAt is asserted non-zero and consistent
// across Put, Get, and Stat, never close to the wall clock. It does not
// assert what a provider does with a PutOptions.Size that is shorter or
// longer than the body, because that is adapter-specific and each provider's
// documentation states its own behavior.
func Run(t *testing.T, newClient func(t *testing.T) storage.Client) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, newClient(t))
		})
	}
}

// testCase is one contract behavior. check receives a client whose container
// exists and a prefix every key it writes must start with.
type testCase struct {
	name  string
	check func(t testing.TB, c storage.Client, prefix string)
}

// run ensures the container and then runs the case under a fresh prefix. It
// takes a testing.TB so the package's own tests can run one case through a
// recorder and assert that a broken client fails it.
func (tc testCase) run(t testing.TB, c storage.Client) {
	t.Helper()
	if err := c.EnsureContainer(t.Context()); err != nil {
		t.Fatalf("EnsureContainer: %v", err)
	}
	tc.check(t, c, newPrefix(t))
}

var cases = []testCase{
	{"EnsureContainer", checkEnsureContainer},
	{"RoundTrip", checkRoundTrip},
	{"LargeBody", checkLargeBody},
	{"EmptyBody", checkEmptyBody},
	{"Replace", checkReplace},
	{"MissingKey", checkMissingKey},
	{"Delete", checkDelete},
	{"List", checkList},
	{"PutDeclaredSize", checkPutDeclaredSize},
	{"PutUnknownSize", checkPutUnknownSize},
	{"PutNonSeekableBody", checkPutNonSeekableBody},
	{"PutOneByteReads", checkPutOneByteReads},
	{"Capabilities", checkCapabilities},
}

// newPrefix returns a key prefix no other run shares. It is ASCII letters,
// digits, and slashes, so it satisfies any provider's key rules.
func newPrefix(t testing.TB) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("random prefix: %v", err)
	}
	return "storagetest/" + hex.EncodeToString(b[:]) + "/"
}

// put stores body at key with opts, registers a cleanup that deletes the
// key, and returns what Put reported.
func put(t testing.TB, c storage.Client, key string, body io.Reader, opts storage.PutOptions) storage.Object {
	t.Helper()
	deleteOnCleanup(t, c, key)
	obj, err := c.Put(t.Context(), key, body, opts)
	if err != nil {
		t.Fatalf("Put(%q): %v", key, err)
	}
	return obj
}

// deleteOnCleanup registers a cleanup that removes key. The delete's error is
// ignored: cleanup is best effort, and a key the test never wrote is a no-op.
func deleteOnCleanup(t testing.TB, c storage.Client, key string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		_ = c.Delete(ctx, key)
	})
}

// get reads and closes the object at key and returns its content and
// metadata.
func get(t testing.TB, c storage.Client, key string) ([]byte, storage.Object) {
	t.Helper()
	blob, err := c.Get(t.Context(), key, storage.GetOptions{})
	if err != nil {
		t.Fatalf("Get(%q): %v", key, err)
	}
	data, err := io.ReadAll(blob.Body)
	if err != nil {
		_ = blob.Body.Close()
		t.Fatalf("read Get(%q) body: %v", key, err)
	}
	if err := blob.Body.Close(); err != nil {
		t.Fatalf("close Get(%q) body: %v", key, err)
	}
	return data, blob.Object
}

// stat returns the metadata of the object at key.
func stat(t testing.TB, c storage.Client, key string) storage.Object {
	t.Helper()
	obj, err := c.Stat(t.Context(), key)
	if err != nil {
		t.Fatalf("Stat(%q): %v", key, err)
	}
	return obj
}

// wantObject asserts got describes key with the given size and content type,
// carries a non-empty ETag and a non-zero ModifiedAt, and agrees with want on
// the ETag and ModifiedAt. what names the call that produced got. An empty
// contentType skips the ContentType check: a provider may apply a default of
// its own to an object stored without one, and Put has no way to report it.
func wantObject(t testing.TB, what string, got, want storage.Object, key string, size int64, contentType string) {
	t.Helper()
	if got.Key != key {
		t.Errorf("%s Key = %q, want %q", what, got.Key, key)
	}
	if got.Size != size {
		t.Errorf("%s Size = %d, want %d", what, got.Size, size)
	}
	if contentType != "" && got.ContentType != contentType {
		t.Errorf("%s ContentType = %q, want %q", what, got.ContentType, contentType)
	}
	if got.ETag == "" {
		t.Errorf("%s ETag is empty", what)
	} else if got.ETag != want.ETag {
		t.Errorf("%s ETag = %q, want %q as Put reported", what, got.ETag, want.ETag)
	}
	if got.ModifiedAt.IsZero() {
		t.Errorf("%s ModifiedAt is zero", what)
	} else if !got.ModifiedAt.Equal(want.ModifiedAt) {
		t.Errorf("%s ModifiedAt = %v, want %v as Put reported", what, got.ModifiedAt, want.ModifiedAt)
	}
}

// wantContent asserts Get and Stat of key agree with put on the content and
// the metadata. An empty contentType skips the ContentType check, as in
// wantObject.
func wantContent(t testing.TB, c storage.Client, key string, put storage.Object, content []byte, contentType string) {
	t.Helper()
	size := int64(len(content))
	wantObject(t, "Put", put, put, key, size, contentType)

	data, obj := get(t, c, key)
	if !bytes.Equal(data, content) {
		t.Errorf("Get(%q) returned %d bytes, want the %d bytes written%s", key, len(data), len(content), firstDifference(data, content))
	}
	wantObject(t, "Get", obj, put, key, size, contentType)

	wantObject(t, "Stat", stat(t, c, key), put, key, size, contentType)
}

// firstDifference describes where got and want diverge, for a failure
// message that stays readable on a large body.
func firstDifference(got, want []byte) string {
	n := min(len(got), len(want))
	for i := range n {
		if got[i] != want[i] {
			return fmt.Sprintf(" (first difference at byte %d)", i)
		}
	}
	if len(got) != len(want) {
		return fmt.Sprintf(" (equal through byte %d)", n)
	}
	return ""
}

// wantNotFound asserts err matches storage.ErrNotFound.
func wantNotFound(t testing.TB, what string, err error) {
	t.Helper()
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("%s = %v, want ErrNotFound", what, err)
	}
}

// listAll walks a listing under prefix to its final page and returns every
// object seen, in the order seen, and the number of pages. It fails the test
// on a page longer than limit, on a key outside prefix, and on a walk that
// does not end within maxPages.
func listAll(t testing.TB, c storage.Client, prefix string, limit int) ([]storage.Object, int) {
	t.Helper()
	var objects []storage.Object
	pages := 0
	token := ""
	for {
		page, err := c.List(t.Context(), storage.ListOptions{Prefix: prefix, Token: token, Limit: limit})
		if err != nil {
			t.Fatalf("List(page %d): %v", pages+1, err)
		}
		pages++
		if limit > 0 && len(page.Objects) > limit {
			t.Errorf("List page %d holds %d objects, want at most the Limit %d", pages, len(page.Objects), limit)
		}
		for _, obj := range page.Objects {
			if !strings.HasPrefix(obj.Key, prefix) {
				t.Errorf("List(%q) returned key %q outside the prefix", prefix, obj.Key)
			}
		}
		objects = append(objects, page.Objects...)
		if page.Next == "" {
			return objects, pages
		}
		token = page.Next
		if pages >= maxPages {
			t.Fatalf("List never returned an empty Next within %d pages", maxPages)
		}
	}
}

// wantKeys asserts objects holds exactly the keys in want, each once, with
// the Size that put reported for it.
func wantKeys(t testing.TB, what string, objects []storage.Object, want map[string]storage.Object) {
	t.Helper()
	seen := make(map[string]int, len(objects))
	for _, obj := range objects {
		seen[obj.Key]++
		if seen[obj.Key] > 1 {
			t.Errorf("%s returned key %q %d times", what, obj.Key, seen[obj.Key])
			continue
		}
		expected, ok := want[obj.Key]
		if !ok {
			t.Errorf("%s returned key %q that was not written under the prefix", what, obj.Key)
			continue
		}
		if obj.Size != expected.Size {
			t.Errorf("%s reports Size %d for %q, want %d as Put reported", what, obj.Size, obj.Key, expected.Size)
		}
	}
	for key := range want {
		if seen[key] == 0 {
			t.Errorf("%s did not return key %q", what, key)
		}
	}
}

// readOnly hides every method of the underlying reader except Read, so a
// provider sees a body it can neither seek nor measure.
type readOnly struct{ r io.Reader }

func (r readOnly) Read(p []byte) (int, error) { return r.r.Read(p) }

// largeBody returns largeBodySize bytes of deterministic pseudo-random
// content.
func largeBody() []byte {
	data := make([]byte, largeBodySize)
	r := mathrand.New(mathrand.NewPCG(0x5701a6e7e57, 0xb0d1))
	for i := 0; i < len(data); i += 8 {
		v := r.Uint64()
		for j := range 8 {
			if i+j < len(data) {
				data[i+j] = byte(v >> (8 * j))
			}
		}
	}
	return data
}

func checkEnsureContainer(t testing.TB, c storage.Client, _ string) {
	t.Helper()
	ctx := t.Context()
	for i := range 2 {
		if err := c.EnsureContainer(ctx); err != nil {
			t.Fatalf("EnsureContainer call %d over an existing container: %v", i+1, err)
		}
	}
	if err := c.Probe(ctx); err != nil {
		t.Errorf("Probe after EnsureContainer = %v, want nil", err)
	}
}

func checkRoundTrip(t testing.TB, c storage.Client, prefix string) {
	t.Helper()
	key := prefix + "hello.txt"
	content := []byte("hello, storage")
	obj := put(t, c, key, bytes.NewReader(content), storage.PutOptions{ContentType: "text/plain", Size: int64(len(content))})
	wantContent(t, c, key, obj, content, "text/plain")
}

func checkLargeBody(t testing.TB, c storage.Client, prefix string) {
	t.Helper()
	key := prefix + "large.bin"
	content := largeBody()
	obj := put(t, c, key, bytes.NewReader(content), storage.PutOptions{ContentType: "application/octet-stream", Size: int64(len(content))})
	wantContent(t, c, key, obj, content, "application/octet-stream")
}

func checkEmptyBody(t testing.TB, c storage.Client, prefix string) {
	t.Helper()
	key := prefix + "empty"
	obj := put(t, c, key, bytes.NewReader(nil), storage.PutOptions{ContentType: "text/plain"})
	wantContent(t, c, key, obj, []byte{}, "text/plain")
}

func checkReplace(t testing.TB, c storage.Client, prefix string) {
	t.Helper()
	key := prefix + "replaced"
	first := put(t, c, key, strings.NewReader("first"), storage.PutOptions{ContentType: "text/plain"})

	content := []byte(`{"second": true}`)
	second := put(t, c, key, bytes.NewReader(content), storage.PutOptions{ContentType: "application/json"})
	if second.ETag == first.ETag {
		t.Errorf("ETag %q unchanged after a replace with different content", second.ETag)
	}
	wantContent(t, c, key, second, content, "application/json")

	objects, _ := listAll(t, c, prefix, 0)
	wantKeys(t, "List after the replace", objects, map[string]storage.Object{key: second})
}

func checkMissingKey(t testing.TB, c storage.Client, prefix string) {
	t.Helper()
	key := prefix + "missing"
	blob, err := c.Get(t.Context(), key, storage.GetOptions{})
	if err == nil {
		_ = blob.Body.Close()
	}
	wantNotFound(t, "Get of a missing key", err)

	_, err = c.Stat(t.Context(), key)
	wantNotFound(t, "Stat of a missing key", err)
}

func checkDelete(t testing.TB, c storage.Client, prefix string) {
	t.Helper()
	ctx := t.Context()
	key := prefix + "deleted"
	put(t, c, key, strings.NewReader("gone soon"), storage.PutOptions{})

	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete(%q): %v", key, err)
	}
	_, err := c.Stat(ctx, key)
	wantNotFound(t, "Stat after Delete", err)
	blob, err := c.Get(ctx, key, storage.GetOptions{})
	if err == nil {
		_ = blob.Body.Close()
	}
	wantNotFound(t, "Get after Delete", err)

	if err := c.Delete(ctx, key); err != nil {
		t.Errorf("second Delete(%q) = %v, want nil", key, err)
	}
	if err := c.Delete(ctx, prefix+"never-written"); err != nil {
		t.Errorf("Delete of a never-written key = %v, want nil", err)
	}
}

func checkList(t testing.TB, c storage.Client, prefix string) {
	t.Helper()
	under := map[string]storage.Object{}
	for _, name := range []string{"a", "b/1", "b/2", "b/3", "c", "d", "e"} {
		key := prefix + name
		under[key] = put(t, c, key, strings.NewReader(name+" content"), storage.PutOptions{})
	}
	// A key under a sibling prefix must never appear in a listing of prefix.
	sibling := strings.TrimSuffix(prefix, "/") + "-sibling/other"
	put(t, c, sibling, strings.NewReader("other"), storage.PutOptions{})

	objects, _ := listAll(t, c, prefix, 0)
	wantKeys(t, "List with the default page size", objects, under)

	objects, pages := listAll(t, c, prefix, 2)
	wantKeys(t, "List with Limit 2", objects, under)
	if pages < 4 {
		t.Errorf("List with Limit 2 walked %d keys in %d pages, want at least 4", len(under), pages)
	}

	nested := prefix + "b/"
	objects, _ = listAll(t, c, nested, 0)
	wantKeys(t, "List of a nested prefix", objects, map[string]storage.Object{
		prefix + "b/1": under[prefix+"b/1"],
		prefix + "b/2": under[prefix+"b/2"],
		prefix + "b/3": under[prefix+"b/3"],
	})

	page, err := c.List(t.Context(), storage.ListOptions{Prefix: prefix + "nothing-here/"})
	if err != nil {
		t.Fatalf("List of an unmatched prefix: %v", err)
	}
	if len(page.Objects) != 0 || page.Next != "" {
		t.Errorf("List of an unmatched prefix = %+v, want no objects and an empty Next", page)
	}
}

func checkPutDeclaredSize(t testing.TB, c storage.Client, prefix string) {
	t.Helper()
	key := prefix + "declared"
	content := []byte("a body whose length the caller declared")
	obj := put(t, c, key, bytes.NewReader(content), storage.PutOptions{Size: int64(len(content))})
	wantContent(t, c, key, obj, content, "")
}

func checkPutUnknownSize(t testing.TB, c storage.Client, prefix string) {
	t.Helper()
	key := prefix + "unknown"
	content := []byte("a body whose length the caller did not declare")
	obj := put(t, c, key, bytes.NewReader(content), storage.PutOptions{})
	wantContent(t, c, key, obj, content, "")
}

func checkPutNonSeekableBody(t testing.TB, c storage.Client, prefix string) {
	t.Helper()
	key := prefix + "non-seekable"
	content := largeBody()[:largeBodySize/2]
	obj := put(t, c, key, readOnly{bytes.NewReader(content)}, storage.PutOptions{ContentType: "application/octet-stream"})
	wantContent(t, c, key, obj, content, "application/octet-stream")
}

func checkPutOneByteReads(t testing.TB, c storage.Client, prefix string) {
	t.Helper()
	key := prefix + "one-byte-reads"
	content := []byte("delivered one byte per Read")
	obj := put(t, c, key, iotest.OneByteReader(bytes.NewReader(content)), storage.PutOptions{})
	wantContent(t, c, key, obj, content, "")
}

func checkCapabilities(t testing.TB, c storage.Client, prefix string) {
	t.Helper()
	caps := c.Capabilities()
	if caps.MaxKeyLength <= 0 {
		t.Errorf("Capabilities().MaxKeyLength = %d, want positive", caps.MaxKeyLength)
	}
	if caps.ValidateKey == nil {
		t.Fatal("Capabilities().ValidateKey is nil, want a function")
	}
	if err := caps.ValidateKey(prefix + "ordinary-key.txt"); err != nil {
		t.Errorf("ValidateKey of an ordinary key = %v, want nil", err)
	}
	if err := caps.ValidateKey(""); err == nil {
		t.Error("ValidateKey of an empty key = nil, want an error")
	}
	if caps.MaxKeyLength > 0 {
		long := strings.Repeat("a", caps.MaxKeyLength+1)
		if err := caps.ValidateKey(long); err == nil {
			t.Errorf("ValidateKey of a %d-character key = nil, want an error past MaxKeyLength %d", len(long), caps.MaxKeyLength)
		}
	}
}
