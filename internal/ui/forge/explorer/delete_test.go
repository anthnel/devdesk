package explorer

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	githubforge "github.com/anthnel/devdesk/internal/forge/github"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The delete command is executed against the fake API: which endpoint it hits
// is the difference between removing a group and removing a project, and the
// node type is the only thing that decides.
func TestConfirmedDeleteHitsTheRightEndpoint(t *testing.T) {
	tests := []struct {
		name string
		node *TreeNode
		want string
	}{
		{"group", &TreeNode{ID: "42", FullPath: "infra", Type: NodeTypeGroup}, "/api/v4/groups/42"},
		{"project", &TreeNode{ID: "7", FullPath: "infra/api", Type: NodeTypeProject}, "/api/v4/projects/7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeGitLab(t, map[string]string{"/api/v4": `{}`})
			m := serverModel(t, f)
			m.deleteTargetNode = tt.node

			next, cmd := m.handleDeleteConfirmed(false)

			run, started := startedRun(cmd)
			if !started {
				t.Fatalf("confirming produced %T, want a registered run", testutil.Msg(cmd))
			}
			if run.Kind != jobs.KindDelete {
				t.Errorf("run kind = %q, want %q", run.Kind, jobs.KindDelete)
			}
			if len(run.Items) != 1 || run.Items[0].Target != tt.node.FullPath {
				t.Errorf("run items = %+v, want the node's path", run.Items)
			}

			msg, ok := runWork(t, cmd).(DeleteCompleteMsg)
			if !ok {
				t.Fatal("the delete work produced no DeleteCompleteMsg")
			}
			if msg.Error != nil {
				t.Errorf("delete reported %v", msg.Error)
			}
			if msg.Target != tt.node.FullPath {
				t.Errorf("DeleteCompleteMsg target = %q, want %q", msg.Target, tt.node.FullPath)
			}
			if msg.DeletedNode != tt.node {
				t.Error("DeleteCompleteMsg does not name the node that was deleted")
			}
			if len(f.paths()) == 0 || f.paths()[0] != tt.want {
				t.Errorf("first request = %v, want %s", f.paths(), tt.want)
			}
			if updated := next.(Model); updated.deleteTargetNode != nil || updated.mode != ModeNormal {
				t.Errorf("confirming left target=%v mode=%v", updated.deleteTargetNode, updated.mode)
			}
		})
	}
}

// A failure has to come back as a message rather than being swallowed inside
// the command, or the row silently stays.
func TestConfirmedDeleteReportsAFailure(t *testing.T) {
	m := serverModel(t, newFakeGitLab(t, nil)) // everything 404s
	m.deleteTargetNode = &TreeNode{ID: "42", FullPath: "infra", Type: NodeTypeGroup}

	_, cmd := m.handleDeleteConfirmed(false)

	msg, ok := runWork(t, cmd).(DeleteCompleteMsg)
	if !ok {
		t.Fatal("the delete work produced no DeleteCompleteMsg")
	}
	if msg.Error == nil {
		t.Error("a failing delete reported no error")
	}
}

// Confirming with nothing selected, or after the session expired, must not
// reach the API at all.
func TestConfirmedDeleteWithoutATargetOrAClient(t *testing.T) {
	t.Run("no target", func(t *testing.T) {
		m := serverModel(t, newFakeGitLab(t, map[string]string{"/api/v4": `{}`}))

		if _, cmd := m.handleDeleteConfirmed(false); cmd != nil {
			t.Errorf("confirming with no target issued %T", testutil.Msg(cmd))
		}
	})

	t.Run("no client", func(t *testing.T) {
		m := New(testConfig(), &shared.State{})
		m.deleteTargetNode = &TreeNode{ID: "1", Type: NodeTypeGroup}

		next, cmd := m.handleDeleteConfirmed(false)

		if cmd != nil {
			t.Errorf("confirming without a client issued %T", testutil.Msg(cmd))
		}
		if updated := next.(Model); updated.error == "" {
			t.Error("confirming without a client reported nothing")
		}
	})
}

// The checkbox in the modal is what decides between scheduling and purging, and
// it has to reach the API call.
func TestPermanentDeleteIsPassedThrough(t *testing.T) {
	f := newFakeGitLab(t, map[string]string{"/api/v4": `{}`})
	m := serverModel(t, f)
	m.deleteTargetNode = &TreeNode{ID: "42", FullPath: "infra/tools", Type: NodeTypeGroup}

	_, cmd := m.handleDeleteConfirmed(true)
	runWork(t, cmd)

	// The permanent path deletes twice: schedule, then purge under the renamed
	// -deletion_scheduled-<id> path.
	if len(f.paths()) < 2 {
		t.Fatalf("requests = %v, want the two-step permanent delete", f.paths())
	}
}

// GitHub deletes at once and has no grace period (forge.Shape.PermanentDelete
// is false there), so the confirmation must not offer a checkbox that would be
// meaningless — see NewDeleteConfirmModal.
func TestDeleteStartOffersNoImmediateOptionOnGitHub(t *testing.T) {
	backend, err := githubforge.New("", "test-token")
	if err != nil {
		t.Fatalf("githubforge.New() error = %v", err)
	}
	m := feed(t, New(testConfig(), &shared.State{Forge: backend, IsAuthenticated: true}),
		tea.WindowSizeMsg{Width: 160, Height: 30},
		RootGroupsLoadedMsg{Nodes: rootFixtures()},
	)

	m = feed(t, m, testutil.Key(keymap.Delete))
	if m.deleteConfirmModal == nil {
		t.Fatal("ctrl+d opened no confirmation")
	}
	if strings.Contains(m.deleteConfirmModal.View(), "Immediate deletion") {
		t.Error("the immediate-deletion checkbox is offered on a GitHub-backed forge")
	}
}

// Rule 128 again, from the key rather than from a synthesised message: the
// confirmation modal answers ctrl+d's modal with a Yes.
func TestDeleteFlowFromTheKeyToTheFooter(t *testing.T) {
	m := feed(t, drilledModel(t), testutil.Key(keymap.Delete))
	if m.deleteConfirmModal == nil {
		t.Fatal("ctrl+d opened no confirmation")
	}

	m = feed(t, m, components.OptionConfirmModalYesMsg{Option: false})
	if m.deleteConfirmModal != nil {
		t.Error("the confirmation is still open after answering it")
	}
}
