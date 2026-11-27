package enginefixture_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// OC-08b, review: pendingIn read the headers with a bufio.Scanner, which stops at
// 64 KiB per line and whose error was ignored: a pending file with a long header
// line was taken for a complete one. The header that says "pending" comes after the
// long line here, so a scanner that gives up misses it.
func TestPendingIsFoundAfterAVeryLongHeaderLine(t *testing.T) {
	long := "# scenario: " + strings.Repeat("x", 200*1024) + "\n"
	root := writeTree(t, map[string][]byte{
		"fixtures/lifecycle/long--docker-29.10.1-rootful.cli": []byte(long + "# record: pending\n\n"),
		"fixtures/lifecycle/done--docker-29.10.1-rootful.cli": []byte(long + "# record: replay\n\n"),
	})
	found := pendingIn(t, filepath.Join(root, "fixtures", "*", "*.cli"))
	want := filepath.Join(root, "fixtures", "lifecycle", "long--docker-29.10.1-rootful.cli")
	if len(found) != 1 || found[0] != want {
		t.Errorf("pendingIn = %v, want only %s", found, want)
	}
}
