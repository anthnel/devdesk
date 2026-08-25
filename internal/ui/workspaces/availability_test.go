package workspaces

import (
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── The scanners ─────────────────────────────────────────────────────────────

// Not knowing is not knowing that not. The check runs in a Cmd, so the first
// frames are drawn before the answer lands, and greying S out to un-grey it
// three frames later reads as a fault.
func TestTheScanKeysAreOfferedBeforeTheCheckComesBack(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(0) // devdesk, a git repo

	if m.deps != nil {
		t.Fatal("the fixture already carries a dependency answer; this test needs the unknown state")
	}
	for _, key := range []string{keymap.Scan, keymap.ScanAll} {
		if shortcutDisabled(m.GetShortcuts(), key) {
			t.Errorf("%s is greyed before anyone has looked for a scanner", key)
		}
	}
}

func TestTheScanKeysFollowWhatIsInstalled(t *testing.T) {
	for _, tc := range []struct {
		name     string
		deps     scan.DependencyStatus
		disabled bool
	}{
		{"neither scanner", scan.DependencyStatus{}, true},
		{"Trivy alone", scan.DependencyStatus{TrivyAvailable: true}, false},
		{"Gitleaks alone", scan.DependencyStatus{GitleaksAvailable: true}, false},
		{"both", scan.DependencyStatus{TrivyAvailable: true, GitleaksAvailable: true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := feed(t, loadedModel(t), DepsCheckedMsg{Deps: tc.deps})
			m.table.SetCursor(0) // devdesk, a git repo

			for _, key := range []string{keymap.Scan, keymap.ScanAll} {
				if got := shortcutDisabled(m.GetShortcuts(), key); got != tc.disabled {
					t.Errorf("%s disabled = %v with %s, want %v", key, got, tc.name, tc.disabled)
				}
			}
		})
	}
}

// One scanner is enough, but none means the scan would run and find nothing —
// which is what happened before: S was offered and produced an empty report.
func TestScanningWithoutAScannerIsRefusedAndSaysSo(t *testing.T) {
	m := feed(t, loadedModel(t), DepsCheckedMsg{Deps: scan.DependencyStatus{}})
	m.table.SetCursor(0) // devdesk, a git repo

	next := refused(t, m, keymap.Scan, reasonNoScanner)

	if len(next.scanningPaths) != 0 {
		t.Errorf("a scan was started with no scanner installed: %v", next.scanningPaths)
	}
}

// A carries the same guard, and its confirmation must not even open: asking a
// question whose answer will be declined wastes the user's time (§3.23).
func TestScanAllWithoutAScannerNeverOpensItsModal(t *testing.T) {
	m := feed(t, loadedModel(t), DepsCheckedMsg{Deps: scan.DependencyStatus{}})

	next := refused(t, m, keymap.ScanAll, reasonNoScanner)

	if next.scanAllModal != nil {
		t.Error("A opened its confirmation with no scanner installed")
	}
}

// ── One calculation, two readers ─────────────────────────────────────────────

// The invariant the whole file exists for: a key the header greys out does not
// act. Nothing enforces it structurally — actions() is simply the only source
// both halves read — so it is checked over every key and every fixture row.
func TestNoGreyedKeyEverActs(t *testing.T) {
	keys := []string{"enter", keymap.Web, keymap.Scan, keymap.Fetch, keymap.ScanAll, keymap.Copy}
	greyed := 0

	for cursor := -1; cursor < len(entryFixtures()); cursor++ {
		m := feed(t, scannedModel(t), DepsCheckedMsg{Deps: scan.DependencyStatus{}})
		if cursor < 0 {
			m = feed(t, newTestModel(t), DepsCheckedMsg{Deps: scan.DependencyStatus{}})
		} else {
			m.table.SetCursor(cursor)
		}

		for _, key := range keys {
			if !shortcutDisabled(m.GetShortcuts(), key) {
				continue
			}
			greyed++

			before := m
			next, _ := step(t, m, testutil.Key(key))

			switch {
			case next.mode != before.mode:
				t.Errorf("row %d: greyed %s changed the mode to %v", cursor, key, next.mode)
			case next.scanAllModal != nil:
				t.Errorf("row %d: greyed %s opened the scan-all modal", cursor, key)
			case len(next.scanningPaths) != 0:
				t.Errorf("row %d: greyed %s started a scan", cursor, key)
			case next.sync != nil:
				t.Errorf("row %d: greyed %s started a sync", cursor, key)
			}
		}
	}

	// Without this the loop would pass by testing nothing the day actions()
	// stops greying anything out.
	if greyed < len(keys) {
		t.Errorf("only %d greyed keys were exercised across every fixture row, want at least %d", greyed, len(keys))
	}
}
