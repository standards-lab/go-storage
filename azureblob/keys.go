package azureblob

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The blob-name limits the package documentation lists.
const (
	maxKeyLength   = 1024
	maxKeySegments = 254
)

// The container-name length limits the package documentation lists.
const (
	minContainerLength = 3
	maxContainerLength = 63
)

// validateKey reports whether the service accepts key as a blob name, with an
// error that says which rule it breaks.
func validateKey(key string) error {
	if key == "" {
		return errors.New("azureblob: empty key")
	}
	if n := utf8.RuneCountInString(key); n > maxKeyLength {
		return fmt.Errorf("azureblob: key is %d characters, the limit is %d", n, maxKeyLength)
	}
	if n := strings.Count(key, "/") + 1; n > maxKeySegments {
		return fmt.Errorf("azureblob: key has %d path segments, the limit is %d", n, maxKeySegments)
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
	for segment := range strings.SplitSeq(key, "/") {
		if strings.HasSuffix(segment, ".") {
			return fmt.Errorf("azureblob: key path segment %q ends in a dot", segment)
		}
	}
	return nil
}

// validateContainer reports whether the service accepts name as a container
// name, with an error that says which rule it breaks.
func validateContainer(name string) error {
	if n := len(name); n < minContainerLength || n > maxContainerLength {
		return fmt.Errorf("azureblob: container name %q is %d characters, want %d to %d", name, n, minContainerLength, maxContainerLength)
	}
	for i := 0; i < len(name); i++ {
		switch b := name[i]; {
		case b >= 'a' && b <= 'z', b >= '0' && b <= '9':
		case b == '-':
			if i == 0 || i == len(name)-1 || name[i-1] == '-' {
				return fmt.Errorf("azureblob: container name %q has a hyphen at its start or end or next to another", name)
			}
		default:
			return fmt.Errorf("azureblob: container name %q has %q; want lowercase letters, digits, and hyphens", name, b)
		}
	}
	return nil
}
