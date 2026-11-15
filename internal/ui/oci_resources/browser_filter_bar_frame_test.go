package ociresources

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-457: the tags-level filter bar is drawn by components.BarFrame, not
// by a copy of it, so its corners cannot drift from the router's junction. The
// stripped output is the frame around exactly one padded line.
func TestTheFilterBarIsABarFrame(t *testing.T) {
	const width = 300
	frame := func(inner string) string {
		pad := width - 2 - ansi.StringWidth(inner)
		return "│" + inner + strings.Repeat(" ", pad) + "│\n└" + strings.Repeat("─", width-2) + "┘"
	}

	m := resultsModel(t)
	b := m.registryBrowser

	m = feed(t, m, testutil.Key("r"))
	if got, want := ansi.Strip(b.FilterBarView(width)), frame(" / filter…  [prod]"); got != want {
		t.Errorf("registry filter:\n got %q\nwant %q", got, want)
	}

	m = feed(t, m, testutil.Key("/"))
	if got, want := ansi.Strip(b.FilterBarView(width)), frame(" / "+ansi.Strip(b.filterInput.View())+"  [prod]"); got != want {
		t.Errorf("active:\n got %q\nwant %q", got, want)
	}

	m = typeInto(t, m, "v1")
	feed(t, m, testutil.Key("enter"))
	if got, want := ansi.Strip(b.FilterBarView(width)), frame(" / v1  [prod]"); got != want {
		t.Errorf("query:\n got %q\nwant %q", got, want)
	}
}
