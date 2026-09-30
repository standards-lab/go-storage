package azureblob

import (
	"strings"
	"testing"
)

func TestValidateKey(t *testing.T) {
	// A 1,024-rune key of two-byte runes is 2,048 bytes, so a byte count
	// would reject it.
	longest := strings.Repeat("é", maxKeyLength)
	mostSegments := strings.Repeat("a/", maxKeySegments-1) + "a"

	valid := []struct{ name, key string }{
		{"storagetest prefix form", "storagetest/0123abcdef/x.txt"},
		{"single segment", "x"},
		{"non-ASCII", "docs/résumé/日本語.txt"},
		{"dot inside a segment", "a/b.c/d"},
		{"leading dot", ".hidden"},
		{"space", "a b/c d"},
		{"backslash inside", `a\b`},
		{"longest key in runes", longest},
		{"most segments", mostSegments},
	}
	for _, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			if err := validateKey(tc.key); err != nil {
				t.Errorf("validateKey(%q) = %v, want nil", tc.key, err)
			}
		})
	}

	invalid := []struct{ name, key, want string }{
		{"empty", "", "empty key"},
		{"one rune too long", longest + "e", "1025 characters"},
		{"one segment too many", mostSegments + "/a", "255 path segments"},
		{"NUL", "a\x00b", "control character U+0000"},
		{"newline", "a\nb", "control character U+000A"},
		{"unit separator U+001F", "a\x1fb", "control character U+001F"},
		{"DEL U+007F", "a\x7fb", "control character U+007F"},
		{"C1 control U+0080", "a\u0080b", "control character U+0080"},
		{"C1 control U+009F", "a\u009fb", "control character U+009F"},
		{"trailing dot", "a/b.", `ends in '.'`},
		{"trailing slash", "a/b/", `ends in '/'`},
		{"trailing backslash", `a\b\`, `ends in '\\'`},
		{"segment ending in a dot", "a./b", `segment "a." ends in a dot`},
	}
	for _, tc := range invalid {
		t.Run("invalid/"+tc.name, func(t *testing.T) {
			err := validateKey(tc.key)
			if err == nil {
				t.Fatalf("validateKey(%q) = nil, want an error containing %q", tc.key, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("validateKey(%q) = %q, want it to contain %q", tc.key, err, tc.want)
			}
		})
	}
}

func TestValidateContainer(t *testing.T) {
	for _, name := range []string{"abc", "unit", "acceptance-0123abcd", "a1-b2-c3", strings.Repeat("a", 63)} {
		if err := validateContainer(name); err != nil {
			t.Errorf("validateContainer(%q) = %v, want nil", name, err)
		}
	}

	invalid := []struct{ name, container, want string }{
		{"too short", "ab", "2 characters"},
		{"too long", strings.Repeat("a", 64), "64 characters"},
		{"upper case", "Assets", `has 'A'`},
		{"underscore", "my_files", `has '_'`},
		{"dot", "my.files", `has '.'`},
		{"leading hyphen", "-files", "hyphen"},
		{"trailing hyphen", "files-", "hyphen"},
		{"double hyphen", "my--files", "hyphen"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			err := validateContainer(tc.container)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("validateContainer(%q) = %v, want an error containing %q", tc.container, err, tc.want)
			}
		})
	}
}
