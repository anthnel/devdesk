package explorer

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The pipeline is run for real against throwaway git repositories and a
// temporary directory: it is what proves the tree is mirrored on disk and that
// an existing checkout is skipped rather than clobbered. Only the clone URL is
// synthetic — the "GitLab host" is a local path.

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
}

// seedRemote creates a git repository that can be cloned from, at
// <root>/<fullPath>.git — the layout cloneOne builds its HTTPS URL for.
func seedRemote(t *testing.T, root, fullPath string) {
	t.Helper()
	dir := filepath.Join(root, fullPath+".git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating the remote: %v", err)
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# seed\n"), 0o600); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	run("add", "README.md")
	run("-c", "user.name=devdesk", "-c", "user.email=devdesk@example.com",
		"-c", "commit.gpgsign=false", "commit", "-m", "seed")
}

// selecting returns a selection with those paths ticked.
func selecting(paths ...string) cloneSelection {
	s := newCloneSelection()
	for _, p := range paths {
		s.toggle(p)
	}
	return s
}

// collect drains a run and returns its events, keyed by path in arrival order.
// It fails rather than hangs: a pipeline that never closes its channel is the
// failure mode worth catching, and a test that hangs says nothing.
func collect(t *testing.T, run *cloneRun) []cloneEvent {
	t.Helper()
	var events []cloneEvent
	deadline := time.After(30 * time.Second)
	for {
		select {
		case event, ok := <-run.events:
			if !ok {
				return events
			}
			events = append(events, event)
		case <-deadline:
			t.Fatalf("the run did not finish; %d events so far", len(events))
		}
	}
}

// endedFor returns the terminal event for a path, or false if there was none.
func endedFor(events []cloneEvent, path string) (cloneEvent, bool) {
	for _, e := range events {
		if e.path == path && (e.kind == cloneEnded || e.kind == cloneWalkFailed) {
			return e, true
		}
	}
	return cloneEvent{}, false
}

func baseSpec(remotes, target string) cloneSpec {
	return cloneSpec{
		target:      target,
		cloneMethod: "https",
		gitlabURL:   remotes,
		jobs:        2,
	}
}

// ── Cloning ──────────────────────────────────────────────────────────────────

func TestThePipelineClonesASelectedProject(t *testing.T) {
	requireGit(t)
	remotes, target := t.TempDir(), t.TempDir()
	seedRemote(t, remotes, "acme/api")

	spec := baseSpec(remotes, target)
	spec.selection = selecting("acme/api")
	spec.roots = []*TreeNode{{Name: "API", FullPath: "acme/api", Type: NodeTypeProject}}

	events := collect(t, startCloneRun(spec))

	ended, ok := endedFor(events, "acme/api")
	if !ok {
		t.Fatalf("no terminal event for the project; events = %+v", events)
	}
	if ended.err != nil || ended.skipped {
		t.Errorf("the clone reported err=%v skipped=%v", ended.err, ended.skipped)
	}
	if _, err := os.Stat(filepath.Join(target, "acme", "api", "README.md")); err != nil {
		t.Errorf("the clone is missing its contents: %v", err)
	}
}

// Decision 4: the destination mirrors the forge path in full. Keeping only the
// last segment made two groups' "platform" land on one directory and merge —
// which could not happen while one subtree was cloned at a time, and can now
// that several roots are confirmed at once.
func TestTwoRootsSharingALeafNameDoNotCollide(t *testing.T) {
	requireGit(t)
	remotes, target := t.TempDir(), t.TempDir()
	seedRemote(t, remotes, "acme/platform")
	seedRemote(t, remotes, "other/platform")

	spec := baseSpec(remotes, target)
	spec.selection = selecting("acme/platform", "other/platform")
	spec.roots = []*TreeNode{
		{Name: "platform", FullPath: "acme/platform", Type: NodeTypeProject},
		{Name: "platform", FullPath: "other/platform", Type: NodeTypeProject},
	}

	collect(t, startCloneRun(spec))

	for _, want := range []string{"acme/platform", "other/platform"} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(want), "README.md")); err != nil {
			t.Errorf("%s was not cloned into its own directory: %v", want, err)
		}
	}
}

// Decision 1: what is already on disk is skipped untouched. Updating it is the
// workspaces view's job (§3.17).
func TestAnExistingCheckoutIsSkipped(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "acme", "api"), 0o755); err != nil {
		t.Fatalf("seeding the destination: %v", err)
	}

	spec := baseSpec("https://gl.example.com", target)
	spec.selection = selecting("acme/api")
	spec.roots = []*TreeNode{{FullPath: "acme/api", Type: NodeTypeProject}}

	events := collect(t, startCloneRun(spec))

	ended, ok := endedFor(events, "acme/api")
	if !ok {
		t.Fatalf("no terminal event; events = %+v", events)
	}
	if !ended.skipped || ended.err != nil {
		t.Errorf("skipped=%v err=%v, want it left alone", ended.skipped, ended.err)
	}
}

func TestAFailedCloneIsReportedAndTheRunGoesOn(t *testing.T) {
	requireGit(t)
	remotes, target := t.TempDir(), t.TempDir()
	seedRemote(t, remotes, "acme/api")

	spec := baseSpec(remotes, target)
	spec.selection = selecting("acme/api", "acme/missing")
	spec.roots = []*TreeNode{
		{FullPath: "acme/missing", Type: NodeTypeProject},
		{FullPath: "acme/api", Type: NodeTypeProject},
	}

	events := collect(t, startCloneRun(spec))

	failed, ok := endedFor(events, "acme/missing")
	if !ok || failed.err == nil {
		t.Fatalf("the missing repository was not reported as a failure: %+v", failed)
	}
	if good, ok := endedFor(events, "acme/api"); !ok || good.err != nil {
		t.Errorf("one failure stopped the run: %+v", good)
	}
}

// Every repository is announced as found before it is cloned — that is what
// makes the list fill while the work happens rather than after it (decision 7).
func TestARepositoryIsAnnouncedBeforeItIsCloned(t *testing.T) {
	requireGit(t)
	remotes, target := t.TempDir(), t.TempDir()
	seedRemote(t, remotes, "acme/api")

	spec := baseSpec(remotes, target)
	spec.selection = selecting("acme/api")
	spec.roots = []*TreeNode{{FullPath: "acme/api", Type: NodeTypeProject}}

	events := collect(t, startCloneRun(spec))

	var order []cloneEventKind
	for _, e := range events {
		order = append(order, e.kind)
	}
	want := []cloneEventKind{cloneFound, cloneBegan, cloneEnded}
	if len(order) != len(want) {
		t.Fatalf("events = %v, want found → began → ended", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("events = %v, want found → began → ended", order)
		}
	}
}

// ── Discovery ────────────────────────────────────────────────────────────────

// A ticked group is walked through the API, and its tree is mirrored on disk.
func TestThePipelineWalksAGroupAndMirrorsIt(t *testing.T) {
	requireGit(t)
	remotes, target := t.TempDir(), t.TempDir()
	seedRemote(t, remotes, "infra/api")
	seedRemote(t, remotes, "infra/tools/cli")

	f := newFakeGitLab(t, map[string]string{
		"/api/v4/groups/1/subgroups": `[{"id":2,"name":"Tools","full_path":"infra/tools"}]`,
		"/api/v4/groups/1/projects":  `[{"id":11,"name":"API","path_with_namespace":"infra/api"}]`,
		"/api/v4/groups/2/subgroups": `[]`,
		"/api/v4/groups/2/projects":  `[{"id":12,"name":"CLI","path_with_namespace":"infra/tools/cli"}]`,
	})
	m := serverModel(t, f)

	spec := baseSpec(remotes, target)
	spec.client = m.shared.GitLabClient
	spec.selection = selecting("infra")
	spec.roots = []*TreeNode{{ID: 1, Name: "Infra", FullPath: "infra", Type: NodeTypeGroup}}

	events := collect(t, startCloneRun(spec))

	for _, want := range []string{"infra/api", "infra/tools/cli"} {
		if ended, ok := endedFor(events, want); !ok || ended.err != nil {
			t.Errorf("%s was not cloned: %+v", want, ended)
		}
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(want), "README.md")); err != nil {
			t.Errorf("%s is not on disk: %v", want, err)
		}
	}
}

// An excluded subtree costs no requests at all: the walk does not enter it, so
// its projects are never even listed. That is what makes "this group, minus
// these" cheap enough to be the representation (decision 11).
func TestAnExcludedSubtreeIsNeverWalked(t *testing.T) {
	requireGit(t)
	remotes, target := t.TempDir(), t.TempDir()
	seedRemote(t, remotes, "infra/api")

	f := newFakeGitLab(t, map[string]string{
		"/api/v4/groups/1/subgroups": `[{"id":2,"name":"Legacy","full_path":"infra/legacy"}]`,
		"/api/v4/groups/1/projects":  `[{"id":11,"name":"API","path_with_namespace":"infra/api"}]`,
		"/api/v4/groups/2/subgroups": `[]`,
		"/api/v4/groups/2/projects":  `[{"id":12,"name":"Old","path_with_namespace":"infra/legacy/old"}]`,
	})
	m := serverModel(t, f)

	selection := selecting("infra")
	selection.toggle("infra/legacy") // drilled in, unticked

	spec := baseSpec(remotes, target)
	spec.client = m.shared.GitLabClient
	spec.selection = selection
	spec.roots = []*TreeNode{{ID: 1, FullPath: "infra", Type: NodeTypeGroup}}

	events := collect(t, startCloneRun(spec))

	if _, ok := endedFor(events, "infra/legacy/old"); ok {
		t.Error("the excluded subgroup's project was cloned")
	}
	if _, ok := endedFor(events, "infra/api"); !ok {
		t.Error("excluding one subgroup took its siblings with it")
	}
	for _, path := range f.paths() {
		if strings.Contains(path, "/groups/2/") {
			t.Errorf("the excluded subgroup was listed anyway: %s", path)
		}
	}
}

// A group that cannot be listed is reported rather than dropped: the
// repositories under it were never discovered, and no other row can stand for
// them.
func TestAFailedWalkIsReported(t *testing.T) {
	m := serverModel(t, newFakeGitLab(t, nil)) // everything 404s

	spec := baseSpec("https://gl", t.TempDir())
	spec.client = m.shared.GitLabClient
	spec.selection = selecting("infra")
	spec.roots = []*TreeNode{{ID: 1, FullPath: "infra", Type: NodeTypeGroup}}

	events := collect(t, startCloneRun(spec))

	failed, ok := endedFor(events, "infra")
	if !ok || failed.kind != cloneWalkFailed {
		t.Fatalf("events = %+v, want the failed walk reported", events)
	}
}

// The walk writes nothing onto the nodes it is given. They are the tree the
// view renders, and discovery nodes carry none of its decoration — storing them
// would blank the role and CI columns, and the write itself would race Update
// (Rule 110).
func TestTheWalkLeavesTheTreeAlone(t *testing.T) {
	f := newFakeGitLab(t, map[string]string{
		"/api/v4/groups/1/subgroups": `[]`,
		"/api/v4/groups/1/projects":  `[{"id":11,"name":"API","path_with_namespace":"infra/api"}]`,
	})
	m := serverModel(t, f)

	root := &TreeNode{ID: 1, FullPath: "infra", Type: NodeTypeGroup} // Children nil
	spec := baseSpec(t.TempDir(), t.TempDir())
	spec.client = m.shared.GitLabClient
	spec.selection = selecting("infra")
	spec.roots = []*TreeNode{root}

	collect(t, startCloneRun(spec))

	if root.Children != nil {
		t.Error("discovery nodes were stored on the tree the view renders")
	}
}

// ── Cancellation ─────────────────────────────────────────────────────────────

// Cancelling closes the channel: whatever was in flight is awaited, and the run
// ends rather than leaving the view waiting on an event that never comes.
func TestCancellingEndsTheRun(t *testing.T) {
	m := serverModel(t, newFakeGitLab(t, nil))

	spec := baseSpec("https://gl", t.TempDir())
	spec.client = m.shared.GitLabClient
	spec.selection = selecting("infra")
	spec.roots = []*TreeNode{{ID: 1, FullPath: "infra", Type: NodeTypeGroup}}

	run := startCloneRun(spec)
	run.cancel()

	collect(t, run) // fails on its own deadline if the channel never closes
}

// ── URLs ─────────────────────────────────────────────────────────────────────

// The clone method decides the URL shape. SSH takes the host alone, so the
// scheme has to come off, and a trailing slash on the configured URL must not
// produce a double slash in the HTTPS form.
func TestCloneURL(t *testing.T) {
	tests := []struct {
		name      string
		gitlabURL string
		method    string
		want      string
	}{
		{"https", "https://gl.example.com", "https", "https://gl.example.com/infra/api.git"},
		{"https with a trailing slash", "https://gl.example.com/", "https", "https://gl.example.com/infra/api.git"},
		{"an unset method defaults to https", "https://gl.example.com", "", "https://gl.example.com/infra/api.git"},
		{"ssh", "https://gl.example.com", "ssh", "git@gl.example.com:infra/api.git"},
		{"ssh over plain http", "http://gl.example.com/", "ssh", "git@gl.example.com:infra/api.git"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cloneURL(tt.gitlabURL, tt.method, "infra/api"); got != tt.want {
				t.Errorf("cloneURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
