package app

import (
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"gitlab.com/anthnell/devsecops/devdesk/internal/command"
	"gitlab.com/anthnell/devsecops/devdesk/internal/scan"
)

// Messages inter-vues

// SwitchViewMsg demande un changement de vue
type SwitchViewMsg struct {
	View command.ViewType
}

// GitLabAuthSuccessMsg indique une authentification réussie
type GitLabAuthSuccessMsg struct {
	Client *gitlabclient.Client
	User   *gitlabclient.User
}

// GitLabAuthFailedMsg indique un échec d'authentification
type GitLabAuthFailedMsg struct {
	Error error
}

// GitLabLogoutMsg demande une déconnexion
type GitLabLogoutMsg struct{}

// GroupsLoadedMsg contient les groupes chargés
type GroupsLoadedMsg struct {
	Groups []*gitlabclient.Group
}

// GroupCreatedMsg indique qu'un groupe a été créé
type GroupCreatedMsg struct {
	Group *gitlabclient.Group
}

// GroupDeletedMsg indique qu'un groupe a été supprimé
type GroupDeletedMsg struct {
	ID int
}

// ProjectsLoadedMsg contient les projets chargés
type ProjectsLoadedMsg struct {
	Projects []*gitlabclient.Project
}

// ProjectCreatedMsg indique qu'un projet a été créé
type ProjectCreatedMsg struct {
	Project *gitlabclient.Project
}

// ProjectDeletedMsg indique qu'un projet a été supprimé
type ProjectDeletedMsg struct {
	ID int
}

// PullStartedMsg indique le début d'une synchronisation
type PullStartedMsg struct{}

// PullProgressMsg contient la progression de la synchro
type PullProgressMsg struct {
	Current int
	Total   int
	Status  map[int]string
}

// PullCompletedMsg indique la fin de la synchronisation
type PullCompletedMsg struct {
	Stats PullStats
}

// PullErrorMsg indique une erreur lors de la synchro
type PullErrorMsg struct {
	ProjectID int
	Error     error
}

// PullStats contient les statistiques de synchronisation
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
