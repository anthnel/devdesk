package app

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/ui/help"
)

// The router probes each view for optional interfaces and silently falls back
// when one is missing. That is the right behaviour and a bad failure mode: the
// configuration view implemented GetShortcuts and GetTitle but not GetIcon or
// GetHeaderInfo, so it satisfied none of HeaderView and the viewport rendered
// an empty title — with nothing to say so.
//
// Checking every view the command parser can name turns that into a build-time
// contract rather than something noticed by looking at the screen.
func TestEveryViewSuppliesItsHeaderAndHelp(t *testing.T) {
	for _, name := range command.ViewNames() {
		view := command.ViewType(name)

		t.Run(name, func(t *testing.T) {
			app := newWithSize(testConfig(), 120, 40)
			app.createView(view)

			built, ok := app.views[view]
			if !ok {
				t.Fatalf("createView(%q) built nothing; the switch has no case for it", view)
			}

			hv, ok := built.(HeaderView)
			if !ok {
				t.Fatalf("%q does not implement HeaderView, so the router renders an empty title for it", view)
			}
			if strings.TrimSpace(hv.GetTitle()) == "" {
				t.Errorf("%q has an empty title", view)
			}
			if len(hv.GetShortcuts()) == 0 {
				t.Errorf("%q advertises no shortcuts", view)
			}

			if _, ok := built.(help.Provider); !ok {
				t.Errorf("%q does not implement help.Provider, so ? shows nothing (Rule 114)", view)
			}
		})
	}
}
