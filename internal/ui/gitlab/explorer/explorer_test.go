package explorer

import (
	"io"
	"log"
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/gitlab"
	"github.com/anthnel/devdesk/internal/shared"
)

// Every command this view returns talks to the GitLab API, the OCI registry, the
// filesystem or the desktop browser. No test executes one: groups, children,
// templates and completion results are fed in as messages, and the assertions
// are on model state.
//
// These tests drive Update() and View() only — model.go is 1402 lines and due to
// be split, and assertions on its internals would pin the current layout.

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	code := m.Run()
	log.SetOutput(os.Stderr)
	os.Exit(code)
}

func testConfig() *config.Config {
	cfg := config.Default()
	cfg.GitLab.URL = "https://gitlab.example.com"
	cfg.GitLab.CloneMethod = "https"
	cfg.GitLab.DefaultVisibility = "private"
	return cfg
}

// authenticatedState carries a client so the view leaves its "not authenticated"
// branch. Building one issues no request; only the commands would.
func authenticatedState(t *testing.T) *shared.State {
	t.Helper()
	client, err := gitlab.NewClient("https://gitlab.example.com", "test-token")
	if err != nil {
		t.Fatalf("gitlab.NewClient() error = %v", err)
	}
	return &shared.State{GitLabClient: client, IsAuthenticated: true}
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
			ID: 1, Name: "alpha", FullPath: "alpha", Type: NodeTypeGroup,
			Visibility: "private", AccessLevel: 50, CreatedAt: at(1),
			WebURL: "https://gitlab.example.com/alpha",
		},
		{
			ID: 2, Name: "beta", FullPath: "beta", Type: NodeTypeGroup,
			Visibility: "internal", AccessLevel: 30, CreatedAt: at(2),
			WebURL: "https://gitlab.example.com/beta",
		},
		{
			ID: 3, Name: "gamma", FullPath: "gamma", Type: NodeTypeGroup,
			Visibility: "public", AccessLevel: 10, CreatedAt: at(3),
			WebURL: "https://gitlab.example.com/gamma",
		},
	}
}

// childFixtures are what "alpha" holds: a subgroup and two projects, one of them
// already scheduled for deletion.
func childFixtures(parent *TreeNode) []*TreeNode {
	return []*TreeNode{
		{
			ID: 10, Name: "sub", FullPath: "alpha/sub", Type: NodeTypeGroup, Parent: parent,
			Visibility: "private", AccessLevel: 40, CreatedAt: at(4),
			WebURL: "https://gitlab.example.com/alpha/sub",
		},
		{
			ID: 11, Name: "api", FullPath: "alpha/api", Type: NodeTypeProject, Parent: parent,
			Visibility: "internal", AccessLevel: 30, CreatedAt: at(5), LastActivityAt: at(6),
			PipelineStatus: "success", WebURL: "https://gitlab.example.com/alpha/api",
		},
		{
			ID: 12, Name: "legacy", FullPath: "alpha/legacy", Type: NodeTypeProject, Parent: parent,
			Visibility: "public", AccessLevel: 20, CreatedAt: at(7), LastActivityAt: at(8),
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
func newGroup(id int64, fullPath string) *gitlabclient.Group {
	return &gitlabclient.Group{ID: id, FullPath: fullPath}
}

func newProject(id int64, pathWithNamespace string) *gitlabclient.Project {
	return &gitlabclient.Project{ID: id, PathWithNamespace: pathWithNamespace}
}
