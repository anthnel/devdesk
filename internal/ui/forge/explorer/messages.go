package explorer

import (
	"github.com/anthnel/devdesk/internal/forge"
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

// RootGroupsLoadedMsg is sent when the root groups are loaded
type RootGroupsLoadedMsg struct {
	Nodes []*TreeNode
}

// GroupCreatedMsg is sent when a group is created.
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

// ProjectCreatedMsg is sent when a project is created. See GroupCreatedMsg
// for why Target is carried.
type ProjectCreatedMsg struct {
	Repository forge.Repository
	Target     string
	Error      error
	// TemplateError is non-nil when the template's initial commit failed: the
	// repository exists, and is empty.
	TemplateError error
	// TemplateUnavailable marks Error as the template's, raised *before* any
	// repository was created — it could not be fetched, or is over the size
	// limits. Nothing exists on the forge, and the footer says so.
	TemplateUnavailable bool
}

// TemplateSelectionRequestMsg asks the app to lend the templates view so the
// user can choose one for the repository being created. It is the second time
// this view borrows another (see CloneSelectionRequestMsg), and the form it was
// asked from is kept as it is meanwhile.
type TemplateSelectionRequestMsg struct{}

// TemplateChosenMsg is the app's answer: the template picked.
type TemplateChosenMsg struct {
	Slug string
	Name string
}

// TemplateChoiceCancelledMsg is sent by the app when the user backed out of the
// picker. The form keeps whatever template it had.
type TemplateChoiceCancelledMsg struct{}

// ChildrenLoadedMsg is sent when a node's children are loaded
type ChildrenLoadedMsg struct {
	ParentNode *TreeNode
	Children   []*TreeNode
}

// LoadErrorMsg is sent when a loading error occurs
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
