package theme

import (
	"path/filepath"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestFoldHome(t *testing.T) {
	home := "/home/dev"
	t.Setenv("HOME", home)

	cases := map[string]string{
		home:                        "~",
		home + "/projects/x":        "~/projects/x",
		"/usr/local/bin/trivy":      "/usr/local/bin/trivy",
		"/home/developer/elsewhere": "/home/developer/elsewhere",
		"":                          "",
	}
	for in, want := range cases {
		if got := FoldHome(in); got != want {
			t.Errorf("FoldHome(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShortPathGivesWayInOrder(t *testing.T) {
	if filepath.Separator != '/' {
		t.Skip("cases are written with /")
	}
	t.Setenv("HOME", "/home/dev")
	long := "/home/dev/projects/workspace-entire/anthnel/devdesk"

	cases := []struct {
		name  string
		path  string
		width int
		want  string
	}{
		{"fits once folded", long, 60, "~/projects/workspace-entire/anthnel/devdesk"},
		{"first segment reduced", long, 38, "~/p/workspace-entire/anthnel/devdesk"},
		{"stops as soon as it fits", long, 26, "~/p/w/anthnel/devdesk"},
		{"every intermediate reduced", long, 15, "~/p/w/a/devdesk"},
		{"head truncated last", long, 12, "...a/devdesk"},
		{"hidden segment keeps its dot", "/home/dev/.config/devdesk/config.yaml", 25, "~/.c/devdesk/config.yaml"},
		{"outside home", "/usr/local/lib/trivy/bin/trivy", 18, "/u/l/l/t/bin/trivy"},
		{"home itself", "/home/dev", 1, "~"},
		{"no budget means no shortening", long, 0, "~/projects/workspace-entire/anthnel/devdesk"},
		{"multibyte segments", "/home/dev/données/été/rapport.md", 16, "~/d/é/rapport.md"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ShortPath(c.path, c.width)
			if got != c.want {
				t.Errorf("ShortPath(%q, %d) = %q, want %q", c.path, c.width, got, c.want)
			}
			if c.width > 0 && runewidth.StringWidth(got) > c.width && got != "~" {
				t.Errorf("%q is wider than %d", got, c.width)
			}
		})
	}
}
