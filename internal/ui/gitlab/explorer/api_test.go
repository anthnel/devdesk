package explorer

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/gitlab"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// The API commands are executed here rather than asserted on identity: the
// GitLab SDK takes a base URL, so an httptest server standing in for the API
// covers argument building and response mapping the way internal/gitlab does.
// A stub would only prove the stub works.

// fakeGitLab answers by path prefix, longest first. Anything unrouted 404s, so
// a request the test did not expect shows up as an error rather than as a
// silent empty list.
//
// Longest-first matters: "/api/v4/groups/1/members/all/1" prefix-matches both
// "/api/v4/groups" (the list) and "/api/v4/groups/1/members" (the lookup).
// Ranging over the map directly picked whichever came out first, so the member
// lookup decoded a group array roughly half the time and the access level came
// back 0. It passed locally and failed in CI.
type fakeGitLab struct {
	server   *httptest.Server
	prefixes []string
	routes   map[string]string
	paths    []string
}

func newFakeGitLab(t *testing.T, routes map[string]string) *fakeGitLab {
	t.Helper()
	f := &fakeGitLab{routes: routes, prefixes: slices.Sorted(maps.Keys(routes))}
	slices.SortFunc(f.prefixes, func(a, b string) int { return len(b) - len(a) })

	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.paths = append(f.paths, r.URL.Path)
		for _, prefix := range f.prefixes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(f.routes[prefix]))
				return
			}
		}
		http.Error(w, `{"message":"404 Not Found"}`, http.StatusNotFound)
	}))
	t.Cleanup(f.server.Close)
	return f
}

// serverModel returns a laid-out model whose client talks to the fake API.
func serverModel(t *testing.T, f *fakeGitLab) Model {
	t.Helper()
	client, err := gitlab.NewClient(f.server.URL, "test-token")
	if err != nil {
		t.Fatalf("gitlab.NewClient() error = %v", err)
	}
	state := &shared.State{GitLabClient: client, IsAuthenticated: true, CurrentUser: newUser("anthnel")}
	return feed(t, New(testConfig(), state), tea.WindowSizeMsg{Width: 160, Height: 30})
}

const (
	twoGroupsJSON = `[
		{"id":1,"name":"Infra","full_path":"infra","visibility":"private","web_url":"https://gl/infra"},
		{"id":2,"name":"Apps","full_path":"apps","visibility":"public","web_url":"https://gl/apps"}
	]`
	oneSubgroupJSON = `[{"id":10,"name":"Tools","full_path":"infra/tools","visibility":"internal","web_url":"https://gl/infra/tools"}]`
	oneProjectJSON  = `[{
		"id":11,"name":"API","path_with_namespace":"infra/api","visibility":"private",
		"web_url":"https://gl/infra/api","marked_for_deletion_on":"2026-07-01T00:00:00Z"
	}]`
	pipelineJSON = `[{"id":900,"status":"failed"}]`
	memberJSON   = `{"id":1,"username":"anthnel","access_level":40}`
)

// ── Loading ──────────────────────────────────────────────────────────────────

func TestLoadRootGroupsMapsTheResponse(t *testing.T) {
	f := newFakeGitLab(t, map[string]string{
		"/api/v4/groups/1/members": memberJSON, // inherited member lookup
		"/api/v4/groups/2/members": memberJSON,
		"/api/v4/groups":           twoGroupsJSON,
	})
	m := serverModel(t, f)

	msg, ok := testutil.MsgOf[RootGroupsLoadedMsg](m.loadRootGroups())
	if !ok {
		t.Fatalf("loadRootGroups() produced %T", testutil.Msg(m.loadRootGroups()))
	}

	if len(msg.Nodes) != 2 {
		t.Fatalf("loaded %d nodes, want 2", len(msg.Nodes))
	}
	first := msg.Nodes[0]
	if first.Name != "Infra" || first.FullPath != "infra" || first.Type != NodeTypeGroup {
		t.Errorf("first node = %+v", first)
	}
	if first.Visibility != "private" || first.WebURL != "https://gl/infra" {
		t.Errorf("metadata not mapped: visibility=%q url=%q", first.Visibility, first.WebURL)
	}
	if first.Children != nil {
		t.Error("root groups arrived with children; they are meant to load lazily")
	}
	if first.AccessLevel != 40 {
		t.Errorf("AccessLevel = %d, want the inherited member's 40", first.AccessLevel)
	}
}

// Only top-level groups belong at the root; the whole tree would arrive flat
// otherwise.
func TestLoadRootGroupsAsksForTopLevelOnly(t *testing.T) {
	f := newFakeGitLab(t, map[string]string{"/api/v4/groups": `[]`})
	m := serverModel(t, f)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("top_level_only"); got != "true" {
			t.Errorf("top_level_only = %q, want true", got)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client, _ := gitlab.NewClient(server.URL, "t")
	m.shared.GitLabClient = client
	testutil.Msg(m.loadRootGroups())
}

func TestLoadRootGroupsReportsAFailure(t *testing.T) {
	f := newFakeGitLab(t, nil) // everything 404s
	m := serverModel(t, f)

	msg, ok := testutil.MsgOf[LoadErrorMsg](m.loadRootGroups())
	if !ok {
		t.Fatalf("a failing API produced %T, want LoadErrorMsg", testutil.Msg(m.loadRootGroups()))
	}
	if msg.Error == nil {
		t.Error("LoadErrorMsg carries no error")
	}
}

// Children are subgroups first, then projects — the order the drill-down list
// is built in before sorting.
func TestLoadChildrenMergesSubgroupsAndProjects(t *testing.T) {
	f := newFakeGitLab(t, map[string]string{
		"/api/v4/groups/1/subgroups":    oneSubgroupJSON,
		"/api/v4/groups/1/projects":     oneProjectJSON,
		"/api/v4/projects/11/pipelines": pipelineJSON,
		"/api/v4/projects/11/members":   memberJSON,
		"/api/v4/groups/10/members":     memberJSON,
	})
	m := serverModel(t, f)
	parent := &TreeNode{ID: 1, Name: "Infra", FullPath: "infra", Type: NodeTypeGroup}

	msg, ok := testutil.MsgOf[ChildrenLoadedMsg](m.loadChildren(parent))
	if !ok {
		t.Fatalf("loadChildren() produced %T", testutil.Msg(m.loadChildren(parent)))
	}

	if len(msg.Children) != 2 {
		t.Fatalf("loaded %d children, want the subgroup and the project", len(msg.Children))
	}
	sub, project := msg.Children[0], msg.Children[1]
	if sub.Type != NodeTypeGroup || sub.Parent != parent {
		t.Errorf("subgroup = %+v, want a group parented on Infra", sub)
	}
	if project.Type != NodeTypeProject || project.FullPath != "infra/api" {
		t.Errorf("project = %+v", project)
	}
	if project.PipelineStatus != "failed" {
		t.Errorf("PipelineStatus = %q, want the last pipeline's", project.PipelineStatus)
	}
	if !project.MarkedForDeletion {
		t.Error("a project scheduled for deletion is not marked as such")
	}
}

// Either half of the fetch can fail, and the parent node has to be named in the
// error so its spinner is released.
func TestLoadChildrenReportsEitherHalfFailing(t *testing.T) {
	tests := map[string]map[string]string{
		"subgroups fail": {"/api/v4/groups/1/projects": `[]`},
		"projects fail":  {"/api/v4/groups/1/subgroups": `[]`},
	}

	for name, routes := range tests {
		t.Run(name, func(t *testing.T) {
			m := serverModel(t, newFakeGitLab(t, routes))
			parent := &TreeNode{ID: 1, Type: NodeTypeGroup}

			msg, ok := testutil.MsgOf[LoadErrorMsg](m.loadChildren(parent))
			if !ok {
				t.Fatal("a failing fetch produced no LoadErrorMsg")
			}
			if msg.ParentNode != parent {
				t.Error("LoadErrorMsg does not name the node that was loading")
			}
		})
	}
}

// fetchGroupChildren is the same fetch, run synchronously inside a recursive
// pull rather than as a command.
func TestFetchGroupChildren(t *testing.T) {
	f := newFakeGitLab(t, map[string]string{
		"/api/v4/groups/1/subgroups":    oneSubgroupJSON,
		"/api/v4/groups/1/projects":     oneProjectJSON,
		"/api/v4/projects/11/pipelines": pipelineJSON,
		"/api/v4/projects/11/members":   memberJSON,
	})
	m := serverModel(t, f)
	parent := &TreeNode{ID: 1, Type: NodeTypeGroup}

	children, err := m.fetchGroupChildren(m.shared.GitLabClient, parent)

	if err != nil {
		t.Fatalf("fetchGroupChildren() error = %v", err)
	}
	if len(children) != 2 {
		t.Errorf("fetched %d children, want 2", len(children))
	}
}

func TestFetchGroupChildrenPropagatesFailure(t *testing.T) {
	m := serverModel(t, newFakeGitLab(t, nil))

	if _, err := m.fetchGroupChildren(m.shared.GitLabClient, &TreeNode{ID: 1}); err == nil {
		t.Error("fetchGroupChildren() returned no error against a failing API")
	}
}

// ── Access level and pipeline lookups ────────────────────────────────────────

// These decorate rows; a failure has to degrade to a blank cell rather than
// abort the whole load.
func TestDecoratorsDegradeQuietly(t *testing.T) {
	m := serverModel(t, newFakeGitLab(t, nil)) // everything 404s
	client := m.shared.GitLabClient

	if got := fetchLastPipelineStatus(client, 11); got != "" {
		t.Errorf("fetchLastPipelineStatus() = %q against a failing API", got)
	}
	if got := fetchGroupAccessLevel(client, 1, 1); got != 0 {
		t.Errorf("fetchGroupAccessLevel() = %d against a failing API", got)
	}
	if got := fetchProjectAccessLevel(client, 11, 1); got != 0 {
		t.Errorf("fetchProjectAccessLevel() = %d against a failing API", got)
	}
}

// An anonymous session has no membership to look up, so the call is skipped
// entirely rather than issued and discarded.
func TestAccessLevelIsSkippedWithoutAUser(t *testing.T) {
	f := newFakeGitLab(t, map[string]string{"/api/v4": memberJSON})
	m := serverModel(t, f)

	fetchGroupAccessLevel(m.shared.GitLabClient, 1, 0)
	fetchProjectAccessLevel(m.shared.GitLabClient, 11, 0)

	if len(f.paths) != 0 {
		t.Errorf("requests issued for an anonymous session: %v", f.paths)
	}
}

func TestPipelineStatusIsEmptyWithoutPipelines(t *testing.T) {
	f := newFakeGitLab(t, map[string]string{"/api/v4/projects/11/pipelines": `[]`})
	m := serverModel(t, f)

	if got := fetchLastPipelineStatus(m.shared.GitLabClient, 11); got != "" {
		t.Errorf("fetchLastPipelineStatus() = %q for a project with no pipelines", got)
	}
}

// ── Creation ─────────────────────────────────────────────────────────────────

// The path is derived from the name, so "My New Group" becomes a usable slug
// rather than a rejected one.
func TestCreateGroupSlugsTheName(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err == nil {
			body = r.Form.Encode()
		}
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		body += string(buf)
		_, _ = w.Write([]byte(`{"id":5,"full_path":"my-new-group"}`))
	}))
	defer server.Close()

	client, _ := gitlab.NewClient(server.URL, "t")
	m := New(testConfig(), &shared.State{GitLabClient: client})

	msg, ok := testutil.MsgOf[GroupCreatedMsg](m.createGroup(creationSubmit("My New Group")))

	if !ok {
		t.Fatal("createGroup() produced no GroupCreatedMsg")
	}
	if msg.Error != nil {
		t.Fatalf("createGroup() error = %v", msg.Error)
	}
	if !strings.Contains(body, "my-new-group") {
		t.Errorf("the request body does not carry the slug: %s", body)
	}
}

func TestCreateGroupReportsAFailure(t *testing.T) {
	m := serverModel(t, newFakeGitLab(t, nil))

	msg, _ := testutil.MsgOf[GroupCreatedMsg](m.createGroup(creationSubmit("x")))

	if msg.Error == nil {
		t.Error("createGroup() reported no error against a failing API")
	}
}

// With no template selected the project is created and nothing else happens —
// the OCI registry is not touched.
func TestCreateProjectWithoutATemplate(t *testing.T) {
	f := newFakeGitLab(t, map[string]string{"/api/v4/projects": `{"id":9,"path_with_namespace":"infra/svc"}`})
	m := serverModel(t, f)

	msg, ok := testutil.MsgOf[ProjectCreatedMsg](m.createProject(creationSubmit("svc")))

	if !ok {
		t.Fatal("createProject() produced no ProjectCreatedMsg")
	}
	if msg.Error != nil || msg.TemplateError != nil {
		t.Errorf("createProject() error = %v, template error = %v", msg.Error, msg.TemplateError)
	}
	if msg.Project == nil || msg.Project.PathWithNamespace != "infra/svc" {
		t.Errorf("created project = %+v", msg.Project)
	}
}

func TestCreateProjectReportsAFailure(t *testing.T) {
	m := serverModel(t, newFakeGitLab(t, nil))

	msg, _ := testutil.MsgOf[ProjectCreatedMsg](m.createProject(creationSubmit("svc")))

	if msg.Error == nil {
		t.Error("createProject() reported no error against a failing API")
	}
}

// ── Templates ────────────────────────────────────────────────────────────────

// An unconfigured registry is the common case, and must not be treated as a
// failure: the form opens with no templates and no warning.
func TestLoadTemplatesWithoutARegistryIsSilent(t *testing.T) {
	m := New(testConfig(), &shared.State{})

	msg, ok := testutil.MsgOf[TemplatesLoadedMsg](m.loadTemplates())

	if !ok {
		t.Fatal("loadTemplates() produced no TemplatesLoadedMsg")
	}
	if msg.Error != nil || len(msg.Templates) != 0 {
		t.Errorf("an unconfigured registry produced error=%v templates=%v", msg.Error, msg.Templates)
	}
}

func TestLoadTemplatesReportsARegistryFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := testConfig()
	cfg.Registry.URL = server.URL
	cfg.Registry.TemplatesRepository = "templates"
	m := New(cfg, &shared.State{})

	msg, _ := testutil.MsgOf[TemplatesLoadedMsg](m.loadTemplates())

	if msg.Error == nil {
		t.Error("a failing registry produced no error")
	}
}
