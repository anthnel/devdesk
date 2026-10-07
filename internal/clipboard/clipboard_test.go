package clipboard

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// fakePowerShell puts a powershell.exe on PATH that records its stdin, and
// returns where the recording lands.
func fakePowerShell(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "stdin")
	script := "#!/bin/sh\ncat > '" + out + "'\n"
	if err := os.WriteFile(filepath.Join(dir, powerShellExe), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WSL_DISTRO_NAME", "Debian")
	return out
}

// What reaches PowerShell is the exact UTF-8 text, base64-encoded: no BOM to
// end up in the clipboard (D81), no byte a code page could reinterpret.
func TestWSLHandsPowerShellTheExactTextAsBase64(t *testing.T) {
	if os.PathListSeparator != ':' {
		t.Skip("the fake powershell.exe is a shell script")
	}
	out := fakePowerShell(t)

	const text = "/home/me/café/😀.txt"
	if err := WriteAll(text); err != nil {
		t.Fatalf("WriteAll() error = %v", err)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		t.Fatalf("stdin is not base64: %q", raw)
	}
	if got := string(decoded); got != text {
		t.Errorf("decoded stdin = %q, want %q", got, text)
	}
	for _, b := range raw {
		if b >= 0x80 {
			t.Fatalf("stdin carries a non-ASCII byte %#x; a code page could alter it", b)
		}
	}
}

// Outside WSL nothing changes: atotto keeps the clipboard.
func TestPowerShellIsOnlyUsedUnderWSL(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "")
	if _, ok := wslPowerShell(); ok {
		t.Error("wslPowerShell() chose PowerShell without WSL_DISTRO_NAME")
	}
}
