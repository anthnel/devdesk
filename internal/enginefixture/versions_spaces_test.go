package enginefixture_test

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-08b, review: versions.env is sourced by bash, where "KEY = v" is a command and
// "KEY= v" is an empty KEY followed by a command. ParseVersions refuses them, and
// names the key, instead of reading a value bash would not.
func TestParseVersionsRefusesABlankAroundTheEqualSign(t *testing.T) {
	for _, key := range []string{"DOCKER_N_VERSION", "DOCKER_N1_VERSION", "PODMAN_N_VERSION", "PODMAN_N1_VERSION"} {
		for _, bad := range []string{key + " = 1.2.3", key + " =1.2.3", key + "= 1.2.3", key + "=\t1.2.3"} {
			text := without(goodVersions, key+"=") + "\n" + bad + "\n"
			_, err := enginefixture.ParseVersions([]byte(text))
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("%q: err = %v, want one that names %s", bad, err, key)
			}
		}
	}
}

func TestParseVersionsStillReadsALineWithACommentAfterBlanks(t *testing.T) {
	text := without(goodVersions, "DOCKER_N_VERSION=") + "\n  DOCKER_N_VERSION=29.10.1    # released\n"
	got, err := enginefixture.ParseVersions([]byte(text))
	if err != nil || got[enginefixture.Docker][0] != "29.10.1" {
		t.Errorf("ParseVersions = %v, %v, want Docker N = 29.10.1 (a blank before the key and before the comment is fine)", got, err)
	}
}
