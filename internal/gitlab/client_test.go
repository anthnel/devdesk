package gitlab

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	gitlabclient "gitlab.com/gitlab-org/api/client-go"
)

// recordedRequest is one call the GitLab client made to the fake API.
type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   string
}

// decode unmarshals the recorded JSON body into v.
func (r recordedRequest) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.Body), v); err != nil {
		t.Fatalf("decoding request body %q: %v", r.Body, err)
	}
}

// fakeGitLab is an httptest-backed GitLab API. It records every request so a
// test can assert on the payload the client built, then delegates to handler
// for the response.
type fakeGitLab struct {
	mu       sync.Mutex
	requests []recordedRequest
	server   *httptest.Server
}

// newFakeGitLab starts a fake API served by handler and shuts it down at the
// end of the test.
func newFakeGitLab(t *testing.T, handler http.HandlerFunc) *fakeGitLab {
	t.Helper()
	f := &fakeGitLab{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Body:   string(body),
		})
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

// client returns a GitLab client pointed at the fake API.
func (f *fakeGitLab) client(t *testing.T) *gitlabclient.Client {
	t.Helper()
	c, err := NewClient(f.server.URL, "test-token")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return c
}

// calls returns a copy of every request received so far.
func (f *fakeGitLab) calls() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recordedRequest, len(f.requests))
	copy(out, f.requests)
	return out
}

// only asserts exactly one request was received and returns it.
func (f *fakeGitLab) only(t *testing.T) recordedRequest {
	t.Helper()
	calls := f.calls()
	if len(calls) != 1 {
		t.Fatalf("got %d requests, want 1: %+v", len(calls), calls)
	}
	return calls[0]
}

// jsonHandler replies with status and the given JSON body.
func jsonHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestNewClientRejectsMalformedURL(t *testing.T) {
	if _, err := NewClient("://not-a-url", "token"); err == nil {
		t.Error("NewClient() with a malformed URL returned no error")
	}
}

func TestNewClientSendsTokenAsPrivateHeader(t *testing.T) {
	var gotToken string
	f := newFakeGitLab(t, func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		_, _ = w.Write([]byte(`{"id":1}`))
	})

	if _, err := TestConnection(f.client(t)); err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if gotToken != "test-token" {
		t.Errorf("PRIVATE-TOKEN header = %q, want %q", gotToken, "test-token")
	}
}

func TestTestConnectionReturnsCurrentUser(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusOK, `{"id":7,"username":"alice","name":"Alice"}`))

	user, err := TestConnection(f.client(t))

	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if user.ID != 7 || user.Username != "alice" {
		t.Errorf("user = %d/%q, want 7/alice", user.ID, user.Username)
	}
	if got := f.only(t); got.Path != "/api/v4/user" {
		t.Errorf("path = %q, want /api/v4/user", got.Path)
	}
}

func TestTestConnectionPropagatesAuthFailure(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusUnauthorized, `{"message":"401 Unauthorized"}`))

	user, err := TestConnection(f.client(t))

	if err == nil {
		t.Fatal("TestConnection() with a rejected token returned no error")
	}
	if user != nil {
		t.Errorf("user = %+v, want nil on error", user)
	}
}

func TestCreateGroupSendsParentIDOnlyWhenSet(t *testing.T) {
	tests := []struct {
		name          string
		parentID      int64
		wantParentSet bool
	}{
		{"top-level group omits parent_id", 0, false},
		{"negative parent id is ignored", -1, false},
		{"subgroup sends parent_id", 42, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeGitLab(t, jsonHandler(http.StatusCreated, `{"id":1,"name":"Infra"}`))

			group, err := CreateGroup(f.client(t), "Infra", "infra", "desc", "private", tt.parentID)

			if err != nil {
				t.Fatalf("CreateGroup() error = %v", err)
			}
			if group.ID != 1 {
				t.Errorf("group.ID = %d, want 1", group.ID)
			}

			req := f.only(t)
			if req.Method != http.MethodPost || req.Path != "/api/v4/groups" {
				t.Errorf("request = %s %s, want POST /api/v4/groups", req.Method, req.Path)
			}

			var body map[string]any
			req.decode(t, &body)
			for field, want := range map[string]any{
				"name":        "Infra",
				"path":        "infra",
				"description": "desc",
				"visibility":  "private",
			} {
				if body[field] != want {
					t.Errorf("body[%q] = %v, want %v", field, body[field], want)
				}
			}

			parent, ok := body["parent_id"]
			if ok != tt.wantParentSet {
				t.Fatalf("parent_id present = %v, want %v (body: %s)", ok, tt.wantParentSet, req.Body)
			}
			if tt.wantParentSet && parent != float64(tt.parentID) {
				t.Errorf("parent_id = %v, want %d", parent, tt.parentID)
			}
		})
	}
}

func TestCreateGroupPropagatesError(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusForbidden, `{"message":"403 Forbidden"}`))

	group, err := CreateGroup(f.client(t), "Infra", "infra", "", "private", 0)

	if err == nil {
		t.Fatal("CreateGroup() on a rejected request returned no error")
	}
	if group != nil {
		t.Errorf("group = %+v, want nil on error", group)
	}
}

func TestCreateProjectSendsNamespaceIDOnlyWhenSet(t *testing.T) {
	tests := []struct {
		name             string
		namespaceID      int64
		wantNamespaceSet bool
	}{
		{"personal namespace omits namespace_id", 0, false},
		{"group namespace sends namespace_id", 9, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeGitLab(t, jsonHandler(http.StatusCreated, `{"id":5,"name":"api"}`))

			project, err := CreateProject(f.client(t), "api", "api", "desc", "internal", tt.namespaceID)

			if err != nil {
				t.Fatalf("CreateProject() error = %v", err)
			}
			if project.ID != 5 {
				t.Errorf("project.ID = %d, want 5", project.ID)
			}

			req := f.only(t)
			if req.Method != http.MethodPost || req.Path != "/api/v4/projects" {
				t.Errorf("request = %s %s, want POST /api/v4/projects", req.Method, req.Path)
			}

			var body map[string]any
			req.decode(t, &body)
			if body["visibility"] != "internal" {
				t.Errorf("visibility = %v, want internal", body["visibility"])
			}

			ns, ok := body["namespace_id"]
			if ok != tt.wantNamespaceSet {
				t.Fatalf("namespace_id present = %v, want %v (body: %s)", ok, tt.wantNamespaceSet, req.Body)
			}
			if tt.wantNamespaceSet && ns != float64(tt.namespaceID) {
				t.Errorf("namespace_id = %v, want %d", ns, tt.namespaceID)
			}
		})
	}
}

func TestCreateProjectPropagatesError(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusBadRequest, `{"message":"path already taken"}`))

	if _, err := CreateProject(f.client(t), "api", "api", "", "private", 0); err == nil {
		t.Fatal("CreateProject() on a rejected request returned no error")
	}
}

func TestInitializeProjectWithFilesBuildsCommit(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusCreated, `{"id":"abc123"}`))

	err := InitializeProjectWithFiles(f.client(t), 12, []CommitAction{
		{Action: "create", FilePath: "README.md", Content: "# hello"},
		{Action: "create", FilePath: "docs/guide.md", Content: "guide"},
	})

	if err != nil {
		t.Fatalf("InitializeProjectWithFiles() error = %v", err)
	}

	req := f.only(t)
	if want := "/api/v4/projects/12/repository/commits"; req.Path != want {
		t.Errorf("path = %q, want %q", req.Path, want)
	}

	var body struct {
		Branch        string `json:"branch"`
		CommitMessage string `json:"commit_message"`
		Actions       []struct {
			Action   string `json:"action"`
			FilePath string `json:"file_path"`
			Content  string `json:"content"`
		} `json:"actions"`
	}
	req.decode(t, &body)

	if body.Branch != "main" {
		t.Errorf("branch = %q, want main", body.Branch)
	}
	if body.CommitMessage == "" {
		t.Error("commit_message is empty")
	}
	if len(body.Actions) != 2 {
		t.Fatalf("got %d actions, want 2", len(body.Actions))
	}
	// Order matters: the commit must apply files as the caller listed them.
	if body.Actions[0].FilePath != "README.md" || body.Actions[1].FilePath != "docs/guide.md" {
		t.Errorf("action paths = %q/%q, want README.md/docs/guide.md",
			body.Actions[0].FilePath, body.Actions[1].FilePath)
	}
	if body.Actions[0].Action != "create" || body.Actions[0].Content != "# hello" {
		t.Errorf("first action = %+v, want create/# hello", body.Actions[0])
	}
}

func TestInitializeProjectWithFilesAcceptsNoFiles(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusCreated, `{"id":"abc123"}`))

	if err := InitializeProjectWithFiles(f.client(t), 12, nil); err != nil {
		t.Fatalf("InitializeProjectWithFiles() with no files error = %v", err)
	}
}

func TestInitializeProjectWithFilesPropagatesError(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusBadRequest, `{"message":"invalid branch"}`))

	err := InitializeProjectWithFiles(f.client(t), 12, []CommitAction{
		{Action: "create", FilePath: "README.md", Content: "x"},
	})
	if err == nil {
		t.Fatal("InitializeProjectWithFiles() on a rejected request returned no error")
	}
}

func TestDeleteGroupScheduledDeletionIssuesOneRequest(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusAccepted, `{}`))

	if err := DeleteGroup(f.client(t), 42, "infra/tools", false); err != nil {
		t.Fatalf("DeleteGroup() error = %v", err)
	}

	req := f.only(t)
	if req.Method != http.MethodDelete || req.Path != "/api/v4/groups/42" {
		t.Errorf("request = %s %s, want DELETE /api/v4/groups/42", req.Method, req.Path)
	}
	if got := req.Query.Get("permanently_remove"); got != "" {
		t.Errorf("permanently_remove = %q, want it absent", got)
	}
}

func TestDeleteGroupPermanentUsesRenamedPath(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusAccepted, `{}`))

	if err := DeleteGroup(f.client(t), 42, "infra/tools", true); err != nil {
		t.Fatalf("DeleteGroup() error = %v", err)
	}

	calls := f.calls()
	if len(calls) != 2 {
		t.Fatalf("got %d requests, want 2 (schedule then purge): %+v", len(calls), calls)
	}
	if calls[0].Path != "/api/v4/groups/42" {
		t.Errorf("first request path = %q, want /api/v4/groups/42", calls[0].Path)
	}
	// GitLab renames a group pending deletion, so the purge must address it by
	// the suffixed path rather than by ID.
	want := "/api/v4/groups/infra/tools-deletion_scheduled-42"
	if calls[1].Path != want {
		t.Errorf("second request path = %q, want %q", calls[1].Path, want)
	}
	if got := calls[1].Query.Get("permanently_remove"); got != "true" {
		t.Errorf("permanently_remove = %q, want true", got)
	}
	if got := calls[1].Query.Get("full_path"); got != "infra/tools-deletion_scheduled-42" {
		t.Errorf("full_path = %q, want infra/tools-deletion_scheduled-42", got)
	}
}

func TestDeleteGroupPermanentStopsWhenScheduleFails(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusForbidden, `{"message":"403 Forbidden"}`))

	err := DeleteGroup(f.client(t), 42, "infra", true)

	if err == nil {
		t.Fatal("DeleteGroup() on a rejected schedule returned no error")
	}
	if n := len(f.calls()); n != 1 {
		t.Errorf("got %d requests, want 1 — the purge must not run after a failed schedule", n)
	}
}

func TestDeleteGroupPermanentReportsPurgeFailure(t *testing.T) {
	var n int
	f := newFakeGitLab(t, func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"404 Group Not Found"}`))
	})

	if err := DeleteGroup(f.client(t), 42, "infra", true); err == nil {
		t.Fatal("DeleteGroup() with a failing purge returned no error")
	}
}

func TestDeleteProjectScheduledDeletionIssuesOneRequest(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusAccepted, `{}`))

	if err := DeleteProject(f.client(t), 7, "infra/api", false); err != nil {
		t.Fatalf("DeleteProject() error = %v", err)
	}

	req := f.only(t)
	if req.Method != http.MethodDelete || req.Path != "/api/v4/projects/7" {
		t.Errorf("request = %s %s, want DELETE /api/v4/projects/7", req.Method, req.Path)
	}
}

func TestDeleteProjectPermanentUsesRenamedPath(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusAccepted, `{}`))

	if err := DeleteProject(f.client(t), 7, "infra/api", true); err != nil {
		t.Fatalf("DeleteProject() error = %v", err)
	}

	calls := f.calls()
	if len(calls) != 2 {
		t.Fatalf("got %d requests, want 2 (schedule then purge): %+v", len(calls), calls)
	}
	want := "/api/v4/projects/infra/api-deletion_scheduled-7"
	if calls[1].Path != want {
		t.Errorf("second request path = %q, want %q", calls[1].Path, want)
	}
	if got := calls[1].Query.Get("permanently_remove"); got != "true" {
		t.Errorf("permanently_remove = %q, want true", got)
	}
}

func TestDeleteProjectPermanentReportsPurgeFailure(t *testing.T) {
	var n int
	f := newFakeGitLab(t, func(w http.ResponseWriter, _ *http.Request) {
		n++
		if n == 1 {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"404 Project Not Found"}`))
	})

	if err := DeleteProject(f.client(t), 7, "infra/api", true); err == nil {
		t.Fatal("DeleteProject() with a failing purge returned no error")
	}
}

func TestDeleteProjectPermanentStopsWhenScheduleFails(t *testing.T) {
	f := newFakeGitLab(t, jsonHandler(http.StatusForbidden, `{"message":"403 Forbidden"}`))

	if err := DeleteProject(f.client(t), 7, "infra/api", true); err == nil {
		t.Fatal("DeleteProject() on a rejected schedule returned no error")
	}
	if n := len(f.calls()); n != 1 {
		t.Errorf("got %d requests, want 1 — the purge must not run after a failed schedule", n)
	}
}
