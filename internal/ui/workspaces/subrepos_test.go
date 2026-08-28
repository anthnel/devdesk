package workspaces

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// mkRepoAt creates an ordinary working-tree repository.
func mkRepoAt(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("creating the repo: %v", err)
	}
	return dir
}

// linkDir makes a directory link the way the platform makes them.
//
// os.Symlink to a directory needs SeCreateSymbolicLinkPrivilege on Windows,
// which a plain user session does not have unless Developer Mode is on — so
// these tests would skip on the very platform the defect was reported from. A
// *junction* needs no privilege, and a junction is what was in the report:
// `mklink /J` is therefore the Windows path rather than a fallback.
func linkDir(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			t.Skipf("cannot create a junction here: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create a directory link here: %v", err)
	}
}

func repoSet(paths []string) map[string]bool {
	out := map[string]bool{}
	for _, p := range paths {
		out[p] = true
	}
	return out
}

// D59 cause 2: DirEntry.IsDir() reports on the link itself, so a junction to a
// directory full of repositories said false and the whole subtree was invisible
// to S, F and A.
func TestARepositoryBehindALinkIsFound(t *testing.T) {
	root := t.TempDir()
	elsewhere := t.TempDir()
	target := mkRepoAt(t, elsewhere, "linked-repo")

	linkDir(t, elsewhere, filepath.Join(root, "linked"))

	found := detectSubRepos(root, false)
	if !repoSet(found.Repos)[filepath.Join(root, "linked", "linked-repo")] {
		t.Errorf("detectSubRepos = %v, want the repository behind the link (target %q)", found.Repos, target)
	}
}

// The cycle guard the depth limit used to provide by accident. Without it this
// test does not fail — it never returns.
func TestALinkPointingAtAnAncestorDoesNotLoop(t *testing.T) {
	root := t.TempDir()
	repo := mkRepoAt(t, root, "a", "repo")
	linkDir(t, root, filepath.Join(root, "a", "loop"))

	found := detectSubRepos(root, false)
	if !repoSet(found.Repos)[repo] {
		t.Errorf("detectSubRepos = %v, want %q", found.Repos, repo)
	}
	for _, p := range found.Repos {
		if strings.Contains(p, "loop") {
			t.Errorf("the walk went through the loop and reported %q", p)
		}
	}
}

// Two links onto the same tree are the same repositories, and reporting them
// twice would scan each of them twice under two names.
func TestADirectoryReachedTwiceIsWalkedOnce(t *testing.T) {
	root := t.TempDir()
	elsewhere := t.TempDir()
	mkRepoAt(t, elsewhere, "shared")

	linkDir(t, elsewhere, filepath.Join(root, "one"))
	linkDir(t, elsewhere, filepath.Join(root, "two"))

	found := detectSubRepos(root, false)
	if len(found.Repos) != 1 {
		t.Errorf("detectSubRepos = %v, want the shared repository reported once", found.Repos)
	}
}

// A bare repository holds history, so it is a scan target like any other. The
// .git test caught a working tree and a worktree and missed this one.
func TestABareRepositoryIsFound(t *testing.T) {
	root := t.TempDir()
	bare := filepath.Join(root, "project.git")
	for _, name := range []string{"objects", "refs"} {
		if err := os.MkdirAll(filepath.Join(bare, name), 0o755); err != nil {
			t.Fatalf("creating the bare repo: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(bare, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatalf("writing HEAD: %v", err)
	}

	found := detectSubRepos(root, false)
	if !repoSet(found.Repos)[bare] {
		t.Errorf("detectSubRepos = %v, want the bare repository %q", found.Repos, bare)
	}
}

// A directory holding only some of those three is not a repository, and saying
// it is would send a scan somewhere there is nothing to read.
func TestAPartialLayoutIsNotABareRepository(t *testing.T) {
	root := t.TempDir()
	decoy := filepath.Join(root, "objects-only")
	if err := os.MkdirAll(filepath.Join(decoy, "objects"), 0o755); err != nil {
		t.Fatalf("creating the decoy: %v", err)
	}

	if isRepo(decoy) {
		t.Errorf("%q was taken for a repository on objects/ alone", decoy)
	}
}

// The `.git` file a worktree leaves behind, which the stat test always caught
// and which the bare check must not break.
func TestAWorktreeIsStillARepository(t *testing.T) {
	root := t.TempDir()
	wt := filepath.Join(root, "worktree")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("creating the worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatalf("writing the .git file: %v", err)
	}

	if !isRepo(wt) {
		t.Errorf("%q is a worktree and was not recognised", wt)
	}
}

// The swallowed os.ReadDir. A directory refused in the middle of a tree
// contributed nothing and said nothing, which is the silence D59 is.
func TestAnUnreadableDirectoryIsCounted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not restrict directory reads on Windows")
	}
	root := t.TempDir()
	visible := mkRepoAt(t, root, "visible")
	closed := filepath.Join(root, "closed")
	if err := os.MkdirAll(closed, 0o755); err != nil {
		t.Fatalf("creating the directory: %v", err)
	}
	mkRepoAt(t, closed, "hidden-repo")
	if err := os.Chmod(closed, 0o000); err != nil {
		t.Fatalf("closing the directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(closed, 0o755) })

	found := detectSubRepos(root, false)
	if !repoSet(found.Repos)[visible] {
		t.Errorf("detectSubRepos = %v, want at least the readable repository", found.Repos)
	}
	if found.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 — a gap nobody counts is a gap nobody sees", found.Skipped)
	}
}

// ── What the omission looks like on screen ───────────────────────────────────

// A junction rendered as a file: neither browsable nor scannable, while the
// tree behind it was perfectly ordinary. The listing and the walk answer this
// question the same way, for the reason isHidden is shared.
func TestTheListingShowsALinkedDirectoryAsADirectory(t *testing.T) {
	root := t.TempDir()
	elsewhere := t.TempDir()
	mkRepoAt(t, elsewhere, "repo")
	linkDir(t, elsewhere, filepath.Join(root, "linked"))

	m := newTestModel(t)
	m.config.App.WorkspacesDir = root
	msg := m.loadEntries()()
	loaded, ok := msg.(EntriesLoadedMsg)
	if !ok {
		t.Fatalf("loadEntries returned %T, want EntriesLoadedMsg", msg)
	}

	for _, e := range loaded.Entries {
		if e.Name != "linked" {
			continue
		}
		if !e.IsDir {
			t.Error("the linked directory is listed as a file, so → and S do not apply to it")
		}
		if len(e.SubRepoPaths) == 0 {
			t.Error("the linked directory reports no nested repository")
		}
		return
	}
	t.Fatalf("no row named %q in %v", "linked", loaded.Entries)
}

// The number that decides whether a gap is visible. The footer could say how
// many repositories were about to be scanned and nothing at all about how many
// it had failed to look for.
func TestTheFooterCountsWhatTheWalkCouldNotRead(t *testing.T) {
	m := newTestModel(t)

	if cmd := m.warnSkipped(0); cmd != nil {
		t.Error("a warning was posted with nothing to warn about")
	}
	if m.footer.IsSet() {
		t.Error("the footer carries a message when nothing was skipped")
	}

	cmd := m.warnSkipped(3)
	if cmd == nil {
		t.Fatal("nothing was posted for three unreadable directories")
	}
	if !strings.Contains(m.footer.Text(), "3 directories") {
		t.Errorf("footer says %q, want the count in it", m.footer.Text())
	}
	if m.footer.Level() != sharedcomponents.LevelWarning {
		t.Errorf("footer level = %v, want Warn: nothing failed, the request cannot be honoured in full", m.footer.Level())
	}
}

// One is not "1 directories".
func TestASingleUnreadableDirectoryReadsAsOne(t *testing.T) {
	m := newTestModel(t)
	m.warnSkipped(1)

	if got := m.footer.Text(); !strings.Contains(got, "1 directory ") {
		t.Errorf("footer says %q, want the singular", got)
	}
}

// A and ctrl+a act on the view rather than on a row, so the number they answer
// for is the whole listing's.
func TestSkippedIsTotalledAcrossTheListing(t *testing.T) {
	entries := entryFixtures()
	entries[2].SubRepoSkipped = 2
	entries[3].SubRepoSkipped = 1
	m := feed(t, newTestModel(t), EntriesLoadedMsg{Entries: entries})

	if got := m.skippedInView(); got != 3 {
		t.Errorf("skippedInView = %d, want 3", got)
	}
}

// "No repository nested under it" is a claim the walk is not entitled to make
// when it could not read part of the tree. Not knowing is not knowing there are
// none (Rule 130).
func TestADirectoryItCouldNotReadRefusesWithTheRightReason(t *testing.T) {
	entries := entryFixtures()
	entries[3].SubRepoSkipped = 2 // empty-dir: nothing found, two places unread
	m := feed(t, newTestModel(t), EntriesLoadedMsg{Entries: entries})
	m.table.SetCursor(3)

	m, _ = step(t, m, testutil.Key(keymap.Fetch))

	if !strings.Contains(m.footer.Text(), "2 directories could not be read") {
		t.Errorf("footer says %q, want the count of what could not be read", m.footer.Text())
	}

	// And the same directory with nothing skipped keeps the plain reason: the
	// wording has to distinguish the two, or it says nothing new.
	plain := feed(t, newTestModel(t), EntriesLoadedMsg{Entries: entryFixtures()})
	plain.table.SetCursor(3)
	plain, _ = step(t, plain, testutil.Key(keymap.Fetch))
	if got := plain.footer.Text(); got != reasonNoScanTarget {
		t.Errorf("footer says %q, want %q", got, reasonNoScanTarget)
	}
}

// The count rides on the run rather than going to the footer as a Warn: the
// run's line is the one that survives the three-second timer, and it is where
// the rest of the batch's outcome is already reported.
func TestTheSyncSummaryReportsWhatItCouldNotRead(t *testing.T) {
	m := newTestModel(t)
	m.sync = &syncRun{total: 1, done: 1, upToDate: 1, unreadable: 2}

	if got := m.syncStatusLine(); !strings.Contains(got, "2 directories unreadable") {
		t.Errorf("syncStatusLine = %q, want the unreadable count", got)
	}
}
