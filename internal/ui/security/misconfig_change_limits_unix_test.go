//go:build unix

package security

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Opening a FIFO blocks until a writer comes: the file is refused on its type
// before it is opened, and the preview never hangs.
func TestAFifoWhereTheFileShouldBeIsDeclinedWithoutBlocking(t *testing.T) {
	fx := fixtureFor(t, "AVD-DS-0021")
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "Dockerfile"), 0o644); err != nil {
		t.Skipf("no FIFO here: %v", err)
	}
	done := make(chan MisconfigFixPreparedMsg, 1)
	go func() { done <- computeMisconfigFix(dir, fx.finding, scannedAtFixed) }()
	select {
	case msg := <-done:
		declinedWith(t, msg, reasonFileNotRegular)
	case <-time.After(5 * time.Second):
		t.Fatal("the preview of a FIFO blocks")
	}
}
