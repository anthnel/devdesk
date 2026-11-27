package enginecontract

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/engineguard"
)

// Session is what a contract run knows once Require accepted it.
type Session struct {
	Engine Engine
	RunID  string
	Mode   Mode
	OutDir string
}

var bannerOnce sync.Once

// Require is what the requireEngine(t) of every package under contract calls
// first (guard G5). In order: the gate (a closed gate skips t, a refusal fails
// it, and nothing has touched the engine yet), engineguard.AllowRealEngine (the
// same gate checked a second time, independently, which also lets the guard G1
// pass this test's reaches for its length), the check that no variable points
// the engine elsewhere (CheckEngineEnv), the run id, the probe (a failure is
// fatal: the engine does not answer), the version check (recording: a version
// outside N and N-1 is fatal; verifying: skipped, no fixture can exist for it),
// then the banner, in t.Logf and once per process on stderr.
func Require(t testing.TB, probe Probe) *Session {
	t.Helper()
	d := Decide(os.Getenv)
	if d.Skip != "" {
		t.Skip(d.Skip)
	}
	if d.Refuse != "" {
		t.Fatal(d.Refuse)
	}
	engineguard.AllowRealEngine(t)
	if err := CheckEngineEnv(os.Getenv); err != nil {
		t.Fatal(err)
	}

	runID, err := ProcessRunID(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	e, err := probe(d.Tool)
	if err != nil {
		t.Fatalf("the %s engine does not answer: %v", d.Tool, err)
	}
	if err := checkVersion(e); err != nil {
		if d.Mode == Record {
			t.Fatalf("%v: a fixture of that version must not be recorded", err)
		}
		t.Skipf("contract test skipped: %v: no fixture can exist for it", err)
	}

	banner := Banner(e, runID, d.Mode)
	bannerOnce.Do(func() { fmt.Fprintln(os.Stderr, banner) })
	t.Log(banner)
	return &Session{Engine: e, RunID: runID, Mode: d.Mode, OutDir: d.OutDir}
}

// checkVersion reads versions.env and checks the engine against it.
func checkVersion(e Engine) error {
	file, err := VersionsFile(os.Getenv)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("versions file: %w", err)
	}
	supported, err := enginefixture.ParseVersions(data)
	if err != nil {
		return err
	}
	return CheckSupported(e, supported)
}
