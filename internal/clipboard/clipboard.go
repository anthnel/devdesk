// Package clipboard writes text to the system clipboard.
//
// It is atotto/clipboard everywhere but one place: under WSL with no X or
// Wayland tool installed, atotto falls back to clip.exe and pipes it UTF-8.
// clip.exe reads its input in the console's code page instead, so every
// non-ASCII character is pasted as mojibake — "é" comes back as "Ã©".
//
// Handing clip.exe UTF-16LE behind a byte-order mark fixes the accents but not
// the text: clip.exe keeps the BOM, and a terminal pastes it as a leading
// U+FEFF (D81). Without the BOM, clip.exe guesses the encoding, and its guess
// is least reliable on short text — a path. So under WSL the text goes to
// PowerShell's Set-Clipboard instead, base64-encoded: what crosses the pipe is
// plain ASCII, which no code page can alter, and Set-Clipboard receives the
// exact string.
package clipboard

import (
	"encoding/base64"
	"os"
	"os/exec"
	"runtime"
	"strings"

	atotto "github.com/atotto/clipboard"
)

// powerShellExe is the Windows PowerShell WSL's interop puts on PATH.
const powerShellExe = "powershell.exe"

// setClipboardScript reads base64 from stdin, decodes it as UTF-8 and sets the
// clipboard to exactly that string.
const setClipboardScript = "Set-Clipboard -Value ([Text.Encoding]::UTF8.GetString(" +
	"[Convert]::FromBase64String([Console]::In.ReadToEnd().Trim())))"

// WriteAll puts text on the system clipboard.
func WriteAll(text string) error {
	if path, ok := wslPowerShell(); ok {
		return writePowerShell(path, text)
	}
	return atotto.WriteAll(text)
}

// wslPowerShell reports the powershell.exe to use when running under WSL.
//
// WSL is recognized by WSL_DISTRO_NAME, the same signal internal/ui/terminal
// uses. PowerShell is preferred there even when xclip or wl-copy exists: it
// writes the Windows clipboard directly, which is where a WSL user pastes,
// while xclip without an X server fails outright.
func wslPowerShell() (string, bool) {
	if runtime.GOOS != "linux" || os.Getenv("WSL_DISTRO_NAME") == "" {
		return "", false
	}
	path, err := exec.LookPath(powerShellExe)
	if err != nil {
		return "", false
	}
	return path, true
}

func writePowerShell(path, text string) error {
	cmd := exec.Command(path, "-NoProfile", "-NonInteractive", "-Command", setClipboardScript)
	cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString([]byte(text)))
	return cmd.Run()
}
