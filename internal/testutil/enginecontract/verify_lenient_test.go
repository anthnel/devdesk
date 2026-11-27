package enginecontract_test

import (
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// verifyNotRequiring is verify with RequireFixtures off: "no fixture at all" is a log line, as it was
// before OC-09b2 versioned the first fixtures and made RequireFixtures true.
func verifyNotRequiring(t *testing.T, fixtures []enginefixture.Fixture, ex enginefixture.Exchange, r enginefixture.Redaction, cmp enginecontract.Comparer) *recT {
	t.Helper()
	rt := &recT{TB: t}
	rt.run(func() { verifySession().VerifyAgainstNotRequiring(rt, fixtures, "inspect-absent", ex, r, cmp) })
	return rt
}
