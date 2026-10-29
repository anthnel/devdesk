//go:build unix

package agentusage

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO at a transcript path is refused, never opened: opening it would
// block until a writer shows up.
func TestReadFileNeverOpensAFifo(t *testing.T) {
	isolateReadFileHome(t)
	path := filepath.Join(t.TempDir(), "p.jsonl")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	p := countOpens(t)
	done := make(chan fileRead, 1)
	go func() { done <- pass(cur(path)) }()
	select {
	case fr := <-done:
		if fr.reset || !fr.next.hasIssue || fr.next.issue != IssueOpenFailed {
			t.Errorf("reset=%v issue=%v (has %v), want a state IssueOpenFailed", fr.reset, fr.next.issue, fr.next.hasIssue)
		}
		if p.opens != 0 || p.calls != 0 {
			t.Errorf("opens=%d calls=%d, want 0 and 0: not even an attempt", p.opens, p.calls)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readFile blocked on a FIFO")
	}
}
