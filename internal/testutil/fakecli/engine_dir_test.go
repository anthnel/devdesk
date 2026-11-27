package fakecli_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/anthnel/devdesk/internal/engineguard"
	"github.com/anthnel/devdesk/internal/testutil/fakecli"
)

// OC-03a (DEVDESK-798): the directories fakecli creates are the ones the engine
// guard accepts. The API these tests pin down:
//
//	fakecli.AllowFakeCLIDir(t testing.TB, dir string) // declares dir for the length of t; Install and InstallAt call it

func TestInstallDeclaresItsDirectoryToTheEngineGuard(t *testing.T) {
	engineguard.Isolate(t)
	fake := fakecli.Install(t, fakecli.Scenario{{Prefix: true}}, "docker")

	engineguard.Check("LookPath", "docker") // resolved through the PATH Install set
	engineguard.Check("Build", filepath.Join(fake.Dir(), "docker"))

	if hits := engineguard.Hits(); len(hits) != 0 {
		t.Errorf("Hits() = %+v, want none for a tool Install put on PATH", hits)
	}
}

func TestInstallAtDeclaresItsDirectoryToTheEngineGuard(t *testing.T) {
	engineguard.Isolate(t)
	fake := fakecli.Install(t, fakecli.Scenario{{Prefix: true}})
	path := fake.InstallAt(t, t.TempDir(), "podman")

	engineguard.Check("Run", path)

	if hits := engineguard.Hits(); len(hits) != 0 {
		t.Errorf("Hits() = %+v, want none for a tool InstallAt linked", hits)
	}
}

func TestAllowFakeCLIDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("an executable is recognised by its extension on Windows")
	}
	tests := []struct {
		name     string
		declare  bool
		wantHits int
	}{
		{"declared", true, 0},
		{"not declared", false, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engineguard.SurveyMode(t) // a reach is recorded here, not refused
			dir := t.TempDir()
			binary := filepath.Join(dir, "docker")
			if err := os.WriteFile(binary, nil, 0o700); err != nil {
				t.Fatal(err)
			}
			if tt.declare {
				fakecli.AllowFakeCLIDir(t, dir)
			}

			engineguard.Check("Run", binary)

			if hits := engineguard.Hits(); len(hits) != tt.wantHits {
				t.Errorf("Hits() = %+v, want %d", hits, tt.wantHits)
			}
		})
	}
}
