package netdiag

import (
	"testing"

	"github.com/anthnel/devdesk/internal/netcheck"
)

// SetCheckEnvForTest replaces the Env the Diagnostics pipeline reaches the
// network through, so a test in another package (the router's, which builds
// the view itself) runs the pipeline without a real ICMP echo. It panics
// outside a test binary, like credentials.SwapHostStoreForTest; production
// never calls it.
func (m *Model) SetCheckEnvForTest(env func(netcheck.Settings) netcheck.Env) {
	requireTestBinary("SetCheckEnvForTest", testing.Testing())
	m.checkEnv = env
}

// requireTestBinary panics unless underTest. Callers pass testing.Testing(); the
// flag is a parameter so a test can check the panic from inside a test binary.
func requireTestBinary(name string, underTest bool) {
	if !underTest {
		panic("netdiag: " + name + " called outside a test binary")
	}
}
