package nexusbench

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// secretMarker is planted wherever a secret could leak; no output may carry it.
const secretMarker = "S3CRET-MARKER"

// fakeEnv returns a getenv backed by a map, so DefaultSecretsPath never reads
// the real environment (and never the real HOME).
func fakeEnv(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func TestDefaultSecretsPath(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		ctx, ns   string
		want      string
		wantError bool
	}{
		{
			name: "XDG_STATE_HOME wins",
			env:  map[string]string{"XDG_STATE_HOME": "/state", "HOME": "/home/u", "USERPROFILE": "/home/u"},
			ctx:  "datomir", ns: "nexus-bench",
			want: filepath.Join("/state", "devdesk", "nexus-bench", "datomir", "nexus-bench", "secrets.json"),
		},
		{
			name: "falls back to HOME/.local/state",
			env:  map[string]string{"HOME": "/home/u", "USERPROFILE": "/home/u"},
			ctx:  "datomir", ns: "nexus-bench",
			want: filepath.Join("/home/u", ".local", "state", "devdesk", "nexus-bench", "datomir", "nexus-bench", "secrets.json"),
		},
		{
			name: "an empty XDG_STATE_HOME counts as unset",
			env:  map[string]string{"XDG_STATE_HOME": "", "HOME": "/home/u", "USERPROFILE": "/home/u"},
			ctx:  "c", ns: "n",
			want: filepath.Join("/home/u", ".local", "state", "devdesk", "nexus-bench", "c", "n", "secrets.json"),
		},
		{
			name: "context and namespace make two different paths",
			env:  map[string]string{"XDG_STATE_HOME": "/state"},
			ctx:  "other", ns: "bench-2",
			want: filepath.Join("/state", "devdesk", "nexus-bench", "other", "bench-2", "secrets.json"),
		},
		{
			name:      "neither XDG_STATE_HOME nor HOME is an error",
			env:       map[string]string{},
			ctx:       "datomir",
			ns:        "nexus-bench",
			wantError: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DefaultSecretsPath(tt.ctx, tt.ns, fakeEnv(tt.env))
			if tt.wantError {
				if err == nil {
					t.Fatalf("DefaultSecretsPath = %q, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("DefaultSecretsPath: %v", err)
			}
			if got != tt.want {
				t.Errorf("DefaultSecretsPath = %q, want %q", got, tt.want)
			}
		})
	}
}

// secretsPathIn returns a store path two levels below a fresh temp dir, that
// does not exist yet: Save has to create the folders itself.
func secretsPathIn(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "ctx", "ns", "secrets.json")
}

func newTestStore(t *testing.T, path string) SecretStore {
	t.Helper()
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore(%q): %v", path, err)
	}
	return store
}

func TestFileStoreLoadAbsent(t *testing.T) {
	store := newTestStore(t, secretsPathIn(t))
	got, found, err := store.Load()
	if err != nil {
		t.Fatalf("Load on an absent file: %v", err)
	}
	if found {
		t.Error("Load on an absent file: found = true, want false")
	}
	if got != (Secrets{}) {
		t.Errorf("Load on an absent file = %+v, want the zero value", got)
	}
}

func TestFileStoreSaveLoadRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   Secrets
	}{
		{"all three fields", Secrets{AdminPassword: "a-1", PendingAdminPassword: "p-2", PullPassword: "pull-3"}},
		{"admin only", Secrets{AdminPassword: "a-1"}},
		{"pending only (interrupted password change)", Secrets{PendingAdminPassword: "p-2"}},
		{"characters JSON must escape", Secrets{AdminPassword: "q\"uo\\te<&>\né", PullPassword: "tab\there"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t, secretsPathIn(t))
			if err := store.Save(tt.in); err != nil {
				t.Fatalf("Save: %v", err)
			}
			got, found, err := store.Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !found {
				t.Fatal("Load: found = false after Save")
			}
			if got != tt.in {
				t.Errorf("Load = %+v, want %+v", got, tt.in)
			}
		})
	}
}

func TestFileStoreSaveOverwrites(t *testing.T) {
	store := newTestStore(t, secretsPathIn(t))
	if err := store.Save(Secrets{AdminPassword: "first", PullPassword: "pull"}); err != nil {
		t.Fatal(err)
	}
	second := Secrets{AdminPassword: "second"}
	if err := store.Save(second); err != nil {
		t.Fatal(err)
	}
	got, _, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != second {
		t.Errorf("Load = %+v, want %+v (the second Save replaces the first)", got, second)
	}
}

func TestFileStoreSavedFileSurvivesANewStore(t *testing.T) {
	path := secretsPathIn(t)
	want := Secrets{AdminPassword: "kept"}
	if err := newTestStore(t, path).Save(want); err != nil {
		t.Fatal(err)
	}
	got, found, err := newTestStore(t, path).Load()
	if err != nil || !found || got != want {
		t.Errorf("Load from a second store = (%+v, %v, %v), want (%+v, true, nil)", got, found, err, want)
	}
}

func TestFileStoreModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	path := secretsPathIn(t)
	store := newTestStore(t, path)

	// A file left by someone else with a wider mode must not keep it.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.Save(Secrets{AdminPassword: "x"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("secrets file mode = %o, want 600", got)
	}

	// A folder that Save creates is 0700.
	fresh := secretsPathIn(t)
	if err := newTestStore(t, fresh).Save(Secrets{AdminPassword: "x"}); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Dir(fresh), filepath.Dir(filepath.Dir(fresh))} {
		di, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := di.Mode().Perm(); got != 0o700 {
			t.Errorf("folder %s mode = %o, want 700", dir, got)
		}
	}
	fi, err := os.Stat(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("new secrets file mode = %o, want 600", got)
	}
}

func TestFileStoreSaveLeavesNoTemporaryFile(t *testing.T) {
	path := secretsPathIn(t)
	store := newTestStore(t, path)
	for i := 0; i < 3; i++ {
		if err := store.Save(Secrets{AdminPassword: strings.Repeat("a", i+1)}); err != nil {
			t.Fatal(err)
		}
	}
	assertOnlyEntries(t, filepath.Dir(path), "secrets.json")
}

func TestFileStoreSaveFailureLeavesNoPartialFile(t *testing.T) {
	// The target is a non-empty directory: the final rename cannot succeed.
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")
	if err := os.MkdirAll(filepath.Join(path, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := newTestStore(t, path)
	err := store.Save(Secrets{AdminPassword: secretMarker})
	if err == nil {
		t.Fatal("Save over a non-empty directory: want an error")
	}
	if strings.Contains(err.Error(), secretMarker) {
		t.Errorf("the error carries the secret: %v", err)
	}
	assertOnlyEntries(t, dir, "secrets.json")
}

func TestFileStoreSaveFailsWhenTheFolderCannotBeCreated(t *testing.T) {
	// A regular file where a folder is needed.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newTestStore(t, filepath.Join(blocker, "ns", "secrets.json"))
	if err := store.Save(Secrets{AdminPassword: "x"}); err == nil {
		t.Fatal("Save under a regular file: want an error")
	}
}

func TestFileStoreLoadCorruptDoesNotShowContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"not JSON", "{" + secretMarker + " this is not json"},
		{"wrong shape", `["` + secretMarker + `"]`},
		{"truncated", `{"adminPassword": "` + secretMarker},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := secretsPathIn(t)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, found, err := newTestStore(t, path).Load()
			if err == nil {
				t.Fatalf("Load = (%+v, %v, nil), want an error", got, found)
			}
			if strings.Contains(err.Error(), secretMarker) {
				t.Errorf("the error shows the file content: %v", err)
			}
			if got != (Secrets{}) {
				t.Errorf("Load returned %+v with an error, want the zero value", got)
			}
		})
	}
}

func TestFileStoreRemove(t *testing.T) {
	t.Run("removes the file and the empty namespace folder", func(t *testing.T) {
		path := secretsPathIn(t)
		store := newTestStore(t, path)
		if err := store.Save(Secrets{AdminPassword: "x"}); err != nil {
			t.Fatal(err)
		}
		if err := store.Remove(); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("the secrets file is still there (stat error = %v)", err)
		}
		if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
			t.Errorf("the empty namespace folder is still there (stat error = %v)", err)
		}
		if _, found, err := store.Load(); err != nil || found {
			t.Errorf("Load after Remove = (found %v, err %v), want (false, nil)", found, err)
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		store := newTestStore(t, secretsPathIn(t))
		for i := 0; i < 3; i++ {
			if err := store.Remove(); err != nil {
				t.Fatalf("Remove #%d on an absent file: %v", i+1, err)
			}
		}
		if err := store.Save(Secrets{AdminPassword: "x"}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err := store.Remove(); err != nil {
				t.Fatalf("Remove #%d after a Save: %v", i+1, err)
			}
		}
	})

	t.Run("keeps a namespace folder that holds something else", func(t *testing.T) {
		path := secretsPathIn(t)
		store := newTestStore(t, path)
		if err := store.Save(Secrets{AdminPassword: "x"}); err != nil {
			t.Fatal(err)
		}
		other := filepath.Join(filepath.Dir(path), "notes.txt")
		if err := os.WriteFile(other, []byte("keep me"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := store.Remove(); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		if _, err := os.Stat(other); err != nil {
			t.Errorf("Remove deleted a file it does not own: %v", err)
		}
	})
}

func TestNewFileStoreRejectsAnEmptyPath(t *testing.T) {
	if store, err := NewFileStore(""); err == nil {
		t.Errorf("NewFileStore(\"\") = %v, want an error", store)
	}
}

// gitRepo makes a repository in a temp dir, isolated from the user's git
// configuration (a global excludes file could ignore anything).
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return repo
}

func TestNewFileStoreGitGuard(t *testing.T) {
	tests := []struct {
		name      string
		gitignore string // content of the repository's .gitignore; "" writes none
		rel       string // secrets path, relative to the repository
		wantError bool
	}{
		{"in a repository, not ignored: refused", "", filepath.Join("sub", "secrets.json"), true},
		{"in a repository, at its root, not ignored: refused", "", "secrets.json", true},
		{"in a repository, ignored by .gitignore: accepted", "secrets.json\n", filepath.Join("sub", "secrets.json"), false},
		{"in a repository, another pattern ignored: still refused", "other.json\n", filepath.Join("sub", "secrets.json"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := gitRepo(t)
			if tt.gitignore != "" {
				if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(tt.gitignore), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			store, err := NewFileStore(filepath.Join(repo, tt.rel))
			if tt.wantError {
				if err == nil {
					t.Fatalf("NewFileStore inside a repository that does not ignore the file = %v, want an error", store)
				}
				if _, statErr := os.Stat(filepath.Join(repo, tt.rel)); !os.IsNotExist(statErr) {
					t.Errorf("a refused path was created anyway (stat error = %v)", statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewFileStore on an ignored path: %v", err)
			}
			if store == nil {
				t.Fatal("NewFileStore returned a nil store without an error")
			}
		})
	}

	t.Run("outside any repository: accepted", func(t *testing.T) {
		gitRepo(t) // only for the isolation of the git configuration
		outside := t.TempDir()
		store, err := NewFileStore(filepath.Join(outside, "ctx", "ns", "secrets.json"))
		if err != nil {
			t.Fatalf("NewFileStore outside a repository: %v", err)
		}
		if store == nil {
			t.Fatal("NewFileStore returned a nil store without an error")
		}
	})
}

// assertOnlyEntries fails unless dir holds exactly the named entries: no
// temporary file left behind.
func assertOnlyEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entries of %s = %v, want %v (no temporary file may remain)", dir, got, want)
	}
}

// Secrets never print their values, through any fmt verb or through slog.
func TestSecretsAreRedactedWhenFormatted(t *testing.T) {
	const (
		admin   = "adm1n-marker-8f3a"
		pending = "pend1ng-marker-1c7d"
		pull    = "pu11-marker-52be"
	)
	s := Secrets{AdminPassword: admin, PendingAdminPassword: pending, PullPassword: pull}
	markers := []string{admin, pending, pull}

	check := func(t *testing.T, what, got string) {
		t.Helper()
		for _, m := range markers {
			if strings.Contains(got, m) {
				t.Errorf("%s shows a secret value: %q", what, got)
			}
		}
		if !strings.Contains(got, "redacted") {
			t.Errorf("%s = %q, want it to say the secrets are redacted", what, got)
		}
	}

	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		check(t, "Sprintf("+verb+", value)", fmt.Sprintf(verb, s))
		check(t, "Sprintf("+verb+", pointer)", fmt.Sprintf(verb, &s))
	}
	check(t, "Sprint", fmt.Sprint(s))
	check(t, "Sprintln", fmt.Sprintln(s))
	check(t, "Sprintf of a struct holding Secrets", fmt.Sprintf("%+v", struct{ S Secrets }{s}))

	var text, jsonBuf bytes.Buffer
	slog.New(slog.NewTextHandler(&text, nil)).Info("loaded", "secrets", s)
	check(t, "slog text handler", text.String())
	slog.New(slog.NewJSONHandler(&jsonBuf, nil)).Info("loaded", "secrets", s)
	check(t, "slog JSON handler", jsonBuf.String())

	// The file keeps the values: only the display is masked.
	store := newTestStore(t, secretsPathIn(t))
	if err := store.Save(s); err != nil {
		t.Fatal(err)
	}
	got, _, err := store.Load()
	if err != nil || got != s {
		t.Errorf("Load after Save = %+v, %v; masking must not touch the stored values", got, err)
	}
}

// git check-ignore must judge the repository of the path, not one a GIT_*
// variable of the caller points at.
func TestNewFileStoreGitGuardIgnoresInheritedGitEnvironment(t *testing.T) {
	repo := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("secrets.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir() // a repository that does not ignore the file
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = other
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(other, ".git", "index"))

	if _, err := NewFileStore(filepath.Join(repo, "secrets.json")); err != nil {
		t.Fatalf("a path ignored by its own repository was refused because of GIT_DIR: %v", err)
	}
}

func TestGitEnvironmentDropsTheRepositoryVariables(t *testing.T) {
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_NAMESPACE"} {
		t.Setenv(name, "/elsewhere")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	env := gitEnvironment()
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(name) {
		case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_NAMESPACE":
			t.Errorf("gitEnvironment keeps %s", kv)
		}
	}
	if !slices.Contains(env, "GIT_CONFIG_NOSYSTEM=1") {
		t.Error("gitEnvironment dropped a variable that is not about the repository")
	}
}
