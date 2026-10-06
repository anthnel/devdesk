// Package clipboard writes text to the system clipboard.
//
// It is atotto/clipboard everywhere but one place: under WSL with no X or
// Wayland tool installed, atotto falls back to clip.exe and pipes it UTF-8.
// clip.exe reads its input in the console's code page instead, so every
// non-ASCII character is pasted as mojibake — "é" comes back as "Ã©". Given
// UTF-16LE behind a byte-order mark, clip.exe reads it as Unicode, which is
// what this package hands it.
package clipboard

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"runtime"
	"unicode/utf16"

	atotto "github.com/atotto/clipboard"
)

// clipExe is the Windows clipboard tool WSL's interop puts on PATH.
const clipExe = "clip.exe"

// WriteAll puts text on the system clipboard.
func WriteAll(text string) error {
	if path, ok := wslClipExe(); ok {
		return writeClipExe(path, text)
	}
	return atotto.WriteAll(text)
}

// wslClipExe reports the clip.exe to use when running under WSL.
//
// WSL is recognized by WSL_DISTRO_NAME, the same signal internal/ui/terminal
// uses. clip.exe is preferred there even when xclip or wl-copy exists: it
// writes the Windows clipboard directly, which is where a WSL user pastes,
// while xclip without an X server fails outright.
func wslClipExe() (string, bool) {
	if runtime.GOOS != "linux" || os.Getenv("WSL_DISTRO_NAME") == "" {
		return "", false
	}
	path, err := exec.LookPath(clipExe)
	if err != nil {
		return "", false
	}
	return path, true
}

func writeClipExe(path, text string) error {
	cmd := exec.Command(path)
	cmd.Stdin = bytes.NewReader(utf16LEWithBOM(text))
	return cmd.Run()
}

// utf16LEWithBOM encodes text as UTF-16 little-endian, preceded by the BOM
// that tells clip.exe the input is Unicode rather than the console code page.
func utf16LEWithBOM(text string) []byte {
	units := utf16.Encode([]rune(text))
	out := make([]byte, 2, 2+2*len(units))
	out[0], out[1] = 0xFF, 0xFE
	for _, u := range units {
		out = binary.LittleEndian.AppendUint16(out, u)
	}
	return out
}
