package gitlab

import (
	"context"

	gitlabclient "gitlab.com/gitlab-org/api/client-go"
)

// DashboardStats contains aggregated GitLab statistics
type DashboardStats struct {
	AssignedMRs    int
	ReviewMRs      int
	AssignedIssues int
	TotalProjects  int
	TotalGroups    int
}

// FetchDashboardStats fetches GitLab statistics for the dashboard
func FetchDashboardStats(c *gitlabclient.Client, userID int64) DashboardStats {
	stats := DashboardStats{}
	ctx := context.Background()

	// Fetch assigned MRs — use TotalItems from X-Total header for accurate count
	if _, resp, err := c.MergeRequests.ListMergeRequests(&gitlabclient.ListMergeRequestsOptions{
		State:      gitlabclient.Ptr("opened"),
		AssigneeID: gitlabclient.AssigneeID(userID),
		ListOptions: gitlabclient.ListOptions{
			PerPage: 1,
		},
	}, gitlabclient.WithContext(ctx)); err == nil {
		stats.AssignedMRs = int(resp.TotalItems)
	}

	// Fetch MRs awaiting review
	if _, resp, err := c.MergeRequests.ListMergeRequests(&gitlabclient.ListMergeRequestsOptions{
		State:      gitlabclient.Ptr("opened"),
		ReviewerID: gitlabclient.ReviewerID(userID),
		ListOptions: gitlabclient.ListOptions{
			PerPage: 1,
		},
	}, gitlabclient.WithContext(ctx)); err == nil {
		stats.ReviewMRs = int(resp.TotalItems)
	}

	// Fetch assigned issues
	if _, resp, err := c.Issues.ListIssues(&gitlabclient.ListIssuesOptions{
		State:      gitlabclient.Ptr("opened"),
		AssigneeID: gitlabclient.AssigneeID(userID),
		ListOptions: gitlabclient.ListOptions{
			PerPage: 1,
		},
	}, gitlabclient.WithContext(ctx)); err == nil {
		stats.AssignedIssues = int(resp.TotalItems)
	}

	// Fetch project count
	if _, resp, err := c.Projects.ListProjects(&gitlabclient.ListProjectsOptions{
		Membership: gitlabclient.Ptr(true),
		ListOptions: gitlabclient.ListOptions{
			PerPage: 1,
		},
	}, gitlabclient.WithContext(ctx)); err == nil {
		stats.TotalProjects = int(resp.TotalItems)
	}

	// Fetch group count
	if _, resp, err := c.Groups.ListGroups(&gitlabclient.ListGroupsOptions{
		ListOptions: gitlabclient.ListOptions{
			PerPage: 1,
		},
	}, gitlabclient.WithContext(ctx)); err == nil {
		stats.TotalGroups = int(resp.TotalItems)
	}

	return stats
}
