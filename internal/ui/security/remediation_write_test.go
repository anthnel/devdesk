package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/dockerfile"
	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	uiviewer "github.com/anthnel/devdesk/internal/ui/viewer"
)

// ── Fixtures ─────────────────────────────────────────────────────────────────

// repoModel opens the Remediation tab on a real directory holding one
// Dockerfile, with entries built by parsing it — so the byte ranges are the
// parser's own, which is what a write is made of.
//
// candidates maps the stage's position to the references it could move to, and
// scanned lists the references that have a result. Nothing reaches a registry or
// a scanner: the Cmds the tab returns are dropped unless a test runs one.
func repoModel(t *testing.T, content string, candidates map[int][]string, scanned ...string) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var entries []remediation.Entry
	for i, st := range dockerfile.Parse([]byte(content)).Stages {
		if st.Kind != dockerfile.KindImage {
			continue
		}
		entries = append(entries, remediation.Entry{
			File: "Dockerfile", Stage: st, StageLabel: st.Name, Image: st.Image, Candidates: candidates[i],
		})
	}
	results := map[string]cache.RemediationEntry{}
	for _, ref := range scanned {
		results[ref] = scannedAt(0, 1)
	}

	result := &scan.Result{Target: dir, TargetType: scan.TargetDirectory, Findings: findingFixtures()}
	m := feed(t, NewWithPreloadedResult(testConfig(), nil, result), tea.WindowSizeMsg{Width: 160, Height: 30})
	for range TabRemediation {
		m = feed(t, m, testutil.Key("tab"))
	}
	return feed(t, m, RemediationDiscoveredMsg{Target: dir, Entries: entries, Results: results}), dir
}

// cursorOn moves the cursor to the row holding ref.
func cursorOn(t *testing.T, m Model, ref string) Model {
	t.Helper()
	m = feed(t, m, testutil.Key("home"))
	for range 20 {
		if row, ok := m.remediation.table.Selected(); ok && row.Ref == ref && !row.Current {
			return m
		}
		m = feed(t, m, testutil.Key("down"))
	}
	t.Fatalf("no candidate row for %q", ref)
	return m
}

func choose(t *testing.T, m Model, ref string) Model {
	t.Helper()
	return feed(t, cursorOn(t, m, ref), testutil.Key(" "))
}

func fileContent(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// run executes a Cmd and returns its message.
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command to run")
	}
	return cmd()
}

const twoStageFile = "FROM golang:1.21 AS build\nRUN go build\nFROM alpine:3.18 AS final\n"

// ── Choosing ─────────────────────────────────────────────────────────────────

func TestOnlyAScannedCandidateCanBeChosen(t *testing.T) {
	m, _ := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23", "golang:1.22"}}, "golang:1.23")

	// The current image is not a candidate.
	if a := m.canSelectCandidate(); a.Enabled() || a.Reason != reasonPickACandidate {
		t.Errorf("on the current image: %+v, want %q", a, reasonPickACandidate)
	}
	// An unscanned candidate has no evidence.
	m = cursorOn(t, m, "golang:1.22")
	if a := m.canSelectCandidate(); a.Enabled() || a.Reason != reasonScanFirst {
		t.Errorf("on an unscanned candidate: %+v, want %q", a, reasonScanFirst)
	}
	m = cursorOn(t, m, "golang:1.23")
	if a := m.canSelectCandidate(); !a.Enabled() {
		t.Errorf("on a scanned candidate: %+v, want it offered", a)
	}
}

func TestSpaceRefusesWithTheReasonWhereItCannotChoose(t *testing.T) {
	m, _ := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}})
	m = feed(t, cursorOn(t, m, "golang:1.23"), testutil.Key(" "))
	if m.footer.Text() != reasonScanFirst || m.footer.Level() != sharedcomponents.LevelWarning {
		t.Errorf("footer = %q at level %v", m.footer.Text(), m.footer.Level())
	}
	if len(m.remediation.selected) != 0 {
		t.Error("an unscanned candidate was chosen")
	}
}

func TestSpaceChoosesAndReleasesACandidate(t *testing.T) {
	m, _ := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}}, "golang:1.23")

	m = choose(t, m, "golang:1.23")
	if m.remediation.selected[0] != "golang:1.23" {
		t.Fatalf("selected = %v", m.remediation.selected)
	}
	if got := m.remediation.table.Items()[1].icon(); got == m.remediation.table.Items()[2].icon() {
		t.Errorf("a chosen candidate looks like an unchosen one: %q", got)
	}

	m = feed(t, m, testutil.Key(" "))
	if len(m.remediation.selected) != 0 {
		t.Errorf("selected = %v, want it released by a second space", m.remediation.selected)
	}
}

// One base image per stage: choosing another replaces the first.
func TestChoosingAnotherCandidateOfTheSameStageReplacesIt(t *testing.T) {
	m, _ := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23", "golang:1.22"}}, "golang:1.23", "golang:1.22")
	m = choose(t, m, "golang:1.23")
	m = choose(t, m, "golang:1.22")
	if m.remediation.selected[0] != "golang:1.22" || len(m.remediation.selected) != 1 {
		t.Errorf("selected = %v, want only golang:1.22", m.remediation.selected)
	}
}

func TestAReferenceThatCannotBeEditedCannotBeChosen(t *testing.T) {
	m, _ := repoModel(t, "ARG V=20\nFROM node:${V}-alpine\n", map[int][]string{0: {"node:22-alpine"}}, "node:22-alpine")
	m = cursorOn(t, m, "node:22-alpine")
	a := m.canSelectCandidate()
	if a.Enabled() || !strings.Contains(a.Reason, "assembled from build args") {
		t.Errorf("availability = %+v, want the parser's reason", a)
	}
}

// Two stages reading one ARG are one place in the file: two answers for it
// cannot both be written.
func TestTwoStagesOnOneARGCannotChooseDifferentBases(t *testing.T) {
	src := "ARG BASE=alpine:3.18\nFROM ${BASE} AS a\nFROM ${BASE} AS b\n"
	m, _ := repoModel(t, src, map[int][]string{0: {"alpine:3.21", "alpine:3.20"}, 1: {"alpine:3.21", "alpine:3.20"}},
		"alpine:3.21", "alpine:3.20")

	m = choose(t, m, "alpine:3.21") // for stage a
	// Stage b's candidates come after a's rows.
	for range 10 {
		if row, ok := m.remediation.table.Selected(); ok && row.Entry == 1 && row.Ref == "alpine:3.20" {
			break
		}
		m = feed(t, m, testutil.Key("down"))
	}
	m = feed(t, m, testutil.Key(" "))
	if !strings.Contains(m.footer.Text(), "same ARG") {
		t.Errorf("footer = %q, want the shared ARG named", m.footer.Text())
	}
	if _, chosen := m.remediation.selected[1]; chosen {
		t.Error("a conflicting choice was accepted")
	}
}

// ── The diff ─────────────────────────────────────────────────────────────────

func TestEnterWithNothingChosenSaysWhy(t *testing.T) {
	m, _ := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}}, "golang:1.23")
	m = feed(t, m, testutil.Key("enter"))
	if m.footer.Text() != reasonNothingChosen || m.footer.Level() != sharedcomponents.LevelWarning {
		t.Errorf("footer = %q at level %v", m.footer.Text(), m.footer.Level())
	}
}

func TestEnterOpensTheDiffOfTheChosenBases(t *testing.T) {
	m, dir := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}, 1: {"alpine:3.21"}}, "golang:1.23", "alpine:3.21")
	m = choose(t, m, "golang:1.23")
	m = choose(t, m, "alpine:3.21")

	_, cmd := step(t, m, testutil.Key("enter"))
	open, ok := run(t, cmd).(uiviewer.OpenRequestMsg)
	if !ok {
		t.Fatal("enter did not ask the viewer to open anything")
	}
	body, err := open.Source.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, want := range []string{"--- Dockerfile", "-FROM golang:1.21 AS build", "+FROM golang:1.23 AS build", "-FROM alpine:3.18 AS final", "+FROM alpine:3.21 AS final"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("diff lacks %q:\n%s", want, body)
		}
	}
	if fileContent(t, dir) != twoStageFile {
		t.Error("looking at the diff changed the file")
	}
}

// ── Writing ──────────────────────────────────────────────────────────────────

func TestWriteWithNothingChosenSaysWhy(t *testing.T) {
	m, _ := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}}, "golang:1.23")
	m = feed(t, m, testutil.Key(writeRemediationKey))
	if m.footer.Text() != reasonNothingChosen {
		t.Errorf("footer = %q", m.footer.Text())
	}
	if m.confirmModal != nil {
		t.Error("a confirmation opened for nothing")
	}
}

func TestWriteIsRefusedOffTheRemediationTab(t *testing.T) {
	m := scannedModel(t)
	m = feed(t, m, testutil.Key(writeRemediationKey))
	if m.footer.Text() != reasonNotRemediationTab || m.confirmModal != nil {
		t.Errorf("footer = %q, modal open: %v", m.footer.Text(), m.confirmModal != nil)
	}
}

// prepared runs the write's first half and feeds its answer back.
func prepared(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := step(t, m, testutil.Key(writeRemediationKey))
	return feed(t, m, run(t, cmd))
}

// Nothing is written on the key alone: it asks, and the answer defaults to No.
func TestTheKeyAsksBeforeWritingAndTheAnswerDefaultsToNo(t *testing.T) {
	m, dir := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}}, "golang:1.23")
	m = prepared(t, choose(t, m, "golang:1.23"))

	if m.confirmModal == nil || len(m.remediation.pending) != 1 {
		t.Fatal("the key did not open a confirmation")
	}
	if fileContent(t, dir) != twoStageFile {
		t.Fatal("the file changed before the user answered")
	}
	if !m.InEditMode() {
		t.Error("a modal is open and the router does not know: it would claim q and : for itself")
	}

	// Enter on the untouched modal is the default answer.
	m, cmd := step(t, m, testutil.Key("enter"))
	m = feed(t, m, run(t, cmd))
	if fileContent(t, dir) != twoStageFile {
		t.Error("the default answer wrote the file")
	}
	if m.confirmModal != nil || m.remediation.pending != nil {
		t.Error("declining left the confirmation or its plan behind")
	}
	if len(m.remediation.selected) != 1 {
		t.Error("declining lost the user's choices")
	}
}

// confirm answers Yes to the modal and runs the write it starts.
func confirm(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := step(t, m, testutil.Key("y"))
	m, cmd = step(t, m, run(t, cmd)) // the modal's answer, delivered
	return feed(t, m, run(t, cmd))   // the write's result, delivered
}

func TestConfirmingWritesOnlyTheChosenImage(t *testing.T) {
	m, dir := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}, 1: {"alpine:3.21"}}, "golang:1.23")
	m = confirm(t, prepared(t, choose(t, m, "golang:1.23")))

	want := "FROM golang:1.23 AS build\nRUN go build\nFROM alpine:3.18 AS final\n"
	if got := fileContent(t, dir); got != want {
		t.Errorf("file:\n%s\nwant:\n%s", got, want)
	}
	if m.footer.Level() != sharedcomponents.LevelInfo || !strings.Contains(m.footer.Text(), "git diff") {
		t.Errorf("footer = %q at level %v, want a notice pointing at git diff", m.footer.Text(), m.footer.Level())
	}
	if len(m.remediation.selected) != 0 || m.remediation.pending != nil {
		t.Error("a written choice was kept")
	}
	// The Dockerfiles are read again, so the table shows the image now in them.
	if m.remediation.phase != phaseLoading {
		t.Errorf("phase = %d, want the Dockerfiles re-read", m.remediation.phase)
	}
}

// The user confirmed one diff. A file edited since is not that diff, and the
// write refuses rather than overwrite it.
func TestAFileEditedAfterThePreviewIsNotOverwritten(t *testing.T) {
	m, dir := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}}, "golang:1.23")
	m = prepared(t, choose(t, m, "golang:1.23"))

	edited := twoStageFile + "# added by the user meanwhile\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	m = confirm(t, m)

	if got := fileContent(t, dir); got != edited {
		t.Errorf("the edited file was overwritten:\n%s", got)
	}
	if m.footer.Level() != sharedcomponents.LevelError || !strings.Contains(m.footer.Text(), "changed since the preview") {
		t.Errorf("footer = %q at level %v", m.footer.Text(), m.footer.Level())
	}
}

func TestPreparingRefusesAFileThatMovedUnderTheSelection(t *testing.T) {
	m, dir := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}}, "golang:1.23")
	m = choose(t, m, "golang:1.23")
	// The image the span was measured on is gone.
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM debian:12 AS build\nRUN go build\nFROM alpine:3.18 AS final\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = prepared(t, m)
	if m.confirmModal != nil {
		t.Error("a confirmation opened for a change that cannot be applied")
	}
	if m.footer.Level() != sharedcomponents.LevelError {
		t.Errorf("footer = %q at level %v", m.footer.Text(), m.footer.Level())
	}
}

func TestTheConfirmationNamesTheFileTheLineAndTheChange(t *testing.T) {
	m, _ := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}, 1: {"alpine:3.21"}}, "golang:1.23", "alpine:3.21")
	m = choose(t, m, "golang:1.23")
	m = choose(t, m, "alpine:3.21")
	m = prepared(t, m)

	text := confirmationText(m.remediation.pending)
	for _, want := range []string{"Dockerfile:1  golang:1.21 -> golang:1.23", "Dockerfile:3  alpine:3.18 -> alpine:3.21"} {
		if !strings.Contains(text, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, text)
		}
	}
}

// "Nothing is lost" holds in the ordinary case; the confirmation is where the
// other cases are told apart.
func TestTheConfirmationSaysWhatGitWillAndWillNotUndo(t *testing.T) {
	tests := []struct {
		name  string
		file  preparedWrite
		wants string
	}{
		{"clean", preparedWrite{State: git.FileClean}, "git checkout"},
		{"uncommitted changes", preparedWrite{State: git.FileModified}, "discard them"},
		{"untracked", preparedWrite{State: git.FileUntracked}, "not tracked by git"},
		{"outside a repository", preparedWrite{State: git.FileOutsideRepo}, "not in a git repository"},
		{"git unreadable", preparedWrite{StateErr: true}, "git could not be read"},
	}
	for _, tt := range tests {
		if got := gitAdvice(tt.file); !strings.Contains(got, tt.wants) {
			t.Errorf("%s: advice = %q, want it to mention %q", tt.name, got, tt.wants)
		}
	}
}

func TestADigestPinnedReferenceIsToldItLosesItsPin(t *testing.T) {
	src := "FROM alpine:3.18@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n"
	m, dir := repoModel(t, src, map[int][]string{0: {"alpine:3.21"}}, "alpine:3.21")
	m = prepared(t, choose(t, m, "alpine:3.21"))

	if text := confirmationText(m.remediation.pending); !strings.Contains(text, "drops the digest pin") {
		t.Errorf("the confirmation does not say the pin is lost:\n%s", text)
	}
	m = confirm(t, m)
	if got := fileContent(t, dir); got != "FROM alpine:3.21\n" {
		t.Errorf("file = %q", got)
	}
}

func TestAnARGDefaultIsWrittenWhereTheImageIsSpelled(t *testing.T) {
	src := "ARG BASE=alpine:3.18\nFROM ${BASE}\n"
	m, dir := repoModel(t, src, map[int][]string{0: {"alpine:3.21"}}, "alpine:3.21")
	confirm(t, prepared(t, choose(t, m, "alpine:3.21")))
	if got := fileContent(t, dir); got != "ARG BASE=alpine:3.21\nFROM ${BASE}\n" {
		t.Errorf("file = %q", got)
	}
}

func TestTheRestOfTheFileIsLeftByteForByte(t *testing.T) {
	src := "# syntax=docker/dockerfile:1\r\nFROM alpine:3.18 AS a\r\nRUN echo alpine:3.18\r\nCOPY . /app"
	m, dir := repoModel(t, src, map[int][]string{0: {"alpine:3.21"}}, "alpine:3.21")
	confirm(t, prepared(t, choose(t, m, "alpine:3.21")))
	want := "# syntax=docker/dockerfile:1\r\nFROM alpine:3.21 AS a\r\nRUN echo alpine:3.18\r\nCOPY . /app"
	if got := fileContent(t, dir); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// ── Keys, shortcuts, declaration ─────────────────────────────────────────────

func TestTheKeysAreOfferedWhereTheyApply(t *testing.T) {
	m, _ := repoModel(t, twoStageFile, map[int][]string{0: {"golang:1.23"}}, "golang:1.23")

	shortcuts := m.GetShortcuts()
	if !testutil.ShortcutDisabled(shortcuts, "space") || !testutil.ShortcutDisabled(shortcuts, writeRemediationKey) {
		t.Error("space or the write key is offered with nothing to act on")
	}
	if d := descriptionOf(t, shortcuts, "enter"); d != "Show diff" {
		t.Errorf("enter reads %q on this tab, want Show diff", d)
	}

	m = choose(t, m, "golang:1.23")
	shortcuts = m.GetShortcuts()
	for _, key := range []string{"space", "enter", writeRemediationKey} {
		if !testutil.ShortcutEnabled(shortcuts, key) {
			t.Errorf("%q is not offered with a candidate chosen", key)
		}
	}
	if d := descriptionOf(t, scannedModel(t).GetShortcuts(), "enter"); d != "Details" {
		t.Errorf("enter reads %q on a findings tab, want Details", d)
	}
}

func descriptionOf(t *testing.T, shortcuts shortcut.Shortcuts, key string) string {
	t.Helper()
	for _, s := range shortcuts {
		if s.Key == key {
			return s.Description
		}
	}
	t.Fatalf("no %q shortcut", key)
	return ""
}

// The write key is a declared exception, like ctrl+y: a ctrl combination that
// is not one of the three survivors and is not declared would be drift.
func TestTheWriteKeyIsADeclaredException(t *testing.T) {
	if !keymap.IsException(writeRemediationKey) {
		t.Errorf("%q is bound but not declared in keymap.DeclaredExceptions", writeRemediationKey)
	}
	for _, e := range keymap.DeclaredExceptions() {
		if e.Key == writeRemediationKey && (e.Surface == "" || e.Why == "") {
			t.Errorf("the exception is declared without its surface or its reason: %+v", e)
		}
	}
}
