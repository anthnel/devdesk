package ociresources

import (
	"log"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/docker"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// OC-20 (DEVDESK-818, S4): a launch that mounts something sensitive of the host
// asks first. The classification is docker.SensitiveMounts; this file is the
// form's half: the --mount values of the Volumes field, the check at the submit
// and the modal that holds the launch back.

// The two actions of the modal. The engine socket gets a named action of its own
// so that no habit (a letter, an enter) can answer for the user.
const (
	mountAnywayLabel      = "Mount anyway"
	mountEngineSocketText = "Mount the engine socket"
)

// engineSocketFooterText is the footer line while the modal names an engine
// socket, at level Error and derived on every render (Rule 128), so it does not
// vanish after 3 s.
const engineSocketFooterText = "Mounting the engine socket gives the container control of the engine"

// mountCheck is a launch held back by the modal: what was submitted and what
// made it sensitive.
type mountCheck struct {
	msg    LaunchFormSubmitMsg
	mounts []docker.SensitiveMount
}

// splitVolumeField splits the Volumes field into -v entries and --mount values.
// The field is comma separated, and a --mount value holds commas itself, so a
// value starts at a token beginning with `type=` and runs on over the tokens
// that are `key=value` or the bare flags `readonly` and `ro`, until the next
// `type=` token (the next value) or the first other token (a -v entry). Keys and
// flags are read in any case, as the engine does. A --mount value that does not
// start with `type=` is not recognized (a bind needs `type=bind` anyway). Blank
// tokens are skipped.
func splitVolumeField(value string) (volumes, mounts []string) {
	var current string // the --mount value being read, "" when none
	flush := func() {
		if current != "" {
			mounts = append(mounts, current)
			current = ""
		}
	}
	for tok := range strings.SplitSeq(value, ",") {
		tok = strings.TrimSpace(tok)
		switch {
		case tok == "":
		case len(tok) >= 5 && strings.EqualFold(tok[:5], "type="):
			flush()
			current = tok
		case current != "" && isMountToken(tok):
			current += "," + tok
		default:
			flush()
			volumes = append(volumes, tok)
		}
	}
	flush()
	return volumes, mounts
}

// isMountToken reports whether tok continues a --mount value: `readonly`, `ro`,
// or `key=value` with a key made of letters, digits and dashes (so `/a=b:/x`, a
// -v entry, is not one).
func isMountToken(tok string) bool {
	if strings.EqualFold(tok, "readonly") || strings.EqualFold(tok, "ro") {
		return true
	}
	key, _, ok := strings.Cut(tok, "=")
	if !ok || key == "" || !unicode.IsLetter(rune(key[0])) {
		return false
	}
	for _, r := range key {
		if r != '-' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// printable replaces the control and format characters of a typed path (escape
// sequences, bidirectional marks) so that the modal never draws what the user did
// not see typed.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return '?'
		}
		return r
	}, s)
}

// hasEngineSocket reports whether one of the mounts is an engine socket.
func hasEngineSocket(mounts []docker.SensitiveMount) bool {
	for _, m := range mounts {
		if m.EngineSocket {
			return true
		}
	}
	return false
}

// mountModalChrome is what the modal takes around its text: the border (2) and
// the padding (4).
const mountModalChrome = 6

// newMountModal builds the modal for the mounts of a launch: the resolved path of
// each with its reason, one action and Cancel (the default). width is the
// terminal's: the text is wrapped to fit it, a path hard-wrapped so that none of
// it is cut off; 0 leaves the text as it is.
func newMountModal(mounts []docker.SensitiveMount, width int) *sharedcomponents.ChoiceModal {
	limit := 0
	if width > 0 {
		limit = max(width-mountModalChrome, 16)
	}
	var b strings.Builder
	title, action := "Sensitive mounts", mountAnywayLabel
	if hasEngineSocket(mounts) {
		title, action = "Engine socket", mountEngineSocketText
		b.WriteString(wrapText("Mounting the engine socket gives the container control of the engine.", limit, 0) + "\n")
	}
	b.WriteString(wrapText("This launch mounts sensitive parts of this host:", limit, 0))
	for _, m := range mounts {
		b.WriteString("\n\n" + wrapPath(printable(m.Path), limit, 2) + "\n" + wrapText(m.Reason, limit, 4))
	}
	return sharedcomponents.NewChoiceModal(title, b.String(), action)
}

// wrapText wraps s at word boundaries to limit columns, each line indented; no
// wrapping when limit is 0.
func wrapText(s string, limit, indent int) string {
	if limit > 0 {
		s = ansi.Wrap(s, max(limit-indent, 8), "")
	}
	return indentLines(s, indent)
}

// wrapPath hard-wraps a path to limit columns: it has no word boundary to break
// at, and every character of it must stay on screen.
func wrapPath(s string, limit, indent int) string {
	if limit > 0 {
		s = ansi.Hardwrap(s, max(limit-indent, 8), true)
	}
	return indentLines(s, indent)
}

func indentLines(s string, indent int) string {
	pad := strings.Repeat(" ", indent)
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

// holdForSensitiveMounts opens the modal when the submitted launch mounts
// something sensitive and reports whether it did. The form stays under it, with
// its values and its focus (Cancel gives it back); nothing is started.
func (m Model) holdForSensitiveMounts(msg LaunchFormSubmitMsg) (Model, bool) {
	// Whatever the form's state: a submit that outlives it (esc was pressed while
	// the message was in flight) is held like any other, never launched unasked.
	mounts := docker.SensitiveMounts(msg.Opts)
	if len(mounts) == 0 {
		return m, false
	}
	categories := make([]string, len(mounts))
	for i, s := range mounts {
		categories[i] = string(s.Category)
	}
	// Categories only: a path may hold what its owner did not mean to log.
	log.Printf("INFO [oci_resources] launch held for confirmation: sensitive mounts %s", strings.Join(categories, ","))
	m.mountModal = newMountModal(mounts, m.width)
	m.mountPending = &mountCheck{msg: msg, mounts: mounts}
	return m, true
}

// handleMountChoicePicked launches what the modal held back.
func (m Model) handleMountChoicePicked() (tea.Model, tea.Cmd) {
	pending := m.mountPending
	m.mountModal, m.mountPending = nil, nil
	if pending == nil {
		return m, nil
	}
	return m.startLaunch(pending.msg)
}

// handleMountChoiceCancelled drops the held launch: the form is back as it was.
func (m Model) handleMountChoiceCancelled() (tea.Model, tea.Cmd) {
	m.mountModal, m.mountPending = nil, nil
	return m, nil
}

// mountStatus is the footer line while the modal is open: an Error for an engine
// socket, derived on every frame with no timer. A plain sensitive mount has
// nothing to add to the modal.
func (m Model) mountStatus() (sharedcomponents.Status, bool) {
	if m.mountModal == nil || m.mountPending == nil || !hasEngineSocket(m.mountPending.mounts) {
		return sharedcomponents.Status{}, false
	}
	return sharedcomponents.Status{Text: engineSocketFooterText, Level: sharedcomponents.LevelError}, true
}
