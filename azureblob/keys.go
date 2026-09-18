package azureblob

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The blob-name limits the Azure Blob service documents. MaxKeyLength is
// the documented 1,024-character limit counted in runes; MaxKeySegments is
// the documented limit on path segments for an account without a
// hierarchical namespace.
const (
	MaxKeyLength   = 1024
	MaxKeySegments = 254
)

// validateKey reports whether the Azure Blob service accepts key as a blob
// name, with an error that says which rule it breaks. A key is non-empty, at
// most MaxKeyLength runes, at most MaxKeySegments path segments separated by
// forward slashes, free of control characters (U+0000 through U+001F and
// U+007F through U+009F), and does not end in a dot, a forward slash, or a
// backslash; no path segment ends in a dot either. It is the ValidateKey a
// Client's Capabilities carries, so a consumer reaches it through
// storage.Store.
func validateKey(key string) error {
	if key == "" {
		return errors.New("azureblob: empty key")
	}
	if n := utf8.RuneCountInString(key); n > MaxKeyLength {
		return fmt.Errorf("azureblob: key is %d characters, the limit is %d", n, MaxKeyLength)
	}
	if n := strings.Count(key, "/") + 1; n > MaxKeySegments {
		return fmt.Errorf("azureblob: key has %d path segments, the limit is %d", n, MaxKeySegments)
	}
	for i, r := range key {
		if unicode.IsControl(r) {
			return fmt.Errorf("azureblob: key has a control character %U at byte %d", r, i)
		}
	}
	switch key[len(key)-1] {
	case '.', '/', '\\':
		return fmt.Errorf("azureblob: key ends in %q", key[len(key)-1])
	}
	for _, segment := range strings.Split(key, "/") {
		if strings.HasSuffix(segment, ".") {
			return fmt.Errorf("azureblob: key path segment %q ends in a dot", segment)
		}
	}
	return nil
}
