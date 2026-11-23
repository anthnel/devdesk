package netdiag

import "testing"

// The reserved constructors panic outside a test binary. Under test the real
// check always sees true, so the decision is tested on its own.
func TestReservedForTestsPanicsOutsideATestBinary(t *testing.T) {
	var got any
	func() {
		defer func() { got = recover() }()
		requireTestBinary("NewPortsKillResultMsg", false)
	}()
	if got == nil {
		t.Fatal("requireTestBinary(false) did not panic")
	}
	requireTestBinary("NewPortsKillResultMsg", true) // must not panic
}

func TestNewPortsKillResultMsgWorksUnderTest(t *testing.T) {
	msg, ok := NewPortsKillResultMsg("812").(portsKillResultMsg)
	if !ok || msg.pid != "812" || msg.gen != 0 {
		t.Fatalf("NewPortsKillResultMsg = %#v, want a generation-0 result for PID 812", msg)
	}
}
