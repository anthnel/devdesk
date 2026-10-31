package app

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-88 step 7 (DEVDESK-116): executing the actions contextcmd resolves.
// Every test runs the Cmds the router returns and feeds the context messages
// back through Update, over an isolated HOME. Footer texts are the ones of
// docs/ux/context-switch.md (E1..E9).

// drive executes cmd and feeds the context messages it yields back through
// Update (and the Cmds those return), returning every footer post met on the
// way. Expiry timers are shortened (Rule 128) and never fed back, so a posted
// message stays on screen.
func drive(t *testing.T, a *App, cmd tea.Cmd) []components.PostFooterMsg {
	t.Helper()
	testutil.FastTimers(t, &components.FooterMsgDuration)
	var posts []components.PostFooterMsg
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0 && steps < 20; steps++ {
		next := queue[0]
		queue = queue[1:]
		for _, m := range testutil.Msgs(next) {
			switch msg := m.(type) {
			case components.PostFooterMsg:
				posts = append(posts, msg)
				_, c := a.Update(msg)
				queue = append(queue, c)
			case ContextSwitchCompleteMsg, ContextSwitchErrorMsg, ContextListMsg, contextNamesLoadedMsg:
				_, c := a.Update(msg)
				queue = append(queue, c)
			}
		}
	}
	return posts
}

// ctxRun reloads the snapshot as opening the command line does, types line,
// presses enter and drives the result.
func ctxRun(t *testing.T, a *App, line string) []components.PostFooterMsg {
	t.Helper()
	testutil.FastTimers(t, &components.FooterMsgDuration)
	ctxOpen(t, a, line)
	return drive(t, a, press(t, a, "enter"))
}

func onlyPost(t *testing.T, posts []components.PostFooterMsg) components.PostFooterMsg {
	t.Helper()
	if len(posts) != 1 {
		t.Fatalf("footer posts = %+v, want exactly one", posts)
	}
	return posts[0]
}

func wantPost(t *testing.T, posts []components.PostFooterMsg, level components.Level, text string) {
	t.Helper()
	p := onlyPost(t, posts)
	if p.Level != level || p.Text != text {
		t.Errorf("footer = level %v %q, want level %v %q", p.Level, p.Text, level, text)
	}
}

func contextFileExists(t *testing.T, name string) bool {
	t.Helper()
	p, err := config.GetContextPath(name)
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(p)
	return err == nil
}

func contextNames(t *testing.T) []string {
	t.Helper()
	names, err := config.ListContexts()
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func requireSettled(t *testing.T, a *App, wantContext string) {
	t.Helper()
	if a.switchingTo != "" {
		t.Errorf("switchingTo = %q, want the overlay gone", a.switchingTo)
	}
	if strings.Contains(a.View(), "Switching to context") {
		t.Error("the switching overlay is still on screen")
	}
	if a.currentContext != wantContext {
		t.Errorf("currentContext = %q, want %q", a.currentContext, wantContext)
	}
}

// ── switchContext: loads, never creates ──────────────────────────────────────

func TestSwitchContextLoadsAnExistingContext(t *testing.T) {
	a := argRouter(t)
	cmd := a.switchContext("work")
	done, ok := testutil.MsgOf[ContextSwitchCompleteMsg](cmd)
	if !ok {
		t.Fatalf("no ContextSwitchCompleteMsg, got %T", testutil.Msg(a.switchContext("work")))
	}
	if done.ContextName != "work" || done.Created || done.Config == nil {
		t.Errorf("complete = name %q created %v config nil %v", done.ContextName, done.Created, done.Config == nil)
	}
}

func TestSwitchContextToAMissingContextCreatesNothing(t *testing.T) {
	a := argRouter(t)
	msg, ok := testutil.MsgOf[ContextSwitchErrorMsg](a.switchContext("ghost"))
	if !ok {
		t.Fatalf("no ContextSwitchErrorMsg, got %T", testutil.Msg(a.switchContext("ghost")))
	}
	if !errors.Is(msg.Error, config.ErrContextNotFound) {
		t.Errorf("error = %v, want ErrContextNotFound", msg.Error)
	}
	if msg.Name != "ghost" || msg.Op != opSwitch {
		t.Errorf("msg = name %q op %q, want ghost/switch", msg.Name, msg.Op)
	}
	if contextFileExists(t, "ghost") {
		t.Error("switching to a missing context created its file")
	}
}

func TestCreateContextCreatesThenSwitches(t *testing.T) {
	a := argRouter(t)
	done, ok := testutil.MsgOf[ContextSwitchCompleteMsg](a.createContext("staging"))
	if !ok {
		t.Fatalf("no ContextSwitchCompleteMsg, got %T", testutil.Msg(a.createContext("staging")))
	}
	if !done.Created || done.ContextName != "staging" {
		t.Errorf("complete = name %q created %v", done.ContextName, done.Created)
	}
	if !contextFileExists(t, "staging") {
		t.Error("the context file was not created")
	}
	if cur, _ := config.GetCurrentContext(); cur != "staging" {
		t.Errorf("persisted current context = %q, want staging", cur)
	}
}

func TestCreateContextThatExistsKeepsItsFile(t *testing.T) {
	a := argRouter(t)
	p, _ := config.GetContextPath("work")
	before, _ := os.ReadFile(p)
	msg, ok := testutil.MsgOf[ContextSwitchErrorMsg](a.createContext("work"))
	if !ok {
		t.Fatalf("no ContextSwitchErrorMsg, got %T", testutil.Msg(a.createContext("work")))
	}
	if !errors.Is(msg.Error, config.ErrContextExists) || msg.Op != opCreate || msg.Name != "work" {
		t.Errorf("msg = %+v, want ErrContextExists/create/work", msg)
	}
	if after, _ := os.ReadFile(p); !bytes.Equal(before, after) {
		t.Error("the existing context file was modified")
	}
}

func TestCreateContextWithAReservedNameCreatesNothing(t *testing.T) {
	a := argRouter(t)
	for _, name := range []string{"new", "-", "list"} {
		msg, ok := testutil.MsgOf[ContextSwitchErrorMsg](a.createContext(name))
		if !ok || !errors.Is(msg.Error, config.ErrReservedContextName) {
			t.Errorf("createContext(%q) = %+v, want ErrReservedContextName", name, msg)
		}
		if contextFileExists(t, name) {
			t.Errorf("a context file named %q exists", name)
		}
	}
}

// ── Criterion 2: ctx wo, tab, enter ──────────────────────────────────────────

func TestCtxPrefixTabEnterSwitchesAndLoadsWork(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx wo")
	press(t, a, "tab")
	posts := drive(t, a, press(t, a, "enter"))

	requireSettled(t, a, "work")
	if len(posts) != 0 {
		t.Errorf("a plain switch posted %+v", posts)
	}
	if cur, _ := config.GetCurrentContext(); cur != "work" {
		t.Errorf("persisted current context = %q, want work", cur)
	}
}

// ── Criteria 4 / E7: already on it ───────────────────────────────────────────

func TestCtxCurrentContextReloadsNothing(t *testing.T) {
	a := argRouter(t, "work", "web", "wip")
	view := a.currentView
	testutil.FastTimers(t, &components.FooterMsgDuration)
	ctxOpen(t, a, "ctx work")
	posts := drive(t, a, press(t, a, "enter"))
	if a.previousContext != "" || a.switchingTo != "" {
		t.Error("a reload started")
	}
	wantPost(t, posts, components.LevelInfo, `Already on context "work"`)
	requireSettled(t, a, "work")
	if a.currentView != view {
		t.Errorf("view changed from %v to %v", view, a.currentView)
	}
	if a.previousContext != "" {
		t.Errorf("previousContext = %q, want it untouched", a.previousContext)
	}
}

func TestContextListPickingTheCurrentContextReloadsNothing(t *testing.T) {
	a := argRouter(t, "work", "web")
	a.Update(ContextListMsg{Contexts: []string{"web", "work"}, Current: "work"})
	testutil.FastTimers(t, &components.FooterMsgDuration)
	cmd := press(t, a, "enter")
	if a.switchingTo != "" {
		t.Fatal("picking the current context in the list started a reload")
	}
	wantPost(t, drive(t, a, cmd), components.LevelInfo, `Already on context "work"`)
	requireSettled(t, a, "work")
	if a.showContextList {
		t.Error("the list overlay stayed open")
	}
}

func TestContextListPickingAnotherContextSwitches(t *testing.T) {
	a := argRouter(t, "work", "web")
	a.Update(ContextListMsg{Contexts: []string{"web", "work"}, Current: "work"})
	press(t, a, "up")
	drive(t, a, press(t, a, "enter"))
	requireSettled(t, a, "web")
	if a.previousContext != "work" {
		t.Errorf("previousContext = %q, want work", a.previousContext)
	}
}

// ── Criterion 5 / E1 / E1b: a typo creates nothing ───────────────────────────

func TestCtxTypoCreatesNoFileAndAnswersNotFound(t *testing.T) {
	a := argRouter(t)
	before := contextNames(t)
	posts := ctxRun(t, a, "ctx wrok")
	wantPost(t, posts, components.LevelError,
		`Context "wrok" not found — did you mean "work"? Use :ctx new wrok to create it`)
	if contextFileExists(t, "wrok") {
		t.Error("config-wrok.yaml was created")
	}
	if got := contextNames(t); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Errorf("contexts = %v, want %v", got, before)
	}
	requireSettled(t, a, "default")
}

func TestCtxUnknownNameWithoutLookalikeAnswersE1(t *testing.T) {
	a := argRouter(t)
	wantPost(t, ctxRun(t, a, "ctx zzzzz"), components.LevelError,
		`Context "zzzzz" not found — use :ctx new zzzzz to create it`)
	if contextFileExists(t, "zzzzz") {
		t.Error("a context was created")
	}
}

// ── Criterion 6 / E4: invalid name ───────────────────────────────────────────

func TestCtxUppercaseNameIsInvalidAndCreatesNothing(t *testing.T) {
	a := argRouter(t)
	wantPost(t, ctxRun(t, a, "ctx Work"), components.LevelError,
		`Invalid context name "Work" — use lowercase letters, digits and hyphens`)
	requireSettled(t, a, "default")
}

func TestHandlerMapsAFormatErrorToTheInvalidNameText(t *testing.T) {
	a := argRouter(t)
	err := config.ValidateContextName("Work")
	if err == nil {
		t.Fatal("precondition: Work should fail the format check")
	}
	_, cmd := a.Update(ContextSwitchErrorMsg{Name: "Work", Op: opCreate, Error: err})
	wantPost(t, drive(t, a, cmd), components.LevelError,
		`Invalid context name "Work" — use lowercase letters, digits and hyphens`)
}

// ── E4b: reserved words ──────────────────────────────────────────────────────

func TestCtxNewWithAReservedNameIsRefusedWithNoFile(t *testing.T) {
	for _, name := range []string{"new", "-", "list"} {
		t.Run(name, func(t *testing.T) {
			a := argRouter(t)
			posts := ctxRun(t, a, "ctx new "+name)
			p := onlyPost(t, posts)
			if p.Level != components.LevelError || !strings.Contains(p.Text, "reserved") {
				t.Errorf("footer = level %v %q, want an Error saying it is reserved", p.Level, p.Text)
			}
			if contextFileExists(t, name) {
				t.Errorf("a context file named %q was created", name)
			}
			requireSettled(t, a, "default")
		})
	}
}

func TestHandlerMapsAReservedErrorToItsText(t *testing.T) {
	a := argRouter(t)
	_, cmd := a.Update(ContextSwitchErrorMsg{Name: "new", Op: opCreate, Error: config.ValidateNewContextName("new")})
	wantPost(t, drive(t, a, cmd), components.LevelError, `"new" is reserved — choose another name`)
}

// ── Criterion 7 / E9: unreadable context ─────────────────────────────────────

func TestCtxWithBrokenYAMLFailsAndKeepsThePreviousView(t *testing.T) {
	a := argRouter(t)
	p, _ := config.GetContextPath("work")
	if err := os.WriteFile(p, []byte("app: [unclosed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(io.Discard) })
	view := a.currentView

	posts := ctxRun(t, a, "ctx work")

	wantPost(t, posts, components.LevelError, `Failed to switch to "work" — check logs`)
	if !strings.Contains(logs.String(), "ERROR [app/context]") {
		t.Errorf("no ERROR [app/context] log line, got %q", logs.String())
	}
	requireSettled(t, a, "default")
	if a.currentView != view {
		t.Errorf("view changed from %v to %v", view, a.currentView)
	}
	if a.previousContext != "" {
		t.Errorf("previousContext = %q after a failed switch", a.previousContext)
	}
}

// ── ctx - ────────────────────────────────────────────────────────────────────

func TestCtxDashAtSessionStartWarns(t *testing.T) {
	a := argRouter(t)
	if a.previousContext != "" {
		t.Fatalf("previousContext = %q at startup, want empty", a.previousContext)
	}
	wantPost(t, ctxRun(t, a, "ctx -"), components.LevelWarning, "No previous context in this session")
	requireSettled(t, a, "default")
}

func TestCtxDashAlternatesBetweenTwoContexts(t *testing.T) {
	a := argRouter(t)
	ctxRun(t, a, "ctx work")
	if a.currentContext != "work" || a.previousContext != "default" {
		t.Fatalf("after A->B: current %q previous %q", a.currentContext, a.previousContext)
	}
	ctxRun(t, a, "ctx -")
	if a.currentContext != "default" || a.previousContext != "work" {
		t.Fatalf("after ctx -: current %q previous %q", a.currentContext, a.previousContext)
	}
	ctxRun(t, a, "ctx -")
	if a.currentContext != "work" || a.previousContext != "default" {
		t.Fatalf("after ctx - again: current %q previous %q", a.currentContext, a.previousContext)
	}
	if contextFileExists(t, "-") {
		t.Error("a context named - exists")
	}
	for _, n := range contextNames(t) {
		if n == "-" {
			t.Error("a context named - is listed")
		}
	}
}

func TestPreviousContextIsNotUpdatedByAReloadOfTheSameContext(t *testing.T) {
	a := argRouter(t)
	ctxRun(t, a, "ctx work")
	a.Update(ContextSwitchCompleteMsg{ContextName: "work", Config: testConfig()})
	if a.previousContext != "default" {
		t.Errorf("previousContext = %q after reloading work, want default", a.previousContext)
	}
}

func TestPreviousContextIsNeverPersisted(t *testing.T) {
	a := argRouter(t)
	ctxRun(t, a, "ctx work") // settles whatever pointer file a first switch writes
	root := os.Getenv("HOME")
	listing := func() map[string]bool {
		out := map[string]bool{}
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				out[p] = true
			}
			return nil
		})
		return out
	}
	before := listing()
	ctxRun(t, a, "ctx -")
	ctxRun(t, a, "ctx -")
	for p := range listing() {
		if !before[p] {
			t.Errorf("a switch wrote a new file: %s", p)
		}
	}
}

// ── ctx new (E8, E5, E6) ─────────────────────────────────────────────────────

func TestCtxNewCreatesSwitchesAndAnnounces(t *testing.T) {
	a := argRouter(t)
	posts := ctxRun(t, a, "ctx new staging")

	wantPost(t, posts, components.LevelInfo, `Context "staging" created`)
	if !contextFileExists(t, "staging") {
		t.Fatal("the context was not created")
	}
	requireSettled(t, a, "staging")
	if a.previousContext != "default" {
		t.Errorf("previousContext = %q, want default", a.previousContext)
	}
	if a.currentView != command.ViewGitAuth {
		t.Errorf("view = %v, want the auth view (no forge in a new context)", a.currentView)
	}
	if !strings.Contains(a.View(), `Context "staging" created`) {
		t.Error("the view shown after the switch does not display the creation notice")
	}
	if !slices.Contains(a.snapshot().Names, "staging") {
		t.Errorf("snapshot names = %v, want staging included after the reload", a.snapshot().Names)
	}
}

func TestCtxNewOfAnExistingContextWarnsAndChangesNothing(t *testing.T) {
	a := argRouter(t)
	p, _ := config.GetContextPath("work")
	before, _ := os.ReadFile(p)
	posts := ctxRun(t, a, "ctx new work")
	wantPost(t, posts, components.LevelWarning, `Context "work" already exists — use :ctx work to switch`)
	if after, _ := os.ReadFile(p); !bytes.Equal(before, after) {
		t.Error("the existing file was modified")
	}
	requireSettled(t, a, "default")
}

func TestCtxNewOfTheCurrentContextWarnsExistsNotAlready(t *testing.T) {
	a := argRouter(t)
	wantPost(t, ctxRun(t, a, "ctx new default"), components.LevelWarning,
		`Context "default" already exists — use :ctx default to switch`)
	requireSettled(t, a, "default")
}

// ── Races between the snapshot and the command ───────────────────────────────

func TestContextRemovedAfterTheSnapshotAnswersNotFoundAndCreatesNothing(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx wip")
	// wip was never saved; stand in for "deleted since the snapshot" by naming
	// a context the snapshot knows and removing its file.
	a.ctxArgs.names = append(a.ctxArgs.names, "wip")
	posts := drive(t, a, press(t, a, "enter"))
	wantPost(t, posts, components.LevelError, `Context "wip" not found — use :ctx new wip to create it`)
	if contextFileExists(t, "wip") {
		t.Error("the vanished context was recreated")
	}
	requireSettled(t, a, "default")
}

func TestContextCreatedAfterTheSnapshotAnswersAlreadyExists(t *testing.T) {
	a := argRouter(t)
	ctxOpen(t, a, "ctx new staging")
	if err := config.SaveContext(testConfig(), "staging"); err != nil {
		t.Fatal(err)
	}
	p, _ := config.GetContextPath("staging")
	before, _ := os.ReadFile(p)
	posts := drive(t, a, press(t, a, "enter"))
	wantPost(t, posts, components.LevelWarning, `Context "staging" already exists — use :ctx staging to switch`)
	if after, _ := os.ReadFile(p); !bytes.Equal(before, after) {
		t.Error("the context created meanwhile was overwritten")
	}
	requireSettled(t, a, "default")
}

// ── Handler: any other error ─────────────────────────────────────────────────

func TestHandlerMapsAnyOtherErrorToFailedToSwitchAndLogsIt(t *testing.T) {
	a := argRouter(t)
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(io.Discard) })
	a.switchingTo = "work"
	_, cmd := a.Update(ContextSwitchErrorMsg{Name: "work", Op: opSwitch, Error: errors.New("disk on fire")})
	wantPost(t, drive(t, a, cmd), components.LevelError, `Failed to switch to "work" — check logs`)
	if !strings.Contains(logs.String(), "ERROR [app/context]") || !strings.Contains(logs.String(), "disk on fire") {
		t.Errorf("log = %q, want an ERROR [app/context] line carrying the cause", logs.String())
	}
	requireSettled(t, a, "default")
}
