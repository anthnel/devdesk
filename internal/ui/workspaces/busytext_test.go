package workspaces

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-144 step 3: Scanned and Git Status say what is running, in plain
// dim text with no spinner frame. Only a running item holds the cell (Q3): a
// queued one keeps its normal value.

const (
	bzClients = "/tmp/workspaces/clients"
	bzSubA    = "/tmp/workspaces/clients/a"
)

// spinnerRunes are braille frames and the test frames the helpers inject.
const spinnerRunes = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏*"

func cellOf(t *testing.T, title string, r workspaceRow) string {
	t.Helper()
	for _, c := range workspaceColumns(false, false) {
		if c.Title == title {
			return c.Cell(r)
		}
	}
	t.Fatalf("no column %q", title)
	return ""
}

func dimOf(t *testing.T, title string, r workspaceRow) bool {
	t.Helper()
	for _, c := range workspaceColumns(false, false) {
		if c.Title != title {
			continue
		}
		if c.Style == nil {
			return false
		}
		return c.Style(r).GetForeground() == theme.DimStyle.GetForeground()
	}
	t.Fatalf("no column %q", title)
	return false
}

func TestRunningOperationShowsFixedTextInTheRightColumn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   jobs.Kind
		column string
		want   string
	}{
		{"scan", jobs.KindScan, "Scanned", "scanning"},
		{"sync", jobs.KindSync, "Git Status", "syncing"},
		{"delete", jobs.KindDelete, "Git Status", "deleting"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := running(t, loadedModel(t), tc.kind, devdeskPath)
			r := rowNamed(t, m, "devdesk")
			if got := cellOf(t, tc.column, r); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.column, got, tc.want)
			}
			if !dimOf(t, tc.column, r) {
				t.Errorf("%s style is not DimStyle while %s", tc.column, tc.want)
			}
			if !strings.Contains(lineOf(t, m, "devdesk"), tc.want) {
				t.Errorf("rendered row lacks %q", tc.want)
			}
		})
	}
}

func TestBusyTextIsWholeCellDimInGitStatus(t *testing.T) {
	for _, kind := range []jobs.Kind{jobs.KindSync, jobs.KindDelete} {
		m := running(t, loadedModel(t), kind, devdeskPath)
		r := rowNamed(t, m, "devdesk")
		if r.branchCut != len(r.GitStatus) {
			t.Errorf("%s: Cut = %d, want %d (whole cell)", kind, r.branchCut, len(r.GitStatus))
		}
	}
}

func TestAQueuedOperationDoesNotHoldTheCells(t *testing.T) {
	base := loadedModel(t)
	for _, kind := range []jobs.Kind{jobs.KindScan, jobs.KindSync, jobs.KindDelete} {
		m := withSnapshot(t, base, []jobs.Run{actRun(kind, testContext, jobs.ItemQueued, devdeskPath)}, nil)
		before, after := rowNamed(t, base, "devdesk"), rowNamed(t, m, "devdesk")
		for _, col := range []string{"Scanned", "Git Status"} {
			if got, want := cellOf(t, col, after), cellOf(t, col, before); got != want {
				t.Errorf("queued %s: %s = %q, want its normal value %q", kind, col, got, want)
			}
		}
	}
}

func TestADirectoryAggregatesItsSubRepoActivity(t *testing.T) {
	m := running(t, loadedModel(t), jobs.KindScan, bzSubA)
	r := rowNamed(t, m, "clients")
	if got := cellOf(t, "Scanned", r); got != "scanning" {
		t.Errorf("directory Scanned = %q, want %q", got, "scanning")
	}
	if !dimOf(t, "Scanned", r) {
		t.Error("directory Scanned is not dim while scanning")
	}
	// A queued sub-repo does not hold the directory.
	q := withSnapshot(t, loadedModel(t), []jobs.Run{actRun(jobs.KindScan, testContext, jobs.ItemQueued, bzSubA)}, nil)
	if got := cellOf(t, "Scanned", rowNamed(t, q, "clients")); got == "scanning" {
		t.Error("directory Scanned says scanning for a merely queued sub-repo")
	}
}

func TestNoCellEverCarriesASpinnerRune(t *testing.T) {
	m := running(t, loadedModel(t), jobs.KindScan, devdeskPath, bzSubA)
	m = running(t, m, jobs.KindSync, devdeskPath)
	m = feed(t, m, jobs.FrameMsg{})
	for _, r := range m.table.Items() {
		for _, c := range workspaceColumns(true, true) {
			if c.Indicator != nil {
				continue
			}
			if got := c.Cell(r); strings.ContainsAny(got, spinnerRunes) {
				t.Errorf("%s / %s = %q carries a spinner frame", r.Entry.Name, c.Title, got)
			}
		}
	}
}

// Rule 122: the colour is a Style, never part of what Cell returns.
func TestBusyTextColourComesFromStyleNotCell(t *testing.T) {
	m := running(t, loadedModel(t), jobs.KindScan, devdeskPath)
	m = running(t, m, jobs.KindSync, devdeskPath)
	r := rowNamed(t, m, "devdesk")
	for _, col := range []string{"Scanned", "Git Status"} {
		if got := cellOf(t, col, r); strings.Contains(got, "\x1b") {
			t.Errorf("%s Cell = %q carries an escape sequence", col, got)
		}
	}
	if !dimOf(t, "Scanned", r) {
		t.Error("Scanned has no dim Style while scanning")
	}
}
