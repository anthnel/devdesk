package oci

import (
	"errors"
	"strings"
	"testing"
)

// DEVDESK-412 step 8 (group D): the refusal of a layer digest that is not sha256
// says so in plain words. The registry's digest is not repeated.
func TestLayerDigestRefusalSaysOnlySha256IsSupported(t *testing.T) {
	const want = "unsupported layer digest: only sha256 is supported"

	for name, digest := range map[string]string{
		"sha512":        "sha512:" + strings.Repeat("ab", 64),
		"no algorithm":  strings.Repeat("a", 64),
		"empty":         "",
		"short sha256":  "sha256:abc",
		"upper case":    "sha256:" + strings.Repeat("A", 64),
		"not hex":       "sha256:" + strings.Repeat("g", 64),
		"path in value": "sha256:../x",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseLayerDigest(digest)
			if !errors.Is(err, errBlobDigestForm) {
				t.Fatalf("err = %v, want errBlobDigestForm", err)
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q lacks %q", err, want)
			}
			if len(digest) > 7 && strings.Contains(err.Error(), digest) {
				t.Errorf("error %q repeats the digest", err)
			}
		})
	}
}
