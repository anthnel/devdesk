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
	if !testing.Testing() {
		panic("netdiag: SetCheckEnvForTest called outside a test binary")
	}
	m.checkEnv = env
}
