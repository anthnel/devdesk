package clipboard

import (
	"bytes"
	"testing"
)

// clip.exe reads UTF-8 in the console code page and pastes "é" as "Ã©"; the
// bytes it is handed must be UTF-16LE behind a BOM instead.
func TestUTF16LEWithBOM(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []byte
	}{
		{"empty is the BOM alone", "", []byte{0xFF, 0xFE}},
		{"ascii", "a", []byte{0xFF, 0xFE, 'a', 0x00}},
		{"accented", "é", []byte{0xFF, 0xFE, 0xE9, 0x00}},
		{"beyond the BMP is a surrogate pair", "😀", []byte{0xFF, 0xFE, 0x3D, 0xD8, 0x00, 0xDE}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := utf16LEWithBOM(tc.in); !bytes.Equal(got, tc.want) {
				t.Errorf("utf16LEWithBOM(%q) = % X, want % X", tc.in, got, tc.want)
			}
		})
	}
}

// Outside WSL nothing changes: atotto keeps the clipboard.
func TestClipExeIsOnlyUsedUnderWSL(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "")
	if _, ok := wslClipExe(); ok {
		t.Error("wslClipExe() chose clip.exe without WSL_DISTRO_NAME")
	}
}
