package app

import (
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// OC-24 (DEVDESK-822), step 2, criterion 1: a pinned engine that is absent from PATH
// must stay the CURRENT engine, so the views name it ("Podman not found on PATH...")
// instead of the one that happened to be current before ("Docker not found...").
// engine.Resolve fails and resolveContainerEngine used to leave the previous shape in
// place.
//
// No fake binary and no engine is started: PATH holds an empty directory, so
// engine.Resolve finds nothing (guard F2 is not concerned).
func TestAnAbsentPinnedEngineIsStillTheCurrentOne(t *testing.T) {
	tests := []struct {
		name       string
		preference string
		previous   string // the engine that was current before the call
		want       string
	}{
		{"podman pinned, docker was current", engine.Podman, engine.Docker, engine.Podman},
		{"docker pinned, podman was current", engine.Docker, engine.Podman, engine.Docker},
		{"podman pinned, podman was current", engine.Podman, engine.Podman, engine.Podman},
		// Auto with neither engine installed: the default engine, as before.
		{"auto, nothing installed", engine.Auto, engine.Podman, engine.Docker},
		{"empty preference, nothing installed", "", engine.Podman, engine.Docker},
		// A configured path that does not exist: the engine is deduced from the file name.
		{"a podman path that does not exist", "/opt/x/podman-remote", engine.Docker, engine.Podman},
		{"another path that does not exist", "/opt/x/nerdctl", engine.Podman, engine.Docker},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := engine.Current()
			t.Cleanup(func() { engine.SetCurrent(previous) })
			engine.SetCurrent(engine.ShapeFor(tt.previous))
			t.Setenv("PATH", t.TempDir())
			a := routerAt(t, &fakeView{}, 180, 40)
			a.config.App.ContainerEngine = tt.preference

			a.resolveContainerEngine()

			if got := engine.Current().Name; got != tt.want {
				t.Errorf("engine.Current().Name = %q after resolving %q with nothing on PATH, want %q", got, tt.preference, tt.want)
			}
		})
	}
}
