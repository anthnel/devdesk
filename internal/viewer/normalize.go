package viewer

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Normalize turns raw bytes into text a viewport can hold.
//
// Both steps are needed, and the reasons come from the container logs pane this
// was moved out of: systemd and anything using a logging library emit coloured
// output, and some containers use \r to overwrite a progress bar in place.
// Either one corrupts the frame — an escape sequence is measured as its bytes
// rather than its width, and a carriage return sends the cursor back over
// content Bubble Tea believes it has drawn.
//
// It runs for *every* document, not just logs. A file can contain the same
// bytes, and a viewer that renders it faithfully renders it broken.
//
// The cost is stated rather than hidden: a JSON string holding a literal escape
// character loses it. That is a trade against a corrupted frame, and the frame
// wins.
func Normalize(data []byte) string {
	text := ansi.Strip(string(data))
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "")
}
