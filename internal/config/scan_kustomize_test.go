package config

import (
	"os"
	"strings"
	"testing"
)

// DEVDESK-767: kustomize builds an overlay with the network cut unless the
// context says a remote base may be fetched. The switch is the one setting
// that is not part of every tool, so it sits beside the shared ones under
// scan.tools.kustomize and nothing else about the block moves.

func TestAllowRemoteBasesIsOffByDefault(t *testing.T) {
	setupTmpHome(t)

	if Default().Scan.Tools.Kustomize.AllowRemoteBases {
		t.Error("a new context lets kustomize fetch remote bases")
	}
}

// A context written before the key existed keeps the network cut.
func TestAContextWithoutTheKeyDoesNotAllowRemoteBases(t *testing.T) {
	cfg := loadScan(t, `
  tools:
    kustomize:
      source: binary
      binary: /opt/kustomize
`)

	k := cfg.Scan.Tools.Kustomize
	if k.AllowRemoteBases {
		t.Error("a file without allow_remote_bases allows remote bases")
	}
	if k.Source != "binary" || k.Binary != "/opt/kustomize" {
		t.Errorf("the shared settings were not read through the inline block: %+v", k.ToolConfig)
	}
}

func TestAllowRemoteBasesIsReadFromTheKey(t *testing.T) {
	cfg := loadScan(t, `
  tools:
    kustomize:
      source: image
      allow_remote_bases: true
`)

	if !cfg.Scan.Tools.Kustomize.AllowRemoteBases {
		t.Error("allow_remote_bases: true was not read")
	}
	if cfg.Scan.Tools.Kustomize.Source != "image" {
		t.Errorf("source = %q, want image: the key must sit beside the shared settings", cfg.Scan.Tools.Kustomize.Source)
	}
}

// The key survives a save and a load, beside the settings every tool has.
func TestAllowRemoteBasesSurvivesASaveAndALoad(t *testing.T) {
	cfg := loadScan(t, "  timeout: 60\n")
	cfg.Scan.Tools.Kustomize.Binary = "/opt/kustomize"
	cfg.Scan.Tools.Kustomize.AllowRemoteBases = true
	if err := SaveContext(cfg, "default"); err != nil {
		t.Fatalf("SaveContext: %v", err)
	}

	path, _ := GetContextPath("default")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "allow_remote_bases: true") {
		t.Errorf("the saved file has no allow_remote_bases: true:\n%s", body)
	}
	again, err := LoadContext("default")
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}
	if !again.Scan.Tools.Kustomize.AllowRemoteBases || again.Scan.Tools.Kustomize.Binary != "/opt/kustomize" {
		t.Errorf("the round trip lost a setting: %+v", again.Scan.Tools.Kustomize)
	}
}

// Off is the default, so it is not written: a file that never asked for the
// switch does not grow a line, and the other tools' blocks do not get one.
func TestAllowRemoteBasesOffIsNotWritten(t *testing.T) {
	cfg := loadScan(t, "  timeout: 60\n")
	if err := SaveContext(cfg, "default"); err != nil {
		t.Fatalf("SaveContext: %v", err)
	}

	path, _ := GetContextPath("default")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(body), "allow_remote_bases") {
		t.Errorf("an off switch was written:\n%s", body)
	}
}

// Tool("kustomize") still hands out the shared settings, and they are the very
// ones inside the kustomize block: defaults, path expansion and detection loop
// over Tool(id) and must reach what the scan reads.
func TestToolKustomizeIsTheSharedBlockOfTheKustomizeSettings(t *testing.T) {
	var tools ScanTools

	got := tools.Tool(ToolKustomize)

	if got != &tools.Kustomize.ToolConfig {
		t.Errorf("Tool(%q) = %p, want the ToolConfig inside Kustomize (%p)", ToolKustomize, got, &tools.Kustomize.ToolConfig)
	}
	got.Image = "mirror/kustomize"
	if tools.Kustomize.Image != "mirror/kustomize" {
		t.Error("a write through Tool() did not reach the kustomize block")
	}
}

// The legacy flat keys still land in the shared settings, and the new switch
// stays off: a legacy bool can only say "on", and none of them is this one.
func TestMigratingTheLegacyKustomizeKeysLeavesRemoteBasesOff(t *testing.T) {
	cfg := loadScan(t, "  kustomize_source: binary\n  kustomize_path: /opt/kustomize\n")

	k := cfg.Scan.Tools.Kustomize
	if k.Source != "binary" || k.Binary != "/opt/kustomize" {
		t.Errorf("legacy keys not carried: %+v", k.ToolConfig)
	}
	if k.AllowRemoteBases {
		t.Error("a migration turned remote bases on")
	}
}
