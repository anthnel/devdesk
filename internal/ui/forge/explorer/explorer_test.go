package explorer

import (
	"io"
	"log"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
	gitlabforge "github.com/anthnel/devdesk/internal/forge/gitlab"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The state machine is driven by messages: groups, children, templates and
// completion results are fed in, and the assertions are on model state. Nothing
// here executes a command that reaches the desktop browser.
//
// The commands that talk to GitLab, the OCI registry and git are executed, in
// api_test.go and pull_test.go, against an httptest server and a throwaway git
// repository — the same trick internal/gitlab uses. A stub would only prove the
// stub works.
//
// These files were written in the order the backlog settled on: the model tests
// first, driving Update() and View() only, so the split of the 1402-line
// model.go that followed moved code with the coverage figure unchanged.

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	code := m.Run()
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.Forge.URL = "https://gitlab.example.com"
	cfg.Forge.CloneMethod = "https"
	cfg.Forge.DefaultVisibility = "private"
	return cfg
}

// authenticatedState carries a backend so the view leaves its "not
// authenticated" branch. Building one issues no request; only the commands
// would.
func authenticatedState(t *testing.T) *shared.State {
	t.Helper()
	backend, err := gitlabforge.New("https://gitlab.example.com", "test-token")
	if err != nil {
		t.Fatalf("gitlabforge.New() error = %v", err)
	}
	return &shared.State{Forge: backend, IsAuthenticated: true}
}

func at(day int) *time.Time {
	ts := time.Date(2026, 7, day, 12, 0, 0, 0, time.UTC)
	return &ts
}

// rootFixtures are three top-level groups. Every sortable column carries a total
// order — sort.Slice is not stable, so ties would make the expected sequences
// ambiguous.
func rootFixtures() []*TreeNode {
	return []*TreeNode{
		{
			ID: "1", Name: "alpha", FullPath: "alpha", Type: NodeTypeGroup,
			Visibility: "private", Role: "Owner", CreatedAt: at(1),
			WebURL: "https://gitlab.example.com/alpha",
		},
		{
			ID: "2", Name: "beta", FullPath: "beta", Type: NodeTypeGroup,
			Visibility: "internal", Role: "Developer", CreatedAt: at(2),
			WebURL: "https://gitlab.example.com/beta",
		},
		{
			ID: "3", Name: "gamma", FullPath: "gamma", Type: NodeTypeGroup,
			Visibility: "public", Role: "Guest", CreatedAt: at(3),
			WebURL: "https://gitlab.example.com/gamma",
		},
	}
}

// childFixtures are what "alpha" holds: a subgroup and two projects, one of them
// already scheduled for deletion.
func childFixtures(parent *TreeNode) []*TreeNode {
	return []*TreeNode{
		{
			ID: "10", Name: "sub", FullPath: "alpha/sub", Type: NodeTypeGroup, Parent: parent,
			Visibility: "private", Role: "Maintainer", CreatedAt: at(4),
			WebURL: "https://gitlab.example.com/alpha/sub",
		},
		{
			ID: "11", Name: "api", FullPath: "alpha/api", Type: NodeTypeProject, Parent: parent,
			Visibility: "internal", Role: "Developer", CreatedAt: at(5), LastActivityAt: at(6),
			PipelineStatus: "success", WebURL: "https://gitlab.example.com/alpha/api",
		},
		{
			ID: "12", Name: "legacy", FullPath: "alpha/legacy", Type: NodeTypeProject, Parent: parent,
			Visibility: "public", Role: "Reporter", CreatedAt: at(7), LastActivityAt: at(8),
			PipelineStatus: "failed", MarkedForDeletion: true,
			WebURL: "https://gitlab.example.com/alpha/legacy",
		},
	}
}

// newTestModel returns a laid-out, authenticated model with no groups yet.
func newTestModel(t *testing.T) Model {
	t.Helper()
	return feed(t, New(testConfig(), authenticatedState(t)), tea.WindowSizeMsg{Width: 160, Height: 30})
}

// loadedModel returns a model showing the three root groups.
func loadedModel(t *testing.T) Model {
	t.Helper()
	return feed(t, newTestModel(t), RootGroupsLoadedMsg{Nodes: rootFixtures()})
}

// drilledModel returns a model inside "alpha", showing its three children.
func drilledModel(t *testing.T) Model {
	t.Helper()
	m := loadedModel(t)
	alpha := m.nodes[0]
	m = feed(t, m, tea.KeyMsg{Type: tea.KeyRight})
	return feed(t, m, ChildrenLoadedMsg{ParentNode: alpha, Children: childFixtures(alpha)})
}

func feed(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		m, _ = step(t, m, msg)
	}
	return m
}

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("Update() returned %T, want explorer.Model", next)
	}
	return updated, cmd
}

// rowNames returns the Name cell of every table row.
func rowNames(rows []table.Row) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row[1])
	}
	return names
}

// newGroup and newProject build the API payloads the creation handlers read,
// which is only the path they select afterwards.
func newGroup(id int64, fullPath string) forge.Namespace {
	return forge.Namespace{ID: strconv.FormatInt(id, 10), Path: fullPath}
}

func newProject(id int64, pathWithNamespace string) forge.Repository {
	return forge.Repository{ID: strconv.FormatInt(id, 10), Path: pathWithNamespace}
}

func newUser(username string) forge.User {
	return forge.User{ID: "1", Username: username}
}

// withTrueColor forces a colour profile for the run. Under go test lipgloss
// detects no TTY, falls back to Ascii and strips every escape sequence, which
// would make any assertion about styling pass whatever the code does.
func withTrueColor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

// creationSubmit builds the form result the create commands read.
func creationSubmit(name string) components.CreationFormSubmitMsg {
	return components.CreationFormSubmitMsg{Name: name, Description: "", Visibility: "private"}
}

// startedRun returns the run a command asks the router to register, if it asks
// for one. Creating and deleting are registry work now, so a launch site
// returns a jobs.StartMsg rather than the API command itself.
func startedRun(cmd tea.Cmd) (jobs.Run, bool) {
	msg, ok := testutil.MsgOf[jobs.StartMsg](cmd)
	if !ok {
		return jobs.Run{}, false
	}
	return msg.Run, true
}

// runWork executes the work a jobs.StartMsg carries, which is what the router
// does one step after admitting the run. It is how a test reaches the API call
// that used to be the command itself.
func runWork(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	msg, ok := testutil.MsgOf[jobs.StartMsg](cmd)
	if !ok {
		t.Fatalf("no run was registered, got %T", testutil.Msg(cmd))
	}
	if msg.Work == nil {
		t.Fatal("the registered run carries no work")
	}
	return testutil.Msg(msg.Work(""))
}

// rowFor returns the visible row for a path, which is what a test asserting on
// a cell needs — the node alone does not carry the decoration.
func rowFor(m Model, path string) (explorerRow, bool) {
	for _, row := range m.table.Visible() {
		if row.node.FullPath == path {
			return row, true
		}
	}
	return explorerRow{}, false
}
