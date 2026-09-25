package sigcol

import (
	"testing"

	"github.com/anthnel/devdesk/internal/trust"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

var blocked = trust.Result{Verdict: trust.IdentityMismatch, Decision: trust.Block}

func TestEachStateHasItsOwnCell(t *testing.T) {
	for name, c := range map[string]struct {
		state State
		want  string
	}{
		"verifying":   {State{Verifying: true}, theme.IconHourglass},
		"local build": {State{LocalBuild: true}, theme.IconHammer},
		"not asked":   {State{}, "-"},
		"no policy":   {State{Known: true}, "-"},
		"verified":    {State{Known: true, Result: trust.Result{Verdict: trust.Verified}}, theme.IconOK},
		"blocked":     {State{Known: true, Result: blocked}, theme.IconError},
		"unsigned":    {State{Known: true, Result: trust.Result{Verdict: trust.Unsigned, Decision: trust.Warn}}, theme.IconWarning},
		"failed":      {State{Known: true, Result: trust.Result{Verdict: trust.Failed, Decision: trust.Warn}}, theme.IconHelpCircle},
	} {
		if got := Cell(c.state); got != c.want {
			t.Errorf("%s: cell %q, want %q", name, got, c.want)
		}
	}
}

func TestTheColumnIsOptionalAndUnsorted(t *testing.T) {
	col := Column(func(s State) State { return s })
	if !col.Optional || col.Less != nil || col.Search != nil {
		t.Errorf("column = %+v", col)
	}
}
