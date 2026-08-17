package components

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthnel/devdesk/internal/ui/theme"
)

// expiry is the message the current message's timer will eventually deliver.
//
// Tests build it directly rather than running the Cmd, which really does sleep.
// TestTheTimerDeliversAnExpiryForTheMessageItWasStartedFor is the one that runs
// it — with the duration shortened — and it is what ties this shortcut to the
// real thing.
func expiry(f *FooterMessage) tea.Msg { return ClearFooterMsg{ID: f.id} }

// ── Levels and colours ───────────────────────────────────────────────────────

func TestEachLevelRendersInItsOwnStyle(t *testing.T) {
	const text = "a message"
	cases := []struct {
		name string
		set  func(f *FooterMessage, s string) tea.Cmd
		want lipgloss.Style
	}{
		{"info", (*FooterMessage).Info, theme.FooterInfoStyle},
		{"warning", (*FooterMessage).Warn, theme.FooterWarnStyle},
		{"error", (*FooterMessage).Error, theme.FooterErrorStyle},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var f FooterMessage
			c.set(&f, text)

			got := f.View(60, Status{})
			if want := c.want.Render(text); !strings.Contains(got, want) {
				t.Errorf("%s level does not render in its own style\n got: %q\nwant it to contain: %q",
					c.name, got, want)
			}
		})
	}
}

// The three levels must be distinguishable. Info used to be ColorHighlight, a
// yellow one notch from the warning's orange.
func TestTheThreeLevelsAreDistinguishable(t *testing.T) {
	seen := map[string]string{}
	for name, color := range map[string]string{
		"info":    string(theme.ColorFooterInfo),
		"warning": string(theme.ColorFooterWarn),
		"error":   string(theme.ColorFooterError),
	} {
		if other, dup := seen[color]; dup {
			t.Errorf("%s and %s share the colour %s", name, other, color)
		}
		seen[color] = name
	}
}

// Green belonged to the security view's status line and to nothing else in a
// footer. It is reserved for status icons (Rule 121).
func TestNoLevelIsGreen(t *testing.T) {
	for _, c := range []lipgloss.Color{theme.ColorFooterInfo, theme.ColorFooterWarn, theme.ColorFooterError} {
		if c == theme.ColorOK {
			t.Errorf("a footer level renders in the OK green %q", c)
		}
	}
}

// ── Layout ───────────────────────────────────────────────────────────────────

func TestEveryLevelIsCentered(t *testing.T) {
	const width = 60
	for _, set := range []func(*FooterMessage, string) tea.Cmd{
		(*FooterMessage).Info, (*FooterMessage).Warn, (*FooterMessage).Error,
	} {
		var f FooterMessage
		set(&f, "centre me")

		line := f.View(width, Status{})
		if !isCentered(line, "centre me") {
			t.Errorf("message is not centred on a %d-wide line:\n%q", width, line)
		}
	}
}

func TestTheStatusLineIsCenteredToo(t *testing.T) {
	var f FooterMessage
	line := f.View(60, Status{Text: "3 actions running…"})
	if !isCentered(line, "3 actions running…") {
		t.Errorf("status line is not centred:\n%q", line)
	}
}

// Rule 124 budgets one line for the message whatever it holds, so an empty
// footer still renders one — filled with the app background (Rule 115).
func TestAnEmptyFooterStillRendersOneFullWidthLine(t *testing.T) {
	var f FooterMessage
	line := f.View(40, Status{})

	if strings.Contains(line, "\n") {
		t.Errorf("empty footer spans several lines:\n%q", line)
	}
	if got := theme.StringWidth(stripANSI(line)); got != 40 {
		t.Errorf("empty footer line is %d columns wide, want 40", got)
	}
}

// A line wider than the viewport wraps onto a second one, and everything below
// it shifts by a line.
func TestALongMessageIsTruncatedRatherThanWrapped(t *testing.T) {
	var f FooterMessage
	f.Error(strings.Repeat("very long failure ", 20))

	line := f.View(40, Status{})
	if strings.Contains(line, "\n") {
		t.Errorf("long message wrapped onto a second line:\n%q", line)
	}
	if got := theme.StringWidth(stripANSI(line)); got != 40 {
		t.Errorf("truncated line is %d columns wide, want 40", got)
	}
}

func TestALongStatusIsTruncatedToo(t *testing.T) {
	var f FooterMessage
	f.SetSpinnerFrame("⠋")

	line := f.View(30, Status{Text: strings.Repeat("loading ", 20), Spinner: true})
	if strings.Contains(line, "\n") {
		t.Errorf("long status wrapped onto a second line:\n%q", line)
	}
	if got := theme.StringWidth(stripANSI(line)); got != 30 {
		t.Errorf("truncated status line is %d columns wide, want 30", got)
	}
}

// ── Precedence ───────────────────────────────────────────────────────────────

func TestATimedMessageBeatsTheDerivedStatus(t *testing.T) {
	var f FooterMessage
	f.Error("Action failed — check logs")

	line := stripANSI(f.View(80, Status{Text: "Stopping web…"}))
	if !strings.Contains(line, "Action failed") {
		t.Errorf("the error was not shown:\n%q", line)
	}
	if strings.Contains(line, "Stopping web") {
		t.Errorf("the status displaced the error:\n%q", line)
	}
}

func TestTheStatusReturnsOnceTheMessageExpires(t *testing.T) {
	var f FooterMessage
	f.Info("Image pulled: nginx")

	f.Handle(expiry(&f))

	line := stripANSI(f.View(80, Status{Text: "Loading images…"}))
	if !strings.Contains(line, "Loading images") {
		t.Errorf("the status did not come back after the message expired:\n%q", line)
	}
}

// ── The spinner ──────────────────────────────────────────────────────────────

func TestALoadingStatusShowsTheSpinnerFrameThenTheText(t *testing.T) {
	var f FooterMessage
	f.SetSpinnerFrame("⠹")

	line := stripANSI(f.View(60, Status{Text: "Loading images…", Spinner: true}))
	if !strings.Contains(line, "⠹ Loading images…") {
		t.Errorf("loading status does not read as spinner then text:\n%q", line)
	}
}

func TestANonLoadingStatusHasNoSpinner(t *testing.T) {
	var f FooterMessage
	f.SetSpinnerFrame("⠹")

	line := stripANSI(f.View(60, Status{Text: "Pruning containers…"}))
	if strings.Contains(line, "⠹") {
		t.Errorf("a status that did not ask for a spinner got one:\n%q", line)
	}
}

// A message is not a loading state: the spinner belongs to the status.
func TestATimedMessageNeverCarriesTheSpinner(t *testing.T) {
	var f FooterMessage
	f.SetSpinnerFrame("⠹")
	f.Warn("Scan already in progress")

	line := stripANSI(f.View(60, Status{Text: "Loading images…", Spinner: true}))
	if strings.Contains(line, "⠹") {
		t.Errorf("a timed message rendered the spinner:\n%q", line)
	}
}

// ── The expiry timer ─────────────────────────────────────────────────────────

// The one test that runs the real Cmd. It is what ties `expiry` — the shortcut
// every other test takes — to the message the timer actually delivers.
//
// The duration is shortened for the length of the test: three seconds of real
// sleep is a lot to pay to learn what tea.Tick puts in an envelope, and the Cmd
// is called once, not twice, because each call sleeps again.
func TestTheTimerDeliversAnExpiryForTheMessageItWasStartedFor(t *testing.T) {
	restore := FooterMsgDuration
	FooterMsgDuration = time.Millisecond
	t.Cleanup(func() { FooterMsgDuration = restore })

	var f FooterMessage
	cmd := f.Info("Process 4242 terminated")
	if cmd == nil {
		t.Fatal("setter returned no timer; a message set without one never clears (Rule 128)")
	}

	delivered := cmd()
	if want := expiry(&f); delivered != want {
		t.Fatalf("timer delivered %#v, want %#v", delivered, want)
	}
	if !f.Handle(delivered) {
		t.Fatal("the message did not consume its own timer")
	}
	if f.IsSet() {
		t.Errorf("the message survived its timer: %q", f.Text())
	}
}

// The production duration is what Rule 128 states. The test above overrides it,
// so something has to assert the default it overrides.
func TestAMessageGetsThreeSeconds(t *testing.T) {
	if FooterMsgDuration != 3*time.Second {
		t.Errorf("FooterMsgDuration = %v, want the three seconds Rule 128 states", FooterMsgDuration)
	}
}

// The bug every local implementation carried: a message set at t+2.9s was
// wiped at t+3s by its predecessor's timer.
func TestAStaleTimerDoesNotClearANewerMessage(t *testing.T) {
	var f FooterMessage
	f.Info("first")
	stale := expiry(&f)
	f.Error("second, set a moment later")

	if f.Handle(stale) {
		t.Error("the newer message consumed the older message's timer")
	}
	if got := f.Text(); got != "second, set a moment later" {
		t.Errorf("the newer message was cleared by a stale timer; text = %q", got)
	}
}

// The ID is also what makes the type safe to share between packages: a timer
// landing on another view must not clear that view's message.
func TestAnotherViewsTimerIsIgnored(t *testing.T) {
	var mine, theirs FooterMessage
	theirs.Info("their message")
	theirTimer := expiry(&theirs)
	mine.Error("my message")

	if mine.Handle(theirTimer) {
		t.Error("a timer from another view was consumed")
	}
	if !mine.IsSet() {
		t.Error("a timer from another view cleared this view's message")
	}
}

func TestHandleIgnoresMessagesItDoesNotOwn(t *testing.T) {
	var f FooterMessage
	f.Info("still here")

	if f.Handle(tea.KeyMsg{}) {
		t.Error("Handle claimed a message that is not a ClearFooterMsg")
	}
	if !f.IsSet() {
		t.Error("an unrelated message cleared the footer")
	}
}

func TestClearRemovesTheMessageImmediately(t *testing.T) {
	var f FooterMessage
	f.Warn("Nothing selected")
	f.Clear()

	if f.IsSet() {
		t.Errorf("Clear left the message in place: %q", f.Text())
	}
	if line := f.View(40, Status{}); line != theme.EmptyLineBg(40) {
		t.Errorf("a cleared footer does not render as an empty line:\n%q", line)
	}
}

// Setting an empty text clears rather than displaying a blank styled line, so
// `m.footer.Info(summary())` is safe when the summary is empty.
func TestSettingAnEmptyTextClears(t *testing.T) {
	var f FooterMessage
	f.Error("something")

	if cmd := f.Info(""); cmd != nil {
		t.Error("clearing via an empty text started a timer")
	}
	if f.IsSet() {
		t.Errorf("an empty text left a message: %q", f.Text())
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// isCentered reports whether text sits in the middle of a padded line, with the
// left and right margins differing by at most one column.
func isCentered(line, text string) bool {
	plain := stripANSI(line)
	idx := strings.Index(plain, text)
	if idx < 0 {
		return false
	}
	left := theme.StringWidth(plain[:idx])
	right := theme.StringWidth(plain[idx+len(text):])
	return left-right <= 1 && right-left <= 1 && left > 0
}

// stripANSI removes escape sequences so a test can measure and search the text
// a user actually sees.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++ // the 'm' itself
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
