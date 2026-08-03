package explorer

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/oci"
	"github.com/anthnel/devdesk/internal/ui/components"
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

// PullCompleteMsg est envoyé quand l'opération de pull est terminée
type PullCompleteMsg struct {
	Report components.PullReport
}

// PullSelectionRequestMsg is sent to the app to open workspace selection for pull destination
type PullSelectionRequestMsg struct{}

// PullDestinationSelectedMsg is sent by the app when the user selected a workspace directory
type PullDestinationSelectedMsg struct {
	Path string
}

// PullSelectionCancelledMsg is sent by the app when the user cancelled workspace selection
type PullSelectionCancelledMsg struct{}

// clearFooterErrorCmd clears the footer error after 3 seconds (Rule 128)
func clearFooterErrorCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		return clearFooterErrorMsg{}
	})
}

// clearFooterErrorMsg is sent to clear the footer error after a delay
type clearFooterErrorMsg struct{}

// BrowserOpenedMsg is sent when the browser launch command has been started
type BrowserOpenedMsg struct {
	Error error
}
