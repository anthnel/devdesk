package security

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
)

// DEVDESK-754 (SC1, step 4): enter on a group opens a level of its own, the
// findings of the group; enter there opens one finding, the same detail as from
// the flat table; esc walks back one level at a time. Y copies the group's command
// and never runs it.

func memberIDs(findings []scan.Finding) []string {
	out := ids(findings)
	slices.Sort(out)
	return out
}

// openedGroup presses enter on the group of subject in the grouped list.
func openedGroup(t *testing.T, m Model, subject string) Model {
	t.Helper()
	m = cursorOnGroup(t, m, subject)
	m = feed(t, m, testutil.Key("enter"))
	if m.state != StateGroup {
		t.Fatalf("enter on %s did not open its group: state %v", subject, m.state)
	}
	return m
}

// cursorOnMember moves the cursor of the group's table down to the finding id.
func cursorOnMember(t *testing.T, m Model, id string) Model {
	t.Helper()
	for range 12 {
		if f, ok := m.groupTable.Selected(); ok && f.ID == id {
			return m
		}
		m = feed(t, m, testutil.Key("down"))
	}
	t.Fatalf("the cursor of the group never reached %s", id)
	return m
}

// ── The level ────────────────────────────────────────────────────────────────

func TestEnterOnAGroupOpensTheListOfItsFindings(t *testing.T) {
	m := openedGroup(t, groupedModel(t, 160), "openssl")

	if got, want := memberIDs(m.groupTable.Visible()), []string{"CVE-1", "CVE-2", "CVE-3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the group lists %v, want %v", got, want)
	}
	if m.openGroup == nil || m.openGroup.Subject != "openssl" || m.openGroup.Installed != "1.1.1k" {
		t.Errorf("openGroup = %+v, want the key of openssl 1.1.1k", m.openGroup)
	}
	header := firstLine(ansi.Strip(m.Render()))
	for _, title := range []string{"Severity", "ID", "Title"} {
		if !strings.Contains(header, title) {
			t.Errorf("the group's header %q lacks %q: it uses the findings' columns", header, title)
		}
	}
	if strings.Contains(header, "Package") {
		t.Errorf("the group's header %q is the grouped list's", header)
	}
}

// The count of a group is what one move closes: the level lists all of it, even
// the members a filter hides in the findings table.
func TestAGroupListsItsHiddenMembersToo(t *testing.T) {
	m := feed(t, groupedModel(t, 160), testutil.Key("c"))
	if want := []string{"openssl"}; !reflect.DeepEqual(groupSubjects(m), want) {
		t.Fatalf("setup: groups = %v", groupSubjects(m))
	}
	m = openedGroup(t, m, "openssl")
	if got := len(m.groupTable.Visible()); got != 3 {
		t.Errorf("the group lists %d findings under a CRITICAL filter, want its 3", got)
	}
}

func TestEnterIsGreyedWhenThereIsNoGroupToOpen(t *testing.T) {
	m := groupedModel(t, 160)
	m = feed(t, m, testutil.Key("/"))
	m = feed(t, m, testutil.Type("zzzz")...)
	m = feed(t, m, testutil.Key("enter"))
	if !testutil.ShortcutDisabled(m.GetShortcuts(), "enter") {
		t.Error("enter is offered with no group to open")
	}
	next, _ := step(t, m, testutil.Key("enter"))
	if next.state != StateResults {
		t.Errorf("enter changed the state to %v", next.state)
	}
}

func TestTheGroupLevelHasFourKeys(t *testing.T) {
	m := openedGroup(t, groupedModel(t, 160), "openssl")
	got := m.GetShortcuts()
	if want := []string{"esc", "enter", keymap.Copy, "?"}; !slices.Equal(testutil.ShortcutKeys(got), want) {
		t.Fatalf("the column is %v, want %v", testutil.ShortcutKeys(got), want)
	}
	words := map[string]string{"esc": "Back", "enter": "Details", keymap.Copy: "Copy cmd", "?": "Help"}
	for _, s := range got {
		if s.Description != words[s.Key] {
			t.Errorf("%s reads %q, want %q", s.Key, s.Description, words[s.Key])
		}
		if len([]rune(s.Description)) > 10 {
			t.Errorf("%s: %q is over ten cells, the width of the column at 80 columns", s.Key, s.Description)
		}
	}
	if !testutil.ShortcutEnabled(got, keymap.Copy) {
		t.Error("Y is greyed on a group with a command")
	}
}

// Same screen, same keys: Y is greyed for a group that has no command, with the
// reason that says which case it is.
func TestCopyIsGreyedWhenTheGroupHasNoCommand(t *testing.T) {
	cases := []struct {
		subject string
		reason  string
	}{
		{"zlib", reasonGroupHasNoFix},
		{"libfoo", reasonNoGroupCommand},
	}
	for _, tc := range cases {
		t.Run(tc.subject, func(t *testing.T) {
			testutil.FastTimers(t, &components.FooterMsgDuration)
			written := stubClipboard(t, nil)
			m := openedGroup(t, groupedModel(t, 160), tc.subject)
			if !testutil.HasShortcut(m.GetShortcuts(), keymap.Copy) || !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Copy) {
				t.Fatalf("Y must be advertised and greyed: %v", m.GetShortcuts())
			}
			if a := m.canCopyGroupCommand(); a.Enabled() || a.Reason != tc.reason {
				t.Errorf("canCopyGroupCommand = %+v, want %q", a, tc.reason)
			}
			m, cmd := step(t, m, testutil.Key(keymap.Copy))
			drained := testutil.Msgs(cmd)
			if m.footer.Text() != tc.reason || m.footer.Level() != components.LevelWarning {
				t.Errorf("footer = %q (level %v), want the Warn %q", m.footer.Text(), m.footer.Level(), tc.reason)
			}
			if len(*written) != 0 {
				t.Errorf("the clipboard got %q for a group with no command", *written)
			}
			for _, msg := range drained {
				if _, ok := msg.(FixCommandCopiedMsg); ok {
					t.Error("Y produced a copy for a group with no command")
				}
			}
		})
	}
}

// A command that is not plain words is never copied (scan.PlainWords).
func TestCopyRefusesACommandThatIsNotPlainWords(t *testing.T) {
	findings := []scan.Finding{cve("CVE-1", "~evil", "npm", "1.0.0", "1.0.1", scan.SeverityHigh)}
	written := stubClipboard(t, nil)
	m := feed(t, groupModelOf(t, findings, 160), testutil.Key(groupToggleKey))
	if !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Copy) {
		t.Error("Y is offered for a group whose command is not plain words")
	}
	if a := m.canCopyGroupCommand(); a.Enabled() || a.Reason != reasonNoGroupCommand {
		t.Errorf("canCopyGroupCommand = %+v, want %q", a, reasonNoGroupCommand)
	}
	m, cmd := step(t, m, testutil.Key(keymap.Copy))
	testutil.FastTimers(t, &components.FooterMsgDuration)
	testutil.Msgs(cmd)
	if len(*written) != 0 || m.footer.Text() != reasonNoGroupCommand {
		t.Errorf("clipboard %q, footer %q", *written, m.footer.Text())
	}
}

func TestTheGroupLevelHasATitleThatNamesTheGroup(t *testing.T) {
	m := openedGroup(t, groupedModel(t, 160), "openssl")
	title := ansi.Strip(m.GetTitle())
	for _, want := range []string{"Security Scanner", "openssl", "1.1.1k -> 1.1.1w"} {
		if !strings.Contains(title, want) {
			t.Errorf("title %q lacks %q", title, want)
		}
	}
}

func TestTheHeaderKeepsItsFiguresInTheGroupLevel(t *testing.T) {
	results := groupedModel(t, 160)
	level := openedGroup(t, results, "openssl")
	for _, key := range []string{"Context", "Findings", "Fixable"} {
		if got, want := headerValue(t, level, key), headerValue(t, results, key); got != want {
			t.Errorf("%s = %q in the group level, %q in the list", key, got, want)
		}
	}
}

func TestTheGroupLevelHasNoEditModeAndDropsAPaste(t *testing.T) {
	m := openedGroup(t, groupedModel(t, 160), "openssl")
	if m.InEditMode() {
		t.Error("the group level claims the keyboard")
	}
	before := ansi.Strip(m.Render())
	m = feed(t, m, testutil.Paste("some pasted text"))
	if m.state != StateGroup || ansi.Strip(m.Render()) != before || m.footer.Text() != "" {
		t.Error("a paste changed the group level (Rule 111)")
	}
}

// What belongs to the list and the tabs does nothing one level down.
func TestKeysOfTheListAreInertInTheGroupLevel(t *testing.T) {
	for _, key := range []string{"tab", "shift+tab", groupToggleKey, "c", "h", "m", "l", "p", "/", ".", "space", writeRemediationKey, keymap.Scan, keymap.Exclude} {
		t.Run(key, func(t *testing.T) {
			m := openedGroup(t, groupedModel(t, 160), "openssl")
			tab, tokens := m.activeTab, activeTokens(m)
			next, _ := step(t, m, testutil.Key(key))
			if next.state != StateGroup || next.activeTab != tab || next.InEditMode() || next.confirmModal != nil || next.scanAllModal != nil {
				t.Errorf("%s acted in the group level: state %v, tab %d", key, next.state, next.activeTab)
			}
			if !reflect.DeepEqual(activeTokens(next), tokens) || !next.isGrouped() {
				t.Errorf("%s changed the list behind the group", key)
			}
		})
	}
}

func TestTheGroupLevelFitsAtEveryWidth(t *testing.T) {
	for _, width := range []int{80, 120, 175} {
		t.Run(fmt.Sprintf("width %d", width), func(t *testing.T) {
			m := openedGroup(t, groupedModel(t, width), "openssl")
			if got, want := m.groupTable.RenderedWidth(), width-2; got != want {
				t.Errorf("RenderedWidth = %d, want %d (Rule 116)", got, want)
			}
			for i, line := range strings.Split(m.Render(), "\n") {
				if w := ansi.StringWidth(line); w > width-2 {
					t.Errorf("line %d is %d cells, the viewport holds %d", i, w, width-2)
				}
			}
			footer := strings.Split(m.RenderFooter(width), "\n")
			if len(footer) != m.GetFooterHeight() {
				t.Errorf("the footer is %d lines, GetFooterHeight says %d", len(footer), m.GetFooterHeight())
			}
			// A resize in the level is followed.
			m = feed(t, m, testutil.Resize(80, 30))
			if got, want := m.groupTable.RenderedWidth(), 78; got != want {
				t.Errorf("after a resize to 80: RenderedWidth = %d, want %d", got, want)
			}
		})
	}
}

// ── AC3: the whole path ──────────────────────────────────────────────────────

func TestFromTheGroupedListToOneFindingAndBack(t *testing.T) {
	m := groupedModel(t, 160)
	m = feed(t, m, testutil.Key("c"), testutil.Key("c")) // a filter state to find again
	tokens := activeTokens(m)
	m = openedGroup(t, m, "openssl")
	m = cursorOnMember(t, m, "CVE-2")

	m = feed(t, m, testutil.Key("enter"))
	if m.state != StateDetails || m.selectedFinding == nil || m.selectedFinding.ID != "CVE-2" {
		t.Fatalf("enter in the group did not open CVE-2: state %v", m.state)
	}
	if m.detailsParent != StateGroup {
		t.Errorf("detailsParent = %v, want the group level", m.detailsParent)
	}

	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateGroup || m.openGroup == nil {
		t.Fatalf("esc in the detail went to state %v, want the group", m.state)
	}
	if f, ok := m.groupTable.Selected(); !ok || f.ID != "CVE-2" {
		t.Errorf("back in the group the cursor is on %+v, want the finding that was open", f)
	}

	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateResults || !m.isGrouped() || m.openGroup != nil {
		t.Fatalf("esc in the group went to state %v (grouped %v), want the grouped list", m.state, m.isGrouped())
	}
	if g := selectedGroup(t, m); g.Subject != "openssl" {
		t.Errorf("the grouped list is on %q, want the group that was open", g.Subject)
	}
	if !reflect.DeepEqual(activeTokens(m), tokens) {
		t.Errorf("tokens = %v, were %v", activeTokens(m), tokens)
	}
}

// The flat path is what it was, and its parent is the results.
func TestTheFlatDetailStillReturnsToTheResults(t *testing.T) {
	m := feed(t, groupModel(t, 160), testutil.Key("enter"))
	if m.state != StateDetails {
		t.Fatalf("enter did not open the detail: %v", m.state)
	}
	if m.detailsParent != StateResults {
		t.Errorf("detailsParent = %v, want StateResults", m.detailsParent)
	}
	m = feed(t, m, testutil.Key("esc"))
	if m.state != StateResults || m.isGrouped() {
		t.Errorf("esc went to state %v (grouped %v)", m.state, m.isGrouped())
	}
}

// ── The detail is the same detail ────────────────────────────────────────────

// detailThroughGroup opens CVE-4 by the grouped list; detailFlat by the table.
func detailThroughGroup(t *testing.T) Model {
	t.Helper()
	m := openedGroup(t, groupedModel(t, 160), "golang.org/x/net")
	return feed(t, m, testutil.Key("enter"))
}

func detailFlat(t *testing.T) Model {
	t.Helper()
	m := cursorOnFinding(t, groupModel(t, 160), "CVE-4")
	return feed(t, m, testutil.Key("enter"))
}

func TestTheDetailOpenedFromAGroupIsTheFlatDetail(t *testing.T) {
	grouped, flat := detailThroughGroup(t), detailFlat(t)
	if grouped.state != StateDetails || flat.state != StateDetails {
		t.Fatalf("states: grouped %v, flat %v", grouped.state, flat.state)
	}
	if got, want := plainLines(grouped), plainLines(flat); !slices.Equal(got, want) {
		t.Errorf("the detail differs:\ngrouped %q\nflat    %q", got, want)
	}
	// Including what 753 shows for a vulnerability: the move, never a diff.
	if content := ansi.Strip(grouped.buildDetailsContent()); !strings.Contains(content, "->") {
		t.Errorf("the detail has no Change section:\n%s", content)
	}
	if !slices.Equal(testutil.ShortcutKeys(grouped.GetShortcuts()), testutil.ShortcutKeys(flat.GetShortcuts())) {
		t.Fatalf("the detail's column differs: %v and %v", testutil.ShortcutKeys(grouped.GetShortcuts()), testutil.ShortcutKeys(flat.GetShortcuts()))
	}
	for i, s := range grouped.GetShortcuts() {
		if f := flat.GetShortcuts()[i]; s.Disabled != f.Disabled || s.Description != f.Description {
			t.Errorf("%s: grouped %+v, flat %+v", s.Key, s, f)
		}
	}
	// The change preview of 753 is that of the finding on screen, not of the path.
	if !reflect.DeepEqual(grouped.changePreview, flat.changePreview) {
		t.Errorf("changePreview: grouped %+v, flat %+v", grouped.changePreview, flat.changePreview)
	}
}

func TestTheKeysOf751WorkFromAGroupsDetail(t *testing.T) {
	t.Run("enter opens the file in the viewer", func(t *testing.T) {
		m := detailThroughGroup(t)
		_, cmd := step(t, m, testutil.Key("enter"))
		if _, ok := testutil.MsgOf[uiviewer.OpenRequestMsg](cmd); !ok {
			t.Error("enter in the detail asked the viewer for no document")
		}
	})
	t.Run("Y copies the command under Run, whole", func(t *testing.T) {
		written := stubClipboard(t, nil)
		m := detailThroughGroup(t)
		_, cmd := step(t, m, testutil.Key(keymap.Copy))
		if _, ok := testutil.MsgOf[FixCommandCopiedMsg](cmd); !ok {
			t.Fatal("Y produced no copy")
		}
		if want := []string{"go get golang.org/x/net@v0.23.0"}; !reflect.DeepEqual(*written, want) {
			t.Errorf("clipboard = %q, want %q", *written, want)
		}
	})
	t.Run("ctrl+o and X are greyed with their reason, as on the flat path", func(t *testing.T) {
		grouped, flat := detailThroughGroup(t), detailFlat(t)
		for _, key := range []string{writeRemediationKey, keymap.Exclude} {
			if !testutil.ShortcutDisabled(grouped.GetShortcuts(), key) || !testutil.ShortcutDisabled(flat.GetShortcuts(), key) {
				t.Fatalf("%s is expected greyed on a CVE", key)
			}
			g, _ := step(t, grouped, testutil.Key(key))
			f, _ := step(t, flat, testutil.Key(key))
			if g.footer.Text() == "" || g.footer.Text() != f.footer.Text() || g.footer.Level() != components.LevelWarning {
				t.Errorf("%s: grouped %q (level %v), flat %q", key, g.footer.Text(), g.footer.Level(), f.footer.Text())
			}
			if g.state != StateDetails || g.confirmModal != nil {
				t.Errorf("%s acted: state %v", key, g.state)
			}
		}
	})
}

// ── Y copies the group's command ─────────────────────────────────────────────

func TestYInTheGroupedListCopiesTheGroupsCommandWholeAndRunsNothing(t *testing.T) {
	for name, open := range map[string]func(*testing.T) Model{
		"the grouped list": func(t *testing.T) Model { return cursorOnGroup(t, groupedModel(t, 160), "openssl") },
		"the group level":  func(t *testing.T) Model { return openedGroup(t, groupedModel(t, 160), "openssl") },
	} {
		t.Run(name, func(t *testing.T) {
			testutil.FastTimers(t, &components.FooterMsgDuration)
			written := stubClipboard(t, nil)
			m := open(t)
			if !testutil.ShortcutEnabled(m.GetShortcuts(), keymap.Copy) {
				t.Fatal("Y is greyed on a group with a command")
			}
			m, cmd := step(t, m, testutil.Key(keymap.Copy))
			if len(*written) != 0 {
				t.Fatalf("Update wrote %q to the clipboard: the write belongs in the Cmd (Rule 110)", *written)
			}
			msg, ok := testutil.MsgOf[FixCommandCopiedMsg](cmd)
			if !ok {
				t.Fatal("Y produced no FixCommandCopiedMsg")
			}
			if want := []string{"apk upgrade openssl"}; !reflect.DeepEqual(*written, want) {
				t.Errorf("clipboard = %q, want %q", *written, want)
			}
			if msg.Command != "apk upgrade openssl" || msg.Err != nil {
				t.Errorf("FixCommandCopiedMsg = %+v", msg)
			}
			m = feed(t, m, msg)
			if m.footer.Text() != "Command copied to the clipboard" || m.footer.Level() != components.LevelInfo {
				t.Errorf("footer = %q (level %v), want the Info confirmation", m.footer.Text(), m.footer.Level())
			}
		})
	}
}

func TestAFailedCopyOfAGroupCommandIsAnErrorWithoutItsCause(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	stubClipboard(t, fmt.Errorf("no clipboard tool: /usr/bin/xclip not found"))
	m := cursorOnGroup(t, groupedModel(t, 160), "openssl")
	m, cmd := step(t, m, testutil.Key(keymap.Copy))
	msg, ok := testutil.MsgOf[FixCommandCopiedMsg](cmd)
	if !ok {
		t.Fatal("Y produced no FixCommandCopiedMsg")
	}
	m = feed(t, m, msg)
	if m.footer.Level() != components.LevelError || strings.Contains(m.footer.Text(), "xclip") {
		t.Errorf("footer = %q (level %v)", m.footer.Text(), m.footer.Level())
	}
}

// In the flat list Y has no group to copy from: it is greyed, and says where it
// works. The key is in the column all the same (Rule 130).
func TestYIsGreyedInTheFlatListWithItsReason(t *testing.T) {
	testutil.FastTimers(t, &components.FooterMsgDuration)
	written := stubClipboard(t, nil)
	m := groupModel(t, 160)
	if !testutil.HasShortcut(m.GetShortcuts(), keymap.Copy) || !testutil.ShortcutDisabled(m.GetShortcuts(), keymap.Copy) {
		t.Fatal("Y must be advertised and greyed in the flat list")
	}
	m, cmd := step(t, m, testutil.Key(keymap.Copy))
	testutil.Msgs(cmd)
	if m.footer.Text() != reasonCopyNeedsGroup || m.footer.Level() != components.LevelWarning {
		t.Errorf("footer = %q (level %v), want the Warn %q", m.footer.Text(), m.footer.Level(), reasonCopyNeedsGroup)
	}
	if len(*written) != 0 {
		t.Errorf("the clipboard got %q from the flat list", *written)
	}
}
