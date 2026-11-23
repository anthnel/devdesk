package security

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// DEVDESK-751: "Re-scanning to confirm the fix..." is a state that ends with the
// verification. A re-scan that fails or is stopped never reaches
// verifyMisconfigCmd, so the end must come from the finished message itself.

func TestAFailedRescanEndsTheRescanningStatus(t *testing.T) {
	for name, err := range map[string]error{
		"failed":  errors.New("trivy exploded"),
		"stopped": context.Canceled,
	} {
		t.Run(name, func(t *testing.T) {
			m, dir := fixDetail(t)
			m = writeFix(t, m, dir)
			if _, ok := m.verifyingStatus(); !ok {
				t.Fatal("no re-scan is announced after the write")
			}

			m = feed(t, m, InventoryScanFinishedMsg{Name: dir, Err: err})

			if st, ok := m.verifyingStatus(); ok {
				t.Errorf("verifyingStatus() = %+v after the re-scan ended in an error", st)
			}
			if m.misconfigVerifying != nil {
				t.Error("the pending verification outlived its re-scan")
			}
			if st := m.status(); strings.Contains(st.Text, "Re-scanning") || st.Activity {
				t.Errorf("status = %+v, want no activity", st)
			}
			if m.WantsBlink() {
				t.Error("WantsBlink() is true with nothing running: the dot would blink for ever")
			}
			if m.state != StateDetails {
				t.Error("an error of the re-scan closed the detail")
			}
		})
	}
}

// Another target's failed scan says nothing about the verification.
func TestAnotherTargetsFailedScanKeepsTheRescanningStatus(t *testing.T) {
	m, dir := fixDetail(t)
	m = writeFix(t, m, dir)
	m = feed(t, m, InventoryScanFinishedMsg{Name: "/elsewhere", Err: errors.New("boom")})
	if _, ok := m.verifyingStatus(); !ok {
		t.Error("a scan of another target ended the verification")
	}
}
