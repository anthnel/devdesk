package netdiag

import (
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/netcheck"
)

// The pipeline is bounded by what the config says, in the unit the config says
// it in. Seconds and days are what a user types; netcheck wants durations, and
// the conversion is the one place the two can disagree.
func TestTheCheckSettingsComeFromTheConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Network.CheckTimeout = 30
	cfg.Network.PingCount = 9
	cfg.Network.CertExpiryWarnDays = 14

	got := New(cfg).checkSettings()

	want := netcheck.Settings{
		CheckTimeout:     30 * time.Second,
		PingCount:        9,
		ExpiryWarnWindow: 14 * 24 * time.Hour,
	}
	if got != want {
		t.Errorf("checkSettings() = %+v, want %+v", got, want)
	}
}

// A hand-edited zero must not become a dial with no deadline. The view does not
// guard: netcheck.Settings.normalize does, at every entry point, so there is
// one rule rather than one per caller.
func TestAZeroedConfigStillYieldsUsableSettings(t *testing.T) {
	cfg := config.Default()
	cfg.Network.CheckTimeout = 0
	cfg.Network.PingCount = 0
	cfg.Network.CertExpiryWarnDays = 0

	if got := New(cfg).checkSettings().Normalized(); got != netcheck.DefaultSettings() {
		t.Errorf("a zeroed config normalized to %+v, want the netcheck defaults", got)
	}
}

// The ports tab polls on the configured interval.
func TestThePortsRefreshComesFromTheConfig(t *testing.T) {
	cfg := config.Default()
	cfg.Network.PortsRefreshInterval = 5

	if got := New(cfg).portsModel.refresh; got != 5*time.Second {
		t.Errorf("refresh = %v, want 5s", got)
	}
}

// A non-positive interval would make tea.Tick fire without pausing, so it falls
// back rather than spinning a docker exec per frame.
func TestANonPositiveRefreshFallsBackRatherThanSpinning(t *testing.T) {
	pm := newPortsModel(0)

	if cmd := portsTickCmd(pm.refresh); cmd == nil {
		t.Fatal("no tick command was produced")
	}
	// The guard lives in portsTickCmd rather than the constructor: the value
	// travels from the config through the model, and the last reader is the
	// only place that cannot be bypassed.
	if pm.refresh > 0 {
		t.Errorf("refresh = %v; the constructor is not where the guard belongs", pm.refresh)
	}
}
