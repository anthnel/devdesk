package explorer

import (
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/oci"
)

// DeleteCompleteMsg is sent when a delete operation completes.
//
// Target is the path the run was registered under, and it is carried rather
// than read back off DeletedNode: a transition has to name its item even when
// the node is gone, and the two would be free to disagree.
type DeleteCompleteMsg struct {
	Error       error
	Target      string
	DeletedNode *TreeNode
}

// RootGroupsLoadedMsg est envoyé quand les groupes racine sont chargés
type RootGroupsLoadedMsg struct {
	Nodes []*TreeNode
}

// GroupCreatedMsg est envoyé quand un groupe est créé.
//
// Target is the path the placeholder row and the run were both keyed on — the
// one this view predicted from the parent and the slug. The forge is free to
// answer with a different path, which is why the prediction has to travel: it
// is the only thing that can still find the row that was put on screen.
type GroupCreatedMsg struct {
	Namespace forge.Namespace
	Target    string
	Error     error
}

// ProjectCreatedMsg est envoyé quand un projet est créé. See GroupCreatedMsg
// for why Target is carried.
type ProjectCreatedMsg struct {
	Repository    forge.Repository
	Target        string
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

// BrowserOpenedMsg is sent when the browser launch command has been started
type BrowserOpenedMsg struct {
	Error error
}
