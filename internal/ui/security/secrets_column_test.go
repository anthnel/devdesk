package security

import (
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// secretsFound and secretsClean write the two known verdicts; the third is nil.
func secretsFound() *bool { v := true; return &v }
func secretsClean() *bool { v := false; return &v }

// verdictFixtures covers the four rows the column has to tell apart — the two
// families, and the three verdicts.
func verdictFixtures() []scanTarget {
	at := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	return []scanTarget{
		{Kind: kindImage, Name: "nexus/api:1.4", Scanned: true, Sensitive: secretsFound(),
			Counts: scan.SeverityCounts{Critical: 3}, ScannedAt: at},
		{Kind: kindImage, Name: "nexus/web:2.0", Scanned: true, Sensitive: secretsClean(), ScannedAt: at},
		{Kind: kindRepo, Name: "/home/dev/ws/devdesk", Scanned: true, ScannedAt: at},
		{Kind: kindRepo, Name: "/home/dev/ws/old", Scanned: true, Sensitive: secretsFound(), ScannedAt: at},
	}
}

func TestTheInventoryTellsTheThreeVerdictsApart(t *testing.T) {
	m := inventoryModel(t, verdictFixtures()...)

	cols := m.inventory.Table().Columns()
	target := columnIndex(t, cols, "Target")
	secrets := columnIndex(t, cols, "Secrets")

	byTarget := map[string]string{}
	for _, row := range m.inventory.Table().Rows() {
		byTarget[row[target]] = row[secrets]
	}

	tests := []struct {
		contains string
		want     theme.SecretsState
		why      string
	}{
		{"nexus/api:1.4", theme.SecretsFound, "a secret was found"},
		{"nexus/web:2.0", theme.SecretsClean, "a stage looked and found nothing"},
		{"devdesk", theme.SecretsUnknown, "no stage ever looked"},
	}

	for _, tt := range tests {
		cell, ok := cellFor(byTarget, tt.contains)
		if !ok {
			t.Errorf("no row for %q among %v", tt.contains, byTarget)
			continue
		}
		if want := theme.SecretsIcon(tt.want); cell != want {
			t.Errorf("%s: Secrets cell = %q, want %q — %s", tt.contains, cell, want, tt.why)
		}
	}
}

// cellFor resolves a row by a fragment of its Target cell, which carries a
// type icon and a path folded to "~".
func cellFor(rows map[string]string, fragment string) (string, bool) {
	for target, cell := range rows {
		if strings.Contains(target, fragment) {
			return cell, true
		}
	}
	return "", false
}

// A row purged by A (with the "purge" checkbox checked) no longer has
// counters — it shows `-` — and it has no verdict either: the one it carried
// described a scan the purge has just erased.
func TestAPurgedRowHasNoVerdictEither(t *testing.T) {
	m := inventoryModel(t, verdictFixtures()...)

	m, _ = scanAll(t, m, true)

	secrets := columnIndex(t, m.inventory.Table().Columns(), "Secrets")
	unknown := theme.SecretsIcon(theme.SecretsUnknown)
	for _, row := range m.inventory.Table().Rows() {
		if row[secrets] != unknown {
			t.Errorf("Secrets cell = %q while purged, want %q", row[secrets], unknown)
		}
	}
}

// The verdict travels with the rescan. Without it on the message, the row
// would show the icon of its previous scan beside brand-new counters, until
// the next ctrl+r.
func TestAFinishedRescanBringsBackItsVerdict(t *testing.T) {
	m := inventoryModel(t, verdictFixtures()...)
	m, _ = scanAll(t, m, true)

	m = feed(t, m, InventoryScanFinishedMsg{
		Name: "nexus/api:1.4", Counts: scan.SeverityCounts{Critical: 1},
		Sensitive: secretsClean(), ScannedAt: time.Now(),
	})

	for _, target := range m.inventory.Items() {
		if target.Name != "nexus/api:1.4" {
			continue
		}
		if target.secrets() != theme.SecretsClean {
			t.Errorf("the rescanned row reads %v, want the verdict the scan just returned", target.secrets())
		}
	}
}
