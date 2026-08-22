package containers

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// cell builds a container out of a raw `docker ps` ports string and renders it,
// which is the whole path the column takes.
func portsCellFor(raw string) string {
	return portsCell(docker.Container{Ports: docker.ParseContainerPorts(raw)})
}

func TestThePortsCellShowsTheHostPortAndWhoCanReachIt(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			// The case the whole column was rewritten for: thirty-three
			// characters of docker output for one published port.
			name: "a dual-stack publication is one short entry",
			raw:  "0.0.0.0:80->80/tcp, :::80->80/tcp",
			want: theme.IconNetwork + " 80",
		},
		{
			name: "loopback says so",
			raw:  "127.0.0.1:5432->5432/tcp",
			want: theme.IconHome + " 5432",
		},
		{
			name: "a named address is its own scope",
			raw:  "192.168.1.5:8080->80/tcp",
			want: theme.IconServer + " 8080",
		},
		{
			name: "exposed but unpublished must not read as connectable",
			raw:  "6379/tcp",
			want: theme.IconLock + " 6379",
		},
		{
			// tcp is the massively dominant case, so naming it on every entry
			// would inform no one.
			name: "only a non-tcp protocol is named",
			raw:  "0.0.0.0:53->53/udp, 0.0.0.0:80->80/tcp",
			want: theme.IconNetwork + " 53/udp  " + theme.IconNetwork + " 80",
		},
		{
			// The loss taken deliberately: the container side is gone, so these
			// two read alike. `enter` still has it.
			name: "the container port is not shown",
			raw:  "0.0.0.0:8080->80/tcp",
			want: theme.IconNetwork + " 8080",
		},
		{
			name: "no ports at all",
			raw:  "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := portsCellFor(tc.raw); got != tc.want {
				t.Errorf("cell for %q = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// The point of the rewrite, measured rather than asserted in prose.
func TestThePortsCellFitsTheColumnItWasGiven(t *testing.T) {
	const raw = "0.0.0.0:80->80/tcp, :::80->80/tcp"

	got := portsCellFor(raw)
	if len(got) >= len(raw) {
		t.Errorf("cell = %q, no shorter than the raw docker string", got)
	}
	// MinWidth on the column. An icon counts as one cell, so the rune count is
	// the measure, not the byte count.
	if n := len([]rune(got)); n > 16 {
		t.Errorf("cell = %q is %d cells wide, over the column's MinWidth of 16", got, n)
	}
}

// Rule 122: the cell is measured before it is drawn, so it must carry no escape
// sequence. This column is the one that gained icons, which is exactly where
// the temptation to colour them lives.
func TestThePortsCellIsPlainText(t *testing.T) {
	for _, c := range containerFixtures() {
		if got := portsCell(c); strings.Contains(got, "\x1b") {
			t.Errorf("%s ports cell = %q carries an escape sequence", c.Name, got)
		}
	}
}

// The parse is what makes the column searchable, which it was not while the
// cell was one opaque string.
func TestThePortsColumnIsSearchableByPortNumber(t *testing.T) {
	m := feed(t, rawModel(t), testutil.Key("/"))
	m = feed(t, m, testutil.Type("6379")...)

	got := names(m)
	if len(got) != 1 || got[0] != "cache" {
		t.Errorf("searching a port number showed %v, want the container publishing it", got)
	}
}
