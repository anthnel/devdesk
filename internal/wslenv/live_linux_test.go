//go:build linux

package wslenv

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestLiveNativeLinuxIsNotWSL2 runs the real probe. It is skipped under WSL,
// where the answer depends on the machine.
func TestLiveNativeLinuxIsNotWSL2(t *testing.T) {
	rel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	if os.Getenv("WSL_DISTRO_NAME") != "" || strings.Contains(strings.ToLower(string(rel)), "microsoft") {
		t.Skip("running under WSL")
	}
	if got := Detect(context.Background()); got.WSL2 {
		t.Errorf("Detect() = %+v on native Linux, want WSL2=false", got)
	}
}

// TestLiveWSLIsWSL2 runs the real probe. It is skipped outside WSL; the
// networking mode is machine-dependent and not asserted.
func TestLiveWSLIsWSL2(t *testing.T) {
	rel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	if !strings.Contains(strings.ToLower(string(rel)), "wsl2") {
		t.Skip("not running under WSL2")
	}
	if got := Detect(context.Background()); !got.WSL2 {
		t.Errorf("Detect() = %+v under WSL2, want WSL2=true", got)
	}
}

// TestLiveDetectKindUnderWSL2 runs the real probe. It is skipped outside WSL2.
func TestLiveDetectKindUnderWSL2(t *testing.T) {
	rel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	if !strings.Contains(strings.ToLower(string(rel)), "wsl2") {
		t.Skip("not running under WSL2")
	}
	if got := DetectKind(); got != KindWSL2 {
		t.Errorf("DetectKind() = %d under WSL2, want KindWSL2", got)
	}
}

// TestLiveDetectKindNativeLinux runs the real probe. It is skipped under WSL.
func TestLiveDetectKindNativeLinux(t *testing.T) {
	rel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	if os.Getenv("WSL_DISTRO_NAME") != "" || strings.Contains(strings.ToLower(string(rel)), "microsoft") {
		t.Skip("running under WSL")
	}
	if got := DetectKind(); got != KindNone {
		t.Errorf("DetectKind() = %d on native Linux, want KindNone", got)
	}
}
