package workspaces

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

const devdeskPath = "/tmp/workspaces/devdesk"

// ── The target ───────────────────────────────────────────────────────────────

// s follows ctrl+s's rule rather than inventing a selection mode: the repo
// under the cursor, or every repo nested under a plain directory.
func TestSyncTargetsTheRepositoryUnderTheCursor(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(0) // devdesk, a git repo

	m, cmd := step(t, m, testutil.Key("s"))

	if cmd == nil {
		t.Fatal("pressing s on a git repository started nothing")
	}
	if m.sync == nil || m.sync.total != 1 {
		t.Fatalf("sync run = %+v, want one repository queued", m.sync)
	}
}

func TestSyncOnAPlainDirectoryTakesEveryRepositoryUnderIt(t *testing.T) {
	m := loadedModel(t)
	m.table.SetCursor(2) // clients, two nested repos

	m, cmd := step(t, m, testutil.Key("s"))

	if cmd == nil {
		t.Fatal("pressing s on a directory of repositories started nothing")
	}
	if m.sync == nil || m.sync.total != 2 {
		t.Fatalf("sync run = %+v, want both nested repositories queued", m.sync)
	}
}

func TestSyncDoesNothingWhereThereIsNoRepository(t *testing.T) {
	for _, tt := range []struct {
		name   string
		cursor int
	}{
		{"a directory holding no repositories", 3},
		{"a file", 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := loadedModel(t)
			m.table.SetCursor(tt.cursor)

			m, cmd := step(t, m, testutil.Key("s"))

			if cmd != nil || m.sync != nil {
				t.Errorf("pressing s started a sync on %s", tt.name)
			}
		})
	}
}

// ── Scan and sync are mutually exclusive ─────────────────────────────────────

// A scan reads the working tree while a fast-forward rewrites it. Each has to
// refuse while the other holds the repository, and both directions matter: only
// one of them was ever guarded before.
func TestAScanAndASyncNeverShareARepository(t *testing.T) {
	t.Run("sync refuses a repository being scanned", func(t *testing.T) {
		m := loadedModel(t)
		m.table.SetCursor(0)
		m.scanningPaths[devdeskPath] = true

		m, cmd := step(t, m, testutil.Key("s"))

		if m.sync != nil {
			t.Error("a sync started on a repository already being scanned")
		}
		if m.footerInfo != busyMessage || cmd == nil {
			t.Errorf("footerInfo = %q with cmd == nil: %v", m.footerInfo, cmd == nil)
		}
	})

	t.Run("scan refuses a repository being synced", func(t *testing.T) {
		m := loadedModel(t)
		m.table.SetCursor(0)
		m.syncingPaths[devdeskPath] = true

		m, cmd := step(t, m, testutil.Key("ctrl+s"))

		if m.footerInfo != busyMessage || cmd == nil {
			t.Errorf("footerInfo = %q with cmd == nil: %v", m.footerInfo, cmd == nil)
		}
	})
}

// ctrl+a purges before it rescans. A repository being synced must be left out
// of the purge too, or its counts are blanked with nothing on the way to
// replace them.
func TestScanAllLeavesASyncingRepositorysCacheAlone(t *testing.T) {
	m := scannedModel(t)
	m.syncingPaths[devdeskPath] = true

	m, _ = step(t, m, testutil.Key("ctrl+a"))

	if _, ok := m.scanCache[devdeskPath]; !ok {
		t.Error("the syncing repository's cached counts were purged")
	}
}

// ── What the row shows ───────────────────────────────────────────────────────

func TestASyncingRowSpinsInTheGitStatusColumn(t *testing.T) {
	m := feed(t, loadedModel(t), WorkspaceSyncStartingMsg{RepoPath: devdeskPath})

	row := m.table.Items()[0]
	if !strings.Contains(row.GitStatus, "syncing") {
		t.Errorf("Git Status = %q, want the sync marker", row.GitStatus)
	}
	// Rule 122: a table cell carries no escape sequences.
	if strings.Contains(row.GitStatus, "\x1b") {
		t.Errorf("Git Status carries ANSI: %q", row.GitStatus)
	}
}

// The counts are the whole point (D35): they came from a tracking ref nothing
// had moved, and a finished sync is the moment they become true.
func TestAFinishedSyncReplacesTheRowsGitCounts(t *testing.T) {
	m := feed(t, loadedModel(t), WorkspaceSyncStartingMsg{RepoPath: devdeskPath})

	m = feed(t, m, WorkspaceSyncCompleteMsg{
		RepoPath: devdeskPath,
		Outcome:  git.SyncUpdated,
		Behind:   4,
		Status: Entry{
			IsGitRepo: true, GitBranch: "main",
			GitRemote: "anthnel/devdesk", GitRemoteURL: "https://github.com/anthnel/devdesk",
			GitModified: 0, GitUntracked: 0, GitUnpushed: 3, GitUnpulled: 0,
		},
	})

	entry := m.table.Items()[0].Entry
	if entry.GitModified != 0 || entry.GitUnpulled != 0 {
		t.Errorf("the row still shows the state before the sync: %+v", entry)
	}
	if entry.GitUnpushed != 3 {
		t.Errorf("GitUnpushed = %d, want the re-read value", entry.GitUnpushed)
	}
	if entry.Name != "devdesk" || entry.ProjectType != "Go" {
		t.Errorf("the listing's own fields were overwritten: %+v", entry)
	}
	if m.syncingPaths[devdeskPath] {
		t.Error("the row is still marked as syncing")
	}
}

// A repository that was navigated away from mid-sync has no row to refresh. The
// run's counters still apply — they are about the batch, not the screen.
func TestACompletionForARowThatIsGoneStillCounts(t *testing.T) {
	m := loadedModel(t)
	m.sync = &syncRun{total: 1}

	m = feed(t, m, WorkspaceSyncCompleteMsg{RepoPath: "/tmp/workspaces/vanished", Outcome: git.SyncUpdated})

	if m.sync == nil || m.sync.updated != 1 {
		t.Errorf("sync run = %+v, want the completion counted", m.sync)
	}
}

// ── The footer ───────────────────────────────────────────────────────────────

func TestTheFooterCountsASyncWhileItRunsAndSumsItUpAfter(t *testing.T) {
	m := loadedModel(t)
	m.sync = &syncRun{total: 3}

	m = feed(t, m, WorkspaceSyncCompleteMsg{RepoPath: devdeskPath, Outcome: git.SyncUpdated})
	if got := m.RenderFooter(160); !strings.Contains(got, "1/3") {
		t.Errorf("the footer does not show the progress:\n%s", got)
	}

	m = feed(t, m,
		WorkspaceSyncCompleteMsg{RepoPath: "/tmp/workspaces/clean-repo", Outcome: git.SyncUpToDate},
		WorkspaceSyncCompleteMsg{
			RepoPath: "/tmp/workspaces/clients/a",
			Outcome:  git.SyncSkipped, Reason: "uncommitted changes",
		},
	)

	footer := m.RenderFooter(160)
	for _, want := range []string{"1 repository updated", "1 up to date", "1 skipped", "a: uncommitted changes"} {
		if !strings.Contains(footer, want) {
			t.Errorf("the summary is missing %q:\n%s", want, footer)
		}
	}
}

// A failure is a failure, not a skip: the difference is whether the repository
// is in the state its owner left it in, or whether DevDesk could not find out.
func TestAFailedSyncIsNamedAndPointsAtTheLog(t *testing.T) {
	m := loadedModel(t)
	m.sync = &syncRun{total: 1}

	m = feed(t, m, WorkspaceSyncCompleteMsg{RepoPath: devdeskPath, Error: errors.New("fatal: could not read from remote")})

	footer := m.RenderFooter(160)
	if !strings.Contains(footer, "1 failed") || !strings.Contains(footer, "devdesk") {
		t.Errorf("the failure is not named in the footer:\n%s", footer)
	}
	if m.sync.skipped != 0 {
		t.Error("a failure was counted as a skip")
	}
}

// The summary's timer must not wipe a sync started inside its three seconds.
func TestTheSummaryTimerOnlyDropsASettledRun(t *testing.T) {
	m := loadedModel(t)
	m.sync = &syncRun{total: 1, done: 1, updated: 1}

	m = feed(t, m, clearSyncSummaryMsg{})
	if m.sync != nil {
		t.Error("a finished run's summary outlived its timer")
	}

	m.sync = &syncRun{total: 4, done: 1}
	m = feed(t, m, clearSyncSummaryMsg{})
	if m.sync == nil {
		t.Error("a running sync was cleared by the previous run's timer")
	}
}

// The spinner has to keep ticking for a sync, not only for a scan.
func TestTheSpinnerKeepsTickingForASyncWithNoScan(t *testing.T) {
	m := feed(t, loadedModel(t), WorkspaceSyncStartingMsg{RepoPath: devdeskPath})

	_, cmd := step(t, m, m.spinner.Tick())

	if cmd == nil {
		t.Error("the spinner stopped, so a syncing row would freeze mid-frame")
	}
}

// The chain the view starts at Init dies on its first tick, because the handler
// stops scheduling once nothing is running. Whatever starts next has to bring
// it back — and exactly once, or the frames advance at twice the rate.
//
// This was already broken for scans: the frozen frame read as a marker rather
// than a stalled animation, which is why nobody noticed.
func TestTheFirstScanOrSyncRestartsTheSpinnerChain(t *testing.T) {
	for _, tt := range []struct {
		name          string
		first, second tea.Msg
	}{
		{
			"scan",
			WorkspaceScanStartingMsg{RepoPath: devdeskPath},
			WorkspaceScanStartingMsg{RepoPath: "/tmp/workspaces/clean-repo"},
		},
		{
			"sync",
			WorkspaceSyncStartingMsg{RepoPath: devdeskPath},
			WorkspaceSyncStartingMsg{RepoPath: "/tmp/workspaces/clean-repo"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := loadedModel(t)

			// Init's chain, arriving with nothing running: it stops here.
			m, cmd := step(t, m, spinner.TickMsg{ID: m.spinner.ID()})
			if cmd != nil {
				t.Fatal("the spinner kept ticking with nothing running")
			}

			m, cmd = step(t, m, tt.first)
			if cmd == nil {
				t.Error("the chain was not restarted, so the row's frame never advances")
			}

			if _, cmd = step(t, m, tt.second); cmd != nil {
				t.Error("a second chain was started beside the live one")
			}
		})
	}
}

// readGitStatus is what a completion carries back, and the reason a refused
// sync is still worth something: it runs against a real repository, after the
// fetch, whatever the sync decided to do.
func TestReadGitStatusReturnsTheGitFieldsAndNothingElse(t *testing.T) {
	repo := gitRepoFixture(t)

	status := readGitStatus(repo)

	if !status.IsGitRepo {
		t.Fatal("the repository was not detected as one")
	}
	if status.GitBranch != "main" {
		t.Errorf("GitBranch = %q, want main", status.GitBranch)
	}
	if status.GitRemote != "group/project" {
		t.Errorf("GitRemote = %q, want the path without the host", status.GitRemote)
	}
	// The listing owns these; a sync had no reason to look at them, and
	// applyGitStatus must not be handed values to overwrite a row with.
	if status.Name != "" || status.ProjectType != "" || !status.ModTime.IsZero() {
		t.Errorf("readGitStatus filled fields that belong to the listing: %+v", status)
	}
}

// gitRepoFixture builds a repository with a remote and one commit.
func gitRepoFixture(t *testing.T) string {
	t.Helper()
	bin := gitOrSkip(t)
	repo := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+filepath.Join(t.TempDir(), "gitconfig"),
			"GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "--initial-branch=main")
	run("remote", "add", "origin", "git@gitlab.com:group/project.git")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# x\n"), 0o600); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial")
	return repo
}

// ── The credential ───────────────────────────────────────────────────────────

// The workspaces view holds whatever the user has cloned. A personal access
// token authenticates one host, and offering DevDesk's GitLab credential to
// anything else hands it to a third party over the wire.
func TestTheTokenGoesToTheConfiguredGitLabHostAndNowhereElse(t *testing.T) {
	load := func() string { return "glpat-secret" }

	tests := []struct {
		name      string
		remote    string
		gitlabURL string
		want      string
	}{
		{"the configured host over https", "https://gitlab.example.com/g/p.git", "https://gitlab.example.com", "glpat-secret"},
		{"the configured host over ssh", "git@gitlab.example.com:g/p.git", "https://gitlab.example.com", "glpat-secret"},
		{"the configured host with a port", "https://gitlab.example.com:8443/g/p.git", "https://gitlab.example.com", "glpat-secret"},
		{"a different case", "https://GitLab.Example.COM/g/p.git", "https://gitlab.example.com", "glpat-secret"},
		{"another forge", "https://github.com/anthnel/devdesk.git", "https://gitlab.example.com", ""},
		{"a host that merely ends the same way", "https://evil-gitlab.example.com.attacker.net/g/p.git", "https://gitlab.example.com", ""},
		{"a local path with no host at all", "/srv/git/bare.git", "https://gitlab.example.com", ""},
		{"no GitLab configured", "https://gitlab.example.com/g/p.git", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tokenForRemote(tt.remote, tt.gitlabURL, load); got != tt.want {
				t.Errorf("tokenForRemote(%q, %q) = %q, want %q", tt.remote, tt.gitlabURL, got, tt.want)
			}
		})
	}
}

// countingStore records how often the secret store is read.
type countingStore struct {
	token string
	reads int
}

func (s *countingStore) Save(string, string) error { return nil }
func (s *countingStore) Delete(string) error       { return nil }
func (s *countingStore) Load(string) (string, error) {
	s.reads++
	return s.token, nil
}

// The keyring is not free, and a batch of a dozen repositories must not read it
// a dozen times — the same reason the clone pipeline loads its token once.
func TestTheSyncTokenIsReadFromTheStoreOnlyOnce(t *testing.T) {
	store := &countingStore{token: "glpat-secret"}
	load := tokenLoader(store, "https://gitlab.example.com")

	for range 5 {
		if got := load(); got != "glpat-secret" {
			t.Fatalf("load() = %q, want the stored token", got)
		}
	}

	if store.reads != 1 {
		t.Errorf("the store was read %d times, want once for the whole batch", store.reads)
	}
}

// A foreign remote must not even reach the loader: no read, no token.
func TestAForeignRemoteNeverTouchesTheSecretStore(t *testing.T) {
	store := &countingStore{token: "glpat-secret"}
	load := tokenLoader(store, "https://gitlab.example.com")

	if got := tokenForRemote("https://github.com/anthnel/devdesk.git", "https://gitlab.example.com", load); got != "" {
		t.Errorf("tokenForRemote() = %q for a foreign remote", got)
	}
	if store.reads != 0 {
		t.Errorf("the store was read %d times for a remote the token is not for", store.reads)
	}
}

func TestTokenLoaderWithoutAStoreAnswersEmpty(t *testing.T) {
	if got := tokenLoader(nil, "https://gitlab.example.com")(); got != "" {
		t.Errorf("tokenLoader(nil, …) = %q, want no token", got)
	}
}
