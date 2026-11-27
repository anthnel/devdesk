package enginecontract

import (
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// VerifyAgainstRequiring is VerifyAgainst with RequireFixtures forced, so the tests
// can hold the strict behavior whatever the constant is.
func (s *Session) VerifyAgainstRequiring(t testing.TB, fixtures []enginefixture.Fixture, scenario string,
	ex enginefixture.Exchange, r enginefixture.Redaction, cmp Comparer) {
	t.Helper()
	s.verifyAgainst(t, fixtures, scenario, ex, r, cmp, true)
}

// VerifyAgainstNotRequiring is VerifyAgainst with RequireFixtures off: what Verify did before the
// first fixtures were versioned (OC-09b2 made the constant true), kept reachable so the tests hold
// that branch too.
func (s *Session) VerifyAgainstNotRequiring(t testing.TB, fixtures []enginefixture.Fixture, scenario string,
	ex enginefixture.Exchange, r enginefixture.Redaction, cmp Comparer) {
	t.Helper()
	s.verifyAgainst(t, fixtures, scenario, ex, r, cmp, false)
}
