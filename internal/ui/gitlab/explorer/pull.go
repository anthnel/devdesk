package explorer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	gitlabclient "gitlab.com/gitlab-org/api/client-go"

	"github.com/anthnel/devdesk/internal/gitlab"
	"github.com/anthnel/devdesk/internal/ui/components"
)

// handlePullStart handles p key - start pull operation
func (m Model) handlePullStart(flatNodes []*TreeNode) (tea.Model, tea.Cmd) {
	cursor := m.table.Cursor()
	if cursor >= len(flatNodes) {
		return m, nil
	}

	node := flatNodes[cursor]
	m.pullTargetNode = node
	return m, func() tea.Msg { return PullSelectionRequestMsg{} }
}

// handlePullDestinationSelected handles workspace directory selection result from app
func (m Model) handlePullDestinationSelected(msg PullDestinationSelectedMsg) (tea.Model, tea.Cmd) {
	m.mode = ModePulling
	m.pullStatus = "Pulling..."

	node := m.pullTargetNode
	targetPath := msg.Path
	client := m.shared.GitLabClient
	cloneMethod := m.config.GitLab.CloneMethod
	gitlabURL := m.config.GitLab.URL

	return m, func() tea.Msg {
		report := m.recursivePull(client, node, targetPath, cloneMethod, gitlabURL)
		return PullCompleteMsg{Report: report}
	}
}

// handlePullComplete handles PullCompleteMsg
func (m Model) handlePullComplete(msg PullCompleteMsg) (tea.Model, tea.Cmd) {
	m.mode = ModeShowingReport
	m.pullTargetNode = nil
	m.pullStatus = ""
	m.reportModal = components.NewReportModal("Pull Complete", msg.Report)
	return m, nil
}

// recursivePull performs a recursive pull/clone operation
func (m Model) recursivePull(client *gitlabclient.Client, node *TreeNode, targetPath, cloneMethod, gitlabURL string) components.PullReport {
	report := components.PullReport{
		Cloned:  []string{},
		Skipped: []string{},
		Errors:  []string{},
	}

	m.recursivePullNode(client, node, targetPath, cloneMethod, gitlabURL, &report)
	return report
}

// recursivePullNode recursively processes a node
func (m Model) recursivePullNode(client *gitlabclient.Client, node *TreeNode, basePath, cloneMethod, gitlabURL string, report *components.PullReport) {
	if node.Type == NodeTypeProject {
		m.pullProject(node, basePath, cloneMethod, gitlabURL, report)
		return
	}

	// It's a group - create directory and process children using slug
	groupPath := filepath.Join(basePath, nodeSlug(node.FullPath))

	// Create directory
	if err := os.MkdirAll(groupPath, 0755); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("mkdir %s: %v", groupPath, err))
		return
	}

	// Load children if not already loaded
	if node.Children == nil {
		children, err := m.fetchGroupChildren(client, node)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("fetch %s: %v", node.FullPath, err))
			return
		}
		node.Children = children
	}

	// Process children recursively
	for _, child := range node.Children {
		m.recursivePullNode(client, child, groupPath, cloneMethod, gitlabURL, report)
	}
}

// pullProject clones or skips a single project
func (m Model) pullProject(node *TreeNode, basePath, cloneMethod, gitlabURL string, report *components.PullReport) {
	projectPath := filepath.Join(basePath, nodeSlug(node.FullPath))

	// Check if already exists
	if gitlab.DirExists(projectPath) {
		report.Skipped = append(report.Skipped, node.FullPath)
		return
	}

	if err := gitlab.Clone(cloneURL(gitlabURL, cloneMethod, node.FullPath), projectPath); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("clone %s: %v", node.FullPath, err))
		return
	}

	report.Cloned = append(report.Cloned, node.FullPath)
}

// cloneURL builds the git URL for a project from the configured GitLab URL and
// the clone method. SSH form takes the host alone, so the scheme is stripped.
func cloneURL(gitlabURL, cloneMethod, fullPath string) string {
	cleanURL := strings.TrimSuffix(gitlabURL, "/")
	if cloneMethod != "ssh" {
		return fmt.Sprintf("%s/%s.git", cleanURL, fullPath)
	}

	host := strings.TrimPrefix(cleanURL, "https://")
	host = strings.TrimPrefix(host, "http://")
	return fmt.Sprintf("git@%s:%s.git", host, fullPath)
}
