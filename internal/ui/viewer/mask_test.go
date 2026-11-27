package viewer

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/secretenv"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	viewerpkg "github.com/anthnel/devdesk/internal/viewer"
)

// OC-13 (DEVDESK-811, S9): a maskable source (the container inspect) opens with
// its sensitive values hidden. `m` reveals them, `Y` copies what is shown, a
// search runs on what is shown, a refresh keeps the state, and the state is
// never persisted. The viewer knows nothing about docker: what is sensitive in
// the document is the source's business (viewer.Maskable), which receives the
// matcher built from the context's containers.sensitive_env.

const maskFixture = `{
  "Env": [
    "API_TOKEN=canary",
    "LOG_LEVEL=info"
  ]
}`

// maskableSource stands in for the inspect source: Mask hides the value of
// every "NAME=VALUE" string whose name the matcher calls sensitive.
type maskableSource struct {
	fakeSource
	maskErr error
}

var envEntry = regexp.MustCompile(`"([A-Za-z_]+)=([^"]*)"`)

func (s maskableSource) Mask(raw []byte, matcher *secretenv.Matcher) ([]byte, int, error) {
	if s.maskErr != nil {
		return nil, 0, s.maskErr
	}
	count := 0
	out := envEntry.ReplaceAllStringFunc(string(raw), func(entry string) string {
		parts := envEntry.FindStringSubmatch(entry)
		if !matcher.Sensitive(parts[1]) {
			return entry
		}
		count++
		return `"` + parts[1] + `=********"`
	})
	return []byte(out), count, nil
}

// followableMaskable adds a follow capability, so the follow path can be driven.
type followableMaskable struct{ maskableSource }

func (followableMaskable) FollowInterval() time.Duration { return time.Millisecond }

func newMaskable(content string) maskableSource {
	return maskableSource{fakeSource: fakeSource{name: "inspect · api", kind: viewerpkg.KindJSON, content: content}}
}

// shown is what the pane displays, escape sequences stripped.
func shown(m Model) string {
	if m.display == displayTree {
		return ansi.Strip(m.tree.View())
	}
	return ansi.Strip(m.textViewport.View())
}

// shownText is the text display's content whatever the current display is.
func shownText(m Model) string {
	return ansi.Strip(m.textViewport.View())
}

// plainLines is the text the pane is built from, line by line.
func plainLines(m Model) string {
	var lines []string
	for _, line := range m.lines {
		lines = append(lines, line.Plain)
	}
	return strings.Join(lines, "\n")
}

func envHeader(m Model, key string) (string, bool) {
	for _, info := range m.GetHeaderInfo("ctx") {
		if info.Key == key {
			return info.Value, true
		}
	}
	return "", false
}

func shortcutDescription(shortcuts shortcut.Shortcuts, key string) (string, bool) {
	for _, s := range shortcuts {
		if s.Key == key {
			return s.Description, true
		}
	}
	return "", false
}

// inTextDisplay switches the opening tree display to the text one, where `/` works.
func inTextDisplay(t *testing.T, m Model) Model {
	t.Helper()
	return feed(t, m, testutil.Key("f"))
}

func TestAMaskableDocumentOpensMasked(t *testing.T) {
	m := open(t, newMaskable(maskFixture))

	for name, view := range map[string]string{"tree": shown(m), "text": shownText(inTextDisplay(t, m))} {
		if strings.Contains(view, "canary") {
			t.Errorf("the %s display shows the secret:\n%s", name, view)
		}
	}
	text := shownText(inTextDisplay(t, m))
	if !strings.Contains(text, "API_TOKEN=********") {
		t.Errorf("API_TOKEN is not shown with its mask:\n%s", text)
	}
	if !strings.Contains(text, "LOG_LEVEL=info") {
		t.Errorf("LOG_LEVEL=info is not shown in clear:\n%s", text)
	}
}

// The mask has one length whatever the secret's: its length would be a hint.
func TestTheMaskDoesNotDependOnTheSecretLength(t *testing.T) {
	short := open(t, newMaskable(strings.Replace(maskFixture, "canary", "c", 1)))
	long := open(t, newMaskable(strings.Replace(maskFixture, "canary", strings.Repeat("c", 200), 1)))

	if plainLines(short) != plainLines(long) {
		t.Errorf("the masked documents differ with the secret's length:\n%s\n---\n%s", plainLines(short), plainLines(long))
	}
}

func TestOpeningMaskedSaysHowManyAreHidden(t *testing.T) {
	m := open(t, newMaskable(maskFixture))

	text := m.footer.Text()
	for _, want := range []string{"1 environment value masked", "can miss secrets", "m reveals"} {
		if !strings.Contains(text, want) {
			t.Errorf("footer %q does not say %q", text, want)
		}
	}
	if m.footer.Level() != components.LevelInfo {
		t.Errorf("footer level = %v, want Info (Rule 128)", m.footer.Level())
	}
}

func TestTheFooterCountsThePlural(t *testing.T) {
	content := strings.Replace(maskFixture, `"LOG_LEVEL=info"`, `"DB_PASSWORD=x", "LOG_LEVEL=info"`, 1)
	m := open(t, newMaskable(content))

	if !strings.Contains(m.footer.Text(), "2 environment values masked") {
		t.Errorf("footer = %q, want a count of 2", m.footer.Text())
	}
}

func TestNothingToMaskStillWarnsThatMaskingCanMissSecrets(t *testing.T) {
	m := open(t, newMaskable(`{"Env":["LOG_LEVEL=info"]}`))

	text := m.footer.Text()
	if !strings.Contains(text, "0 environment values masked") || !strings.Contains(text, "can miss secrets") {
		t.Errorf("footer = %q", text)
	}
}

func TestMRevealsAndMasksAgain(t *testing.T) {
	m := inTextDisplay(t, open(t, newMaskable(maskFixture)))

	if got, _ := envHeader(m, "Env"); got != "masked" {
		t.Errorf("header Env = %q while masked, want masked", got)
	}

	m = feed(t, m, testutil.Key("m"))
	if !strings.Contains(shownText(m), "API_TOKEN=canary") {
		t.Errorf("m did not reveal the value:\n%s", shownText(m))
	}
	if got, _ := envHeader(m, "Env"); got != "revealed" {
		t.Errorf("header Env = %q after m, want revealed", got)
	}
	if !strings.Contains(m.footer.Text(), "revealed") {
		t.Errorf("footer = %q, want it to say the values are revealed", m.footer.Text())
	}

	m = feed(t, m, testutil.Key("m"))
	if strings.Contains(shownText(m), "canary") {
		t.Errorf("a second m did not mask again:\n%s", shownText(m))
	}
	if got, _ := envHeader(m, "Env"); got != "masked" {
		t.Errorf("header Env = %q after a second m, want masked", got)
	}
}

func TestMWorksInTheTreeToo(t *testing.T) {
	m := open(t, newMaskable(maskFixture))
	if m.display != displayTree {
		t.Fatalf("an inspect opens on %v, want the tree", m.display)
	}
	if strings.Contains(shown(m), "canary") {
		t.Fatalf("the tree shows the secret:\n%s", shown(m))
	}

	m = feed(t, m, testutil.Key("m"))
	if !strings.Contains(shown(m), "canary") {
		t.Errorf("m did not reveal the value in the tree:\n%s", shown(m))
	}
}

// The state belongs to the view that was opened, never to the next one.
func TestReopeningAfterRevealingIsMaskedAgain(t *testing.T) {
	source := newMaskable(maskFixture)
	first := feed(t, open(t, source), testutil.Key("m"))
	if first.revealed != true {
		t.Fatal("the first view is not revealed")
	}

	second := inTextDisplay(t, open(t, source))
	if strings.Contains(shownText(second), "canary") {
		t.Errorf("a reopened inspect shows the secret:\n%s", shownText(second))
	}
	if got, _ := envHeader(second, "Env"); got != "masked" {
		t.Errorf("header Env = %q on a reopened inspect, want masked", got)
	}
}

// The state is in the model only: nothing is written under HOME, so no
// configuration or cache file can carry it.
func TestRevealingWritesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	m := open(t, newMaskable(maskFixture))
	m = feed(t, m, testutil.Key("m"), testutil.Key("m"), testutil.Key("m"))
	m = feed(t, m, testutil.Key("ctrl+r"))
	_ = m

	var files []string
	err := filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("the viewer wrote %v under HOME", files)
	}
}

func TestYCopiesExactlyWhatIsShown(t *testing.T) {
	var copied string
	orig := writeClipboard
	writeClipboard = func(text string) error { copied = text; return nil }
	t.Cleanup(func() { writeClipboard = orig })

	m := inTextDisplay(t, open(t, newMaskable(maskFixture)))
	m, cmd := step(t, m, testutil.Key(keymap.Copy))
	if cmd == nil {
		t.Fatal("Y issued no command")
	}
	m = feed(t, m, cmd())

	if copied != plainLines(m) {
		t.Errorf("the clipboard holds\n%q\nbut the pane shows\n%q", copied, plainLines(m))
	}
	if strings.Contains(copied, "canary") || !strings.Contains(copied, "API_TOKEN=********") || !strings.Contains(copied, "LOG_LEVEL=info") {
		t.Errorf("copied text is not the masked document:\n%s", copied)
	}

	// Revealed, what is shown is the real text, and so is what is copied.
	m = feed(t, m, testutil.Key("m"))
	_, cmd = step(t, m, testutil.Key(keymap.Copy))
	feed(t, m, cmd())
	if copied != plainLines(m) || !strings.Contains(copied, "API_TOKEN=canary") {
		t.Errorf("revealed: the clipboard holds\n%q\nbut the pane shows\n%q", copied, plainLines(m))
	}
}

func TestSearchRunsOnTheShownText(t *testing.T) {
	m := inTextDisplay(t, open(t, newMaskable(maskFixture)))
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("canary")...)
	m = feed(t, m, testutil.Key("enter"))

	if m.matchedLines != 0 {
		t.Errorf("a search for the secret matched %d lines while masked", m.matchedLines)
	}
	if !strings.Contains(shownText(m), "No line matches canary") {
		t.Errorf("the pane does not say nothing matched:\n%s", shownText(m))
	}

	// The same search finds it once revealed: the filter follows the display.
	m = feed(t, m, testutil.Key("m"))
	if m.matchedLines != 1 {
		t.Errorf("after m the search matched %d lines, want 1", m.matchedLines)
	}
}

func TestAReloadKeepsTheState(t *testing.T) {
	for _, revealed := range []bool{false, true} {
		t.Run(fmt.Sprintf("revealed=%v", revealed), func(t *testing.T) {
			m := inTextDisplay(t, open(t, newMaskable(maskFixture)))
			if revealed {
				m = feed(t, m, testutil.Key("m"))
			}

			m, cmd := step(t, m, testutil.Key("ctrl+r"))
			m = feed(t, m, resolve(t, cmd)...)

			if m.revealed != revealed {
				t.Errorf("revealed = %v after a reload, want %v", m.revealed, revealed)
			}
			if got := strings.Contains(shownText(m), "canary"); got != revealed {
				t.Errorf("secret shown = %v after a reload, want %v:\n%s", got, revealed, shownText(m))
			}
		})
	}
}

func TestAFollowTickKeepsTheState(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	for _, revealed := range []bool{false, true} {
		t.Run(fmt.Sprintf("revealed=%v", revealed), func(t *testing.T) {
			m := inTextDisplay(t, open(t, followableMaskable{newMaskable(maskFixture)}))
			if revealed {
				m = feed(t, m, testutil.Key("m"))
			}

			m, cmd := step(t, m, testutil.Key(keymap.Fetch))
			m = feed(t, m, resolve(t, cmd)...)

			if m.revealed != revealed {
				t.Errorf("revealed = %v after following, want %v", m.revealed, revealed)
			}
			if got := strings.Contains(shownText(m), "canary"); got != revealed {
				t.Errorf("secret shown = %v after following, want %v", got, revealed)
			}
		})
	}
}

// Toggling is a different reading of the same document: the reader stays where
// they were, and nothing is fetched again.
func TestTogglingKeepsThePositionAndFetchesNothing(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	var env []string
	for i := 0; i < 60; i++ {
		env = append(env, fmt.Sprintf(`    "VAR_%02d=v"`, i))
	}
	env = append(env, `    "API_TOKEN=canary"`)
	content := "{\n  \"Env\": [\n" + strings.Join(env, ",\n") + "\n  ]\n}"
	m := inTextDisplay(t, open(t, newMaskable(content)))
	m = feed(t, m, testutil.Key("pgdown"), testutil.Key("down"), testutil.Key("down"))
	before := m.textViewport.YOffset()
	if before == 0 {
		t.Fatal("the fixture does not scroll")
	}

	m, cmd := step(t, m, testutil.Key("m"))

	if m.textViewport.YOffset() != before {
		t.Errorf("YOffset = %d after m, was %d", m.textViewport.YOffset(), before)
	}
	if m.loading {
		t.Error("m reloaded the document; it has the text already")
	}
	for _, msg := range resolve(t, cmd) {
		if _, ok := msg.(DocumentLoadedMsg); ok {
			t.Error("m fetched the source again")
		}
	}
}

func TestTheConfiguredListAddsToWhatIsMasked(t *testing.T) {
	cfg := config.Default()
	cfg.Containers.SensitiveEnv = []string{"^LOG_LEVEL$"}
	m := NewWithSource(cfg, newMaskable(maskFixture))
	m = feed(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})
	m = feed(t, m, resolve(t, m.Init())...)
	m = inTextDisplay(t, m)

	if strings.Contains(shownText(m), "info") || strings.Contains(shownText(m), "canary") {
		t.Errorf("the configured pattern did not apply:\n%s", shownText(m))
	}
	if !strings.Contains(m.footer.Text(), "2 environment values masked") {
		t.Errorf("footer = %q", m.footer.Text())
	}
}

func TestAViewerWithoutAConfigMasksWithTheDefaults(t *testing.T) {
	m := NewWithSource(nil, newMaskable(maskFixture))
	m = feed(t, m, tea.WindowSizeMsg{Width: 80, Height: 20})
	m = feed(t, m, resolve(t, m.Init())...)

	if strings.Contains(shown(m), "canary") {
		t.Errorf("a nil config shows the secret:\n%s", shown(m))
	}
}

// A source that cannot mask must not fall back to showing: the load fails.
func TestAMaskFailureShowsNothing(t *testing.T) {
	src := newMaskable(maskFixture)
	src.maskErr = errors.New("not JSON")
	m := open(t, src)

	if m.hasDocument() {
		t.Errorf("the document is shown although it could not be masked:\n%s", shown(m))
	}
	if m.footer.Level() != components.LevelError {
		t.Errorf("footer level = %v (%q), want Error", m.footer.Level(), m.footer.Text())
	}
	if strings.Contains(m.footer.Text(), "not JSON") {
		t.Errorf("the footer carries the raw error: %q", m.footer.Text())
	}
}

func TestMIsOfferedOnlyForAMaskableSource(t *testing.T) {
	plain := jsonModel(t)
	if testutil.HasShortcut(plain.GetShortcuts(), "m") {
		t.Error("m is offered for a document with nothing to mask")
	}
	if _, ok := envHeader(plain, "Env"); ok {
		t.Error("the header shows an Env state for a document with nothing to mask")
	}
	// And the key does nothing there.
	after := feed(t, plain, testutil.Key("m"))
	if after.revealed || after.footer.IsSet() {
		t.Error("m did something on a document with nothing to mask")
	}

	masked := open(t, newMaskable(maskFixture))
	if !testutil.ShortcutEnabled(masked.GetShortcuts(), "m") {
		t.Fatal("m is missing or greyed for a maskable source")
	}
	if got, _ := shortcutDescription(masked.GetShortcuts(), "m"); got != "Reveal values" {
		t.Errorf("m reads %q while masked, want Reveal values", got)
	}
	revealed := feed(t, masked, testutil.Key("m"))
	if got, _ := shortcutDescription(revealed.GetShortcuts(), "m"); got != "Mask values" {
		t.Errorf("m reads %q while revealed, want Mask values", got)
	}
}

// The set of keys does not change between the two states (Rule 130).
func TestMaskingDoesNotReshapeTheColumn(t *testing.T) {
	masked := open(t, newMaskable(maskFixture))
	revealed := feed(t, masked, testutil.Key("m"))

	a := strings.Join(testutil.ShortcutKeys(masked.GetShortcuts()), " ")
	b := strings.Join(testutil.ShortcutKeys(revealed.GetShortcuts()), " ")
	if a != b {
		t.Errorf("masked advertises %q and revealed %q", a, b)
	}
}

func TestTheHelpDocumentsM(t *testing.T) {
	help := jsonModel(t).GetHelpContent()

	var bound bool
	for _, binding := range help.KeyBindings {
		if binding.Key == "m" {
			bound = strings.Contains(binding.Description, "mask") || strings.Contains(binding.Description, "Mask")
		}
	}
	if !bound {
		t.Error("the viewer's help has no entry for m that talks about masking")
	}
	var section bool
	for _, s := range help.Sections {
		if strings.Contains(s.Body, "can miss") {
			section = true
		}
	}
	if !section {
		t.Error("the viewer's help does not say that masking can miss secrets")
	}
}

// The search bar owns the keyboard: an m typed there is a character.
func TestMTypedInTheSearchBarIsNotTheToggle(t *testing.T) {
	m := inTextDisplay(t, open(t, newMaskable(maskFixture)))
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("mask")...)

	if m.revealed {
		t.Error("typing m in the search bar revealed the values")
	}
}
