package app

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/about"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-791: `W` reaches the About screen through the router, which owns
// no uppercase key of its own. The command it gets back is not run here: it
// would start the host's browser. The opener seam and the outcome messages are
// covered in internal/ui/about.
func TestTheRouterHandsTheOnlineHelpKeyToTheAboutScreen(t *testing.T) {
	a := router(t, about.New(testConfig()))

	if cmd := feedKey(t, a, testutil.Key(keymap.Web)); cmd == nil {
		t.Fatalf("%q on the About screen produced no command: the router swallowed it or the screen ignored it", keymap.Web)
	}
}

// The keys the screen did not use before still do nothing there, through the
// router as well.
func TestTheRouterStillGivesNothingToTheOtherAboutKeys(t *testing.T) {
	for _, key := range []string{"w", "O", "enter", "down"} {
		a := router(t, about.New(testConfig()))

		if cmd := feedKey(t, a, testutil.Key(key)); cmd != nil {
			t.Errorf("%q on the About screen produced a command", key)
		}
	}
}
