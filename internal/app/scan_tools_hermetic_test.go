package app

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

// App.Init starts a detection of the scanners. Left alone, it runs the real
// `trivy`, `gitleaks` and `plumber` found on the machine (`--version`), so a
// test that plays Init gets a different answer, and a different duration, on a
// workstation that has them and on a CI runner that does not (DEVDESK-645).
//
// This guard puts a decoy of each tool first on PATH. A decoy that is ever
// executed leaves a witness file behind and fails. The test is written against
// what is observable, not against the name of the seam that fixes it: once the
// router's detection is replaced in TestMain, nothing reaches PATH at all.
//
// The production value of the seam is pinned by the second test of this file.
func TestInitDoesNotRunTheScannersOfTheHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the decoys are POSIX shell scripts")
	}

	bin, witnesses := t.TempDir(), t.TempDir()
	tools := []string{"trivy", "gitleaks", "plumber"}
	for _, name := range tools {
		witness := filepath.Join(witnesses, name)
		script := "#!/bin/sh\necho ran > '" + witness + "'\nexit 1\n"
		//nolint:gosec // an executable decoy, by design
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	a := router(t, &fakeView{})
	appInit(t, a)

	for _, name := range tools {
		if _, err := os.Stat(filepath.Join(witnesses, name)); err == nil {
			t.Errorf("App.Init executed the %s found on PATH: the result of the tests depends on the tools of the machine", name)
		}
	}
}

// productionDetectTools is the value of the seam before TestMain replaces it:
// a package-level initialiser of a _test.go file runs before TestMain.
var productionDetectTools = detectTools

// The seam is neutralised for every test, so nothing else would notice a router
// that stopped calling scan.Detect in production: this pins the real wiring.
func TestDetectToolsIsScanDetectInProduction(t *testing.T) {
	got := reflect.ValueOf(productionDetectTools).Pointer()
	want := reflect.ValueOf(scan.Detect).Pointer()
	if got != want {
		t.Errorf("production detectTools is not scan.Detect: the router would not detect the scanners")
	}
	// And TestMain did replace it: the other tests do not reach the machine.
	if reflect.ValueOf(detectTools).Pointer() == want {
		t.Errorf("detectTools is still scan.Detect in the tests: TestMain must neutralise it")
	}
}
