package credentials

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain is the only place that allows the host-store guard to stand down
// (DEVDESK-610). The tests of this package either mock go-keyring themselves or
// run git against a hermetic config (hermeticGit), so none of them can reach
// the real Secret Service. The mock installed here is a net for a test that
// forgets to install its own.
func TestMain(m *testing.M) {
	keyring.MockInit()
	hostReachableUnderTest.Store(true)
	os.Exit(m.Run())
}
