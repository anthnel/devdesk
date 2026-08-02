package gitlab

import (
	"net/http"
	"strconv"
	"testing"
)

// countHandler serves an empty list page and reports the supplied total through
// the X-Total header, which is where FetchDashboardStats reads its counts from.
func countHandler(total int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Total", strconv.Itoa(total))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}
}

// dashboardRoutes dispatches the five requests FetchDashboardStats makes. The
// two merge request calls share a path and differ only by query parameter.
func dashboardRoutes(mrAssigned, mrReview, issues, projects, groups int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v4/merge_requests" && r.URL.Query().Get("assignee_id") != "":
			countHandler(mrAssigned)(w, r)
		case r.URL.Path == "/api/v4/merge_requests" && r.URL.Query().Get("reviewer_id") != "":
			countHandler(mrReview)(w, r)
		case r.URL.Path == "/api/v4/issues":
			countHandler(issues)(w, r)
		case r.URL.Path == "/api/v4/projects":
			countHandler(projects)(w, r)
		case r.URL.Path == "/api/v4/groups":
			countHandler(groups)(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"unrouted"}`))
		}
	}
}

func TestFetchDashboardStatsMapsEachCount(t *testing.T) {
	f := newFakeGitLab(t, dashboardRoutes(3, 5, 11, 27, 4))

	got := FetchDashboardStats(f.client(t), 99)

	want := DashboardStats{
		AssignedMRs:    3,
		ReviewMRs:      5,
		AssignedIssues: 11,
		TotalProjects:  27,
		TotalGroups:    4,
	}
	if got != want {
		t.Errorf("FetchDashboardStats() = %+v, want %+v", got, want)
	}
	if n := len(f.calls()); n != 5 {
		t.Errorf("got %d requests, want 5", n)
	}
}

func TestFetchDashboardStatsScopesQueriesToTheUser(t *testing.T) {
	f := newFakeGitLab(t, dashboardRoutes(1, 1, 1, 1, 1))

	FetchDashboardStats(f.client(t), 99)

	var sawAssignedMR, sawReviewMR, sawAssignedIssue bool
	for _, req := range f.calls() {
		switch req.Path {
		case "/api/v4/merge_requests":
			if req.Query.Get("state") != "opened" {
				t.Errorf("merge request query state = %q, want opened", req.Query.Get("state"))
			}
			if req.Query.Get("assignee_id") == "99" {
				sawAssignedMR = true
			}
			if req.Query.Get("reviewer_id") == "99" {
				sawReviewMR = true
			}
		case "/api/v4/issues":
			if req.Query.Get("assignee_id") == "99" {
				sawAssignedIssue = true
			}
		case "/api/v4/projects":
			if req.Query.Get("membership") != "true" {
				t.Errorf("projects query membership = %q, want true", req.Query.Get("membership"))
			}
		}
	}

	if !sawAssignedMR {
		t.Error("no merge request query used assignee_id=99")
	}
	if !sawReviewMR {
		t.Error("no merge request query used reviewer_id=99")
	}
	if !sawAssignedIssue {
		t.Error("no issue query used assignee_id=99")
	}
}

func TestFetchDashboardStatsReturnsZeroesWhenEveryCallFails(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusUnauthorized, `{"message":"401 Unauthorized"}`))

	got := FetchDashboardStats(f.client(t), 99)

	if got != (DashboardStats{}) {
		t.Errorf("FetchDashboardStats() = %+v, want the zero value", got)
	}
}

func TestFetchDashboardStatsKeepsCountsFromSucceedingCalls(t *testing.T) {
	// Only the issues endpoint fails; the other four counts must survive.
	f := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v4/issues" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message":"403 Forbidden"}`))
			return
		}
		dashboardRoutes(3, 5, 0, 27, 4)(w, r)
	})

	got := FetchDashboardStats(f.client(t), 99)

	want := DashboardStats{AssignedMRs: 3, ReviewMRs: 5, AssignedIssues: 0, TotalProjects: 27, TotalGroups: 4}
	if got != want {
		t.Errorf("FetchDashboardStats() = %+v, want %+v", got, want)
	}
}
