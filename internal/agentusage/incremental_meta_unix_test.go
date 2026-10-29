//go:build unix

package agentusage

import (
	"os"
	"testing"
)

// DECLARED EXCEPTION to the oracle of D3 (it comes from D7): a meta file that
// exists but cannot be read (chmod 000) is MetaUnreadable for the incremental
// reader, because ReadDelta sees it through Lstat and then fails to read it,
// whereas ParseSession(LocateSession(...)) never learns about the file and
// counts it as MetaMissing. The two readings are pinned here so a change to
// either is deliberate.
func TestUnreadableMetaIsADeclaredOracleException(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a chmod 000 file")
	}
	s := newSess(t)
	s.put(s.parent(), ln("p1"))
	s.put(s.sub("b1"), ln("s1"))
	s.putMeta("b1", metaUnfinished, 0)
	if err := os.Chmod(s.metaPath("b1"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.metaPath("b1"), 0o600) })

	a := NewAggregate(s.parent())
	readApply(t, a)
	got := a.Usage().Diagnostics
	if got.MetaUnreadable != 1 || got.MetaMissing != 0 {
		t.Errorf("ReadDelta: MetaUnreadable=%d MetaMissing=%d, want 1 and 0", got.MetaUnreadable, got.MetaMissing)
	}

	want, err := ParseSession(s.located())
	if err != nil {
		t.Fatalf("ParseSession: %v", err)
	}
	if want.Diagnostics.MetaMissing != 1 || want.Diagnostics.MetaUnreadable != 0 {
		t.Errorf("ParseSession: MetaMissing=%d MetaUnreadable=%d, want 1 and 0",
			want.Diagnostics.MetaMissing, want.Diagnostics.MetaUnreadable)
	}
}
