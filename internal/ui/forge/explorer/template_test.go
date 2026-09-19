package explorer

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	gitlabforge "github.com/anthnel/devdesk/internal/forge/gitlab"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/template"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// ── Fixtures ─────────────────────────────────────────────────────────────────

// gitRepo commits the given files in a fresh repository and returns its path.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet", "--initial-branch=main")
	// No line-ending conversion, whatever the host's core.autocrlf says.
	files[".gitattributes"] = "* -text\n"
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", ".")
	run("commit", "--quiet", "-m", "init")
	return dir
}

// catalog writes a catalog holding one local template and returns its path.
func catalog(t *testing.T, slug, repo string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "templates.yaml")
	store, err := template.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Put(template.Entry{Slug: slug, Name: "Fixture", Source: template.Source{Kind: template.KindLocal, Path: repo}})
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// recordingGitLab serves a project creation and a commit, recording what each
// request was. The routes are exact: an unexpected one is a 404, which is what
// a commit that was never supposed to happen looks like.
type recordingGitLab struct {
	mu       sync.Mutex
	requests []string
	commit   []byte
	server   *httptest.Server
}

func newRecordingGitLab(t *testing.T, commitStatus int) *recordingGitLab {
	t.Helper()
	g := &recordingGitLab{}
	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		g.mu.Lock()
		g.requests = append(g.requests, r.Method+" "+r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/repository/commits") {
			g.commit = body
		}
		g.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v4/projects":
			_, _ = w.Write([]byte(`{"id":9,"path_with_namespace":"infra/svc"}`))
		case strings.HasSuffix(r.URL.Path, "/repository/commits"):
			w.WriteHeader(commitStatus)
			_, _ = w.Write([]byte(`{"id":"abc","message":"commit"}`))
		default:
			http.Error(w, `{"message":"404 Not Found"}`, http.StatusNotFound)
		}
	}))
	t.Cleanup(g.server.Close)
	return g
}

func (g *recordingGitLab) seen() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.requests)
}

func modelFor(t *testing.T, g *recordingGitLab, catalogPath string) Model {
	t.Helper()
	backend, err := gitlabforge.New(g.server.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	m := New(testConfig(), &shared.State{Forge: backend, IsAuthenticated: true})
	m.templatesPath = catalogPath
	m.templateCache = template.NewCacheAt(t.TempDir())
	return m
}

// ── Applying a template ──────────────────────────────────────────────────────

func TestATemplateFillsTheNewRepositoryInOneCommit(t *testing.T) {
	repo := gitRepo(t, map[string]string{"README.md": "hello\n", "Makefile": "all:\n"})
	g := newRecordingGitLab(t, http.StatusCreated)
	m := modelFor(t, g, catalog(t, "fixture", repo))

	sub := creationSubmit("svc")
	sub.Template = "fixture"
	msg, _ := testutil.MsgOf[ProjectCreatedMsg](m.createProject(sub, "svc"))

	if msg.Error != nil || msg.TemplateError != nil {
		t.Fatalf("error = %v, template error = %v", msg.Error, msg.TemplateError)
	}
	if got, want := g.seen(), []string{"POST /api/v4/projects", "POST /api/v4/projects/9/repository/commits"}; !slices.Equal(got, want) {
		t.Errorf("requests = %v, want the repository, then one commit %v", got, want)
	}

	var body struct {
		Actions []struct {
			FilePath string `json:"file_path"`
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
		} `json:"actions"`
	}
	if err := json.Unmarshal(g.commit, &body); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, a := range body.Actions {
		raw, _ := base64.StdEncoding.DecodeString(a.Content)
		got[a.FilePath] = string(raw)
	}
	if got["README.md"] != "hello\n" || got["Makefile"] != "all:\n" {
		t.Errorf("committed files = %v", got)
	}
}

// Decision 3b: the template is fetched before the repository exists, so a
// template that cannot be fetched creates nothing.
func TestAnUnavailableTemplateCreatesNothing(t *testing.T) {
	g := newRecordingGitLab(t, http.StatusCreated)
	missing := filepath.Join(t.TempDir(), "gone")
	m := modelFor(t, g, catalog(t, "fixture", missing))

	sub := creationSubmit("svc")
	sub.Template = "fixture"
	msg, _ := testutil.MsgOf[ProjectCreatedMsg](m.createProject(sub, "svc"))

	if msg.Error == nil || !msg.TemplateUnavailable {
		t.Fatalf("msg = %+v, want the template's failure", msg)
	}
	if len(g.seen()) != 0 {
		t.Errorf("the forge was called (%v) although the template could not be fetched", g.seen())
	}
}

func TestATemplateNoLongerInTheCatalogCreatesNothing(t *testing.T) {
	g := newRecordingGitLab(t, http.StatusCreated)
	m := modelFor(t, g, catalog(t, "other", t.TempDir()))

	sub := creationSubmit("svc")
	sub.Template = "removed"
	msg, _ := testutil.MsgOf[ProjectCreatedMsg](m.createProject(sub, "svc"))

	if msg.Error == nil || !msg.TemplateUnavailable || !strings.Contains(msg.Error.Error(), "no longer in the catalog") {
		t.Errorf("msg = %+v, want it to say the template is gone", msg)
	}
	if len(g.seen()) != 0 {
		t.Errorf("the forge was called: %v", g.seen())
	}
}

// A template over the limits is refused before the repository exists, too.
func TestATemplateOverTheLimitsCreatesNothing(t *testing.T) {
	files := map[string]string{}
	for i := 0; i <= template.MaxFiles; i++ {
		files["f"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))+string(rune('a'+(i/676)%26))] = "x"
	}
	if len(files) <= template.MaxFiles {
		t.Fatalf("fixture has only %d files", len(files))
	}
	repo := gitRepo(t, files)
	g := newRecordingGitLab(t, http.StatusCreated)
	m := modelFor(t, g, catalog(t, "fixture", repo))

	sub := creationSubmit("svc")
	sub.Template = "fixture"
	msg, _ := testutil.MsgOf[ProjectCreatedMsg](m.createProject(sub, "svc"))

	if msg.Error == nil || !msg.TemplateUnavailable {
		t.Fatalf("msg = %+v, want the size refusal", msg)
	}
	if len(g.seen()) != 0 {
		t.Errorf("the forge was called: %v", g.seen())
	}
}

// What can still fail after the repository exists is the commit. That keeps the
// old outcome: the repository stays, and the message says the template did not
// apply.
func TestAFailedCommitLeavesTheRepositoryAndSaysSo(t *testing.T) {
	repo := gitRepo(t, map[string]string{"README.md": "hello\n"})
	// A 4xx, not a 5xx: the GitLab client retries a 5xx with a backoff, and the
	// test would wait it out.
	g := newRecordingGitLab(t, http.StatusForbidden)
	m := modelFor(t, g, catalog(t, "fixture", repo))

	sub := creationSubmit("svc")
	sub.Template = "fixture"
	msg, _ := testutil.MsgOf[ProjectCreatedMsg](m.createProject(sub, "svc"))

	if msg.Error != nil {
		t.Fatalf("Error = %v: the repository was created, so this is not a creation failure", msg.Error)
	}
	if msg.TemplateError == nil || msg.Repository.ID == "" {
		t.Errorf("msg = %+v, want the repository and the template's error", msg)
	}
}

// No template: no catalog is read, and no commit is made.
func TestNoTemplateMakesNoCommitAndReadsNoCatalog(t *testing.T) {
	g := newRecordingGitLab(t, http.StatusCreated)
	m := modelFor(t, g, filepath.Join(t.TempDir(), "does-not-exist.yaml"))

	msg, _ := testutil.MsgOf[ProjectCreatedMsg](m.createProject(creationSubmit("svc"), "svc"))

	if msg.Error != nil || msg.TemplateError != nil {
		t.Fatalf("msg = %+v", msg)
	}
	if got := g.seen(); !slices.Equal(got, []string{"POST /api/v4/projects"}) {
		t.Errorf("requests = %v, want only the repository", got)
	}
}

// ── Reporting ────────────────────────────────────────────────────────────────

func TestAnUnavailableTemplateIsReportedAsNothingCreated(t *testing.T) {
	m := serverModel(t, newFakeGitLab(t, nil))
	m = feed(t, m, ProjectCreatedMsg{Target: "svc", Error: io.EOF, TemplateUnavailable: true})

	if !strings.Contains(m.footer.Text(), "was not created") {
		t.Errorf("footer = %q, want it to say nothing was created", m.footer.Text())
	}

	tr := ProjectCreatedMsg{Target: "svc", Error: io.EOF, TemplateUnavailable: true}.Transition()
	if !strings.Contains(tr.Detail, "nothing was created") {
		t.Errorf("run detail = %q", tr.Detail)
	}
}

// A repository that exists but is empty is not a success of a create that asked
// for a template: the run says so, and the detail says why.
func TestACreateWhoseTemplateCommitFailedIsAFailedItem(t *testing.T) {
	tr := ProjectCreatedMsg{Target: "svc", TemplateError: io.EOF}.Transition()
	if tr.State != jobs.ItemFailed || !strings.Contains(tr.Detail, "created empty") {
		t.Errorf("transition = %+v, want failed, naming the empty repository", tr)
	}
}
