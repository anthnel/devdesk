package security

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
)

// DEVDESK-751: findingFilePath reads the path, not the disk (it feeds the
// header, Rule 130). The check that follows links is made in the Cmd of enter.

// linkedTree is a scanned directory holding links, and a file outside it.
func linkedTree(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	outside := t.TempDir()
	write := func(path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, name string) {
		t.Helper()
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("symbolic links are not available: %v", err)
		}
	}
	write(filepath.Join(outside, "secret.env"))
	write(filepath.Join(root, "real", "inside.env"))
	link(outside, "linked-dir")
	link(filepath.Join(outside, "secret.env"), "linked-file.env")
	link(filepath.Join(root, "real"), "alias-inside")
	link(filepath.Join(root, "real", "missing"), "dangling")
	return root
}

func enterOn(t *testing.T, root, file string) (Model, []any) {
	t.Helper()
	testutil.FastTimers(t, &components.FooterMsgDuration)
	m := openDetailOn(t, gitleaksSecret(file, 1), root, scan.TargetDirectory, 160, 30)
	m, cmd := step(t, m, testutil.Key("enter"))
	var msgs []any
	for _, msg := range testutil.Msgs(cmd) {
		msgs = append(msgs, msg)
		m = feed(t, m, msg)
	}
	return m, msgs
}

func TestEnterRefusesAFileReachedThroughALinkThatLeavesTheTarget(t *testing.T) {
	root := linkedTree(t)
	for _, file := range []string{"linked-dir/secret.env", "linked-file.env"} {
		t.Run(file, func(t *testing.T) {
			m, msgs := enterOn(t, root, file)
			for _, msg := range msgs {
				if _, ok := msg.(uiviewer.OpenRequestMsg); ok {
					t.Fatalf("enter asked the viewer to open %s, which is outside the scanned directory", file)
				}
			}
			if m.footer.Text() != reasonFileLinkOutsideTarget || m.footer.Level() != components.LevelWarning {
				t.Errorf("footer = %q (level %v), want %q as a warning", m.footer.Text(), m.footer.Level(), reasonFileLinkOutsideTarget)
			}
		})
	}
}

func TestEnterStillOpensWhatALinkKeepsInsideTheTarget(t *testing.T) {
	root := linkedTree(t)
	for _, file := range []string{"real/inside.env", "alias-inside/inside.env", "no/such/file.env", "dangling"} {
		t.Run(file, func(t *testing.T) {
			_, msgs := enterOn(t, root, file)
			var asked bool
			for _, msg := range msgs {
				_, ok := msg.(uiviewer.OpenRequestMsg)
				asked = asked || ok
			}
			if !asked {
				t.Errorf("enter on %s produced %v, want a request to the viewer", file, msgs)
			}
		})
	}
}

// The header says nothing about links: it does no I/O.
func TestTheHeaderDoesNotFollowLinks(t *testing.T) {
	root := linkedTree(t)
	m := openDetailOn(t, gitleaksSecret("linked-dir/secret.env", 1), root, scan.TargetDirectory, 160, 30)
	if a := m.canOpenFindingFile(); !a.Enabled() {
		t.Errorf("canOpenFindingFile() = %+v, want it enabled: the link is for the key press to find", a)
	}
}
