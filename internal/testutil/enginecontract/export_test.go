package enginecontract

import (
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// VerifyAgainstRequiring is VerifyAgainst with RequireFixtures forced, so the tests
// can hold the strict behavior while the constant is still false.
func (s *Session) VerifyAgainstRequiring(t testing.TB, fixtures []enginefixture.Fixture, scenario string,
	ex enginefixture.Exchange, r enginefixture.Redaction, cmp Comparer) {
	t.Helper()
	s.verifyAgainst(t, fixtures, scenario, ex, r, cmp, true)
}
