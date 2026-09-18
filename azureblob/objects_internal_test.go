package azureblob

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

func TestEntityTag(t *testing.T) {
	cases := []struct {
		name string
		in   *azcore.ETag
		want string
	}{
		{"nil", nil, ""},
		{"empty", etag(""), ""},
		{"unquoted listing value", etag("0x8DDF0E1C2B3A4D5"), `"0x8DDF0E1C2B3A4D5"`},
		{"quoted header value", etag(`"0x8DDF0E1C2B3A4D5"`), `"0x8DDF0E1C2B3A4D5"`},
		{"weak validator", etag(`W/"0x8DDF0E1C2B3A4D5"`), `W/"0x8DDF0E1C2B3A4D5"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := entityTag(tc.in)
			if got != tc.want {
				t.Errorf("entityTag(%v) = %q, want %q", tc.in, got, tc.want)
			}
			// The form is a fixed point: normalizing an already normalized
			// value changes nothing.
			if again := entityTag(etag(got)); again != got {
				t.Errorf("entityTag(%q) = %q, want it unchanged", got, again)
			}
		})
	}
}

func etag(s string) *azcore.ETag {
	e := azcore.ETag(s)
	return &e
}
