package explorer

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/oci"
)

// DeleteCompleteMsg is sent when a delete operation completes
type DeleteCompleteMsg struct {
	Error       error
	DeletedNode *TreeNode
}

// RootGroupsLoadedMsg est envoyé quand les groupes racine sont chargés
type RootGroupsLoadedMsg struct {
	Nodes []*TreeNode
}

// GroupCreatedMsg est envoyé quand un groupe est créé
type GroupCreatedMsg struct {
	Group *gitlabclient.Group
	Error error
}

// ProjectCreatedMsg est envoyé quand un projet est créé
type ProjectCreatedMsg struct {
	Project       *gitlabclient.Project
	Error         error
	TemplateError error // Non-nil if template application failed (project still exists)
}

// TemplatesLoadedMsg est envoyé quand les templates OCI sont chargées
type TemplatesLoadedMsg struct {
	Templates []oci.TemplateEntry
	Error     error
}

// ChildrenLoadedMsg est envoyé quand les enfants d'un nœud sont chargés
type ChildrenLoadedMsg struct {
	ParentNode *TreeNode
	Children   []*TreeNode
}

// LoadErrorMsg est envoyé en cas d'erreur de chargement
type LoadErrorMsg struct {
	Error      error
	ParentNode *TreeNode // Optional: node that was loading when error occurred
}

// CloneSelectionRequestMsg asks the app to borrow the workspaces view so the
// user can pick where the clones go (decision 5).
type CloneSelectionRequestMsg struct{}

// CloneDestinationSelectedMsg is the app's answer: the chosen directory.
type CloneDestinationSelectedMsg struct {
	Path string
}

// CloneSelectionCancelledMsg is sent by the app when the user backed out of the
// destination picker. The selection is kept — only the destination was refused.
type CloneSelectionCancelledMsg struct{}

// CloneEventMsg carries one pipeline event into Update. The event itself is
// unexported: it is the pipeline's vocabulary, and nothing outside this package
// has anything to do with it.
type CloneEventMsg struct {
	event cloneEvent
}

// CloneRunFinishedMsg is the closed event channel — every clone has returned,
// whether the run was cancelled or ran to the end.
type CloneRunFinishedMsg struct{}

// clearFooterMsgCmd clears the footer error or info after 3 seconds (Rule 128)
func clearFooterMsgCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return clearFooterMsg{}
	})
}

// clearFooterMsg is sent to clear the footer message after a delay
type clearFooterMsg struct{}

// BrowserOpenedMsg is sent when the browser launch command has been started
type BrowserOpenedMsg struct {
	Error error
}
