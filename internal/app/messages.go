package app

import (
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/scan"
)

// Cross-view messages

// SwitchViewMsg requests a view change
type SwitchViewMsg struct {
	View command.ViewType
}

// ForgeAuthSuccessMsg indicates a successful authentication
type ForgeAuthSuccessMsg struct {
	Client *gitlabclient.Client
	User   *gitlabclient.User
}

// GitLabAuthFailedMsg indicates an authentication failure
type GitLabAuthFailedMsg struct {
	Error error
}

// GitLabLogoutMsg requests a logout
type GitLabLogoutMsg struct{}

// GroupsLoadedMsg holds the loaded groups
type GroupsLoadedMsg struct {
	Groups []*gitlabclient.Group
}

// GroupCreatedMsg indicates that a group was created
type GroupCreatedMsg struct {
	Group *gitlabclient.Group
}

// GroupDeletedMsg indicates that a group was deleted
type GroupDeletedMsg struct {
	ID int
}

// ProjectsLoadedMsg holds the loaded projects
type ProjectsLoadedMsg struct {
	Projects []*gitlabclient.Project
}

// ProjectCreatedMsg indicates that a project was created
type ProjectCreatedMsg struct {
	Project *gitlabclient.Project
}

// ProjectDeletedMsg indicates that a project was deleted
type ProjectDeletedMsg struct {
	ID int
}

// PullStartedMsg indicates the start of a synchronization
type PullStartedMsg struct{}

// PullProgressMsg holds the sync's progress
type PullProgressMsg struct {
	Current int
	Total   int
	Status  map[int]string
}

// PullCompletedMsg indicates the end of the synchronization
type PullCompletedMsg struct {
	Stats PullStats
}

// PullErrorMsg indicates an error during the sync
type PullErrorMsg struct {
	ProjectID int
	Error     error
}

// PullStats holds the synchronization statistics
type PullStats struct {
	Cloned  int
	Updated int
	Skipped int
	Failed  int
}

// WorkspaceScanResultLoadedMsg is sent when a cached workspace scan result has been loaded from disk
type WorkspaceScanResultLoadedMsg struct {
	Result   *scan.Result
	RepoPath string
	Err      error
}

// ImageScanResultLoadedMsg is sent when a cached image scan result has been loaded from disk
type ImageScanResultLoadedMsg struct {
	Result    *scan.Result
	ImageName string
	Err       error
}
