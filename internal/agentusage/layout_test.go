package agentusage

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

const sessUUID = "11111111-2222-3333-4444-555555555555"

func isolateEnv(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
}

func TestProjectsRoot(t *testing.T) {
	t.Run("CLAUDE_CONFIG_DIR wins", func(t *testing.T) {
		isolateEnv(t)
		cfg := t.TempDir()
		t.Setenv("CLAUDE_CONFIG_DIR", cfg)
		got, err := ProjectsRoot()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(cfg, "projects"); got != want {
			t.Fatalf("root = %q, want %q", got, want)
		}
	})
	t.Run("default is home/.claude/projects", func(t *testing.T) {
		isolateEnv(t)
		home, _ := os.UserHomeDir()
		got, err := ProjectsRoot()
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(home, ".claude", "projects"); got != want {
			t.Fatalf("root = %q, want %q", got, want)
		}
	})
}

// session builds <proj>/<uuid>.jsonl and returns the parent path.
func session(t *testing.T) (parent, dir string) {
	t.Helper()
	proj := t.TempDir()
	parent = filepath.Join(proj, sessUUID+".jsonl")
	writeFile(t, parent, "{}\n")
	return parent, filepath.Join(proj, sessUUID)
}

func TestLocateSession(t *testing.T) {
	isolateEnv(t)
	parent, dir := session(t)
	sub := filepath.Join(dir, "subagents")
	writeFile(t, filepath.Join(sub, "agent-b.jsonl"), "{}\n")
	writeFile(t, filepath.Join(sub, "agent-b.meta.json"), "{}")
	writeFile(t, filepath.Join(sub, "agent-a.jsonl"), "{}\n") // no meta
	writeFile(t, filepath.Join(sub, "agent-c.jsonl"), "{}\n")
	writeFile(t, filepath.Join(sub, "agent-c.meta.json"), "{}")
	// Must all be ignored.
	writeFile(t, filepath.Join(sub, "notes.jsonl"), "{}\n")
	writeFile(t, filepath.Join(sub, "agent-x.meta.json"), "{}") // meta alone, not a transcript
	writeFile(t, filepath.Join(sub, "agent-y.txt"), "x")
	writeFile(t, filepath.Join(sub, "nested", "agent-z.jsonl"), "{}\n") // no recursion
	writeFile(t, filepath.Join(sub, "tool-results", "agent-t.jsonl"), "{}\n")
	writeFile(t, filepath.Join(dir, "tool-results", "agent-u.jsonl"), "{}\n")

	got, err := LocateSession(parent)
	if err != nil {
		t.Fatal(err)
	}
	want := SessionFiles{
		ID:     sessUUID,
		Parent: parent,
		SubAgents: []SubAgentFiles{
			{ID: "a", Transcript: filepath.Join(sub, "agent-a.jsonl")},
			{ID: "b", Transcript: filepath.Join(sub, "agent-b.jsonl"), Meta: filepath.Join(sub, "agent-b.meta.json")},
			{ID: "c", Transcript: filepath.Join(sub, "agent-c.jsonl"), Meta: filepath.Join(sub, "agent-c.meta.json")},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestLocateSessionNoSessionDir(t *testing.T) {
	isolateEnv(t)
	parent, _ := session(t)
	got, err := LocateSession(parent)
	if err != nil {
		t.Fatalf("missing <uuid>/ dir must not be an error: %v", err)
	}
	if got.ID != sessUUID || got.Parent != parent || len(got.SubAgents) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestLocateSessionEmptySubagentsDir(t *testing.T) {
	isolateEnv(t)
	parent, dir := session(t)
	if err := os.MkdirAll(filepath.Join(dir, "subagents"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := LocateSession(parent)
	if err != nil || len(got.SubAgents) != 0 {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestLocateSessionMissingParent(t *testing.T) {
	isolateEnv(t)
	if _, err := LocateSession(filepath.Join(t.TempDir(), sessUUID+".jsonl")); err == nil {
		t.Fatal("want an error for a missing parent transcript")
	}
}

func TestLocateSessionIgnoresSymlinks(t *testing.T) {
	isolateEnv(t)
	parent, dir := session(t)
	sub := filepath.Join(dir, "subagents")
	target := filepath.Join(t.TempDir(), "real.jsonl")
	writeFile(t, target, "{}\n")
	writeFile(t, filepath.Join(sub, "agent-real.jsonl"), "{}\n")
	writeFile(t, filepath.Join(sub, "agent-real.meta.json"), "{}")
	if err := os.Symlink(target, filepath.Join(sub, "agent-link.jsonl")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// A symlinked meta must not be picked up either.
	metaTarget := filepath.Join(t.TempDir(), "m.json")
	writeFile(t, metaTarget, "{}")
	writeFile(t, filepath.Join(sub, "agent-lm.jsonl"), "{}\n")
	if err := os.Symlink(metaTarget, filepath.Join(sub, "agent-lm.meta.json")); err != nil {
		t.Fatal(err)
	}

	got, err := LocateSession(parent)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]SubAgentFiles{}
	for _, s := range got.SubAgents {
		ids[s.ID] = s
	}
	if _, ok := ids["link"]; ok {
		t.Fatalf("symlinked transcript must be ignored: %+v", got.SubAgents)
	}
	if len(got.SubAgents) != 2 || ids["real"].Meta == "" {
		t.Fatalf("got %+v", got.SubAgents)
	}
	if ids["lm"].Meta != "" {
		t.Fatalf("symlinked meta must be ignored: %+v", ids["lm"])
	}
}

func TestLocateSessionSymlinkedSubagentsDirIgnored(t *testing.T) {
	isolateEnv(t)
	parent, dir := session(t)
	other := t.TempDir()
	writeFile(t, filepath.Join(other, "agent-q.jsonl"), "{}\n")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(dir, "subagents")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, err := LocateSession(parent)
	if err != nil || len(got.SubAgents) != 0 {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

func TestLocateSessionParentWithoutJSONLSuffix(t *testing.T) {
	isolateEnv(t)
	for _, name := range []string{sessUUID, sessUUID + ".json", sessUUID + ".txt"} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), name)
			writeFile(t, p, "{}\n")
			_, err := LocateSession(p)
			if err == nil {
				t.Fatal("want an error for a parent without the .jsonl suffix")
			}
			// Today the error is an accidental ENOTDIR from probing
			// "<name>/subagents" under a regular file; the check must be
			// explicit, not a side effect.
			if errors.Is(err, syscall.ENOTDIR) {
				t.Fatalf("error is an accidental ENOTDIR, want an explicit suffix check: %v", err)
			}
		})
	}
}

func TestLocateSessionSymlinkedSessionDirIgnored(t *testing.T) {
	isolateEnv(t)
	parent, dir := session(t)
	real := t.TempDir()
	writeFile(t, filepath.Join(real, "subagents", "agent-q.jsonl"), "{}\n")
	if err := os.Symlink(real, dir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, err := LocateSession(parent)
	if err != nil || len(got.SubAgents) != 0 {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

// Errors name the file, never the directory around it.
func TestLocateSessionErrorsNameOnlyTheBaseName(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	missing := filepath.Join(dir, sessUUID+".jsonl")
	asDir := filepath.Join(dir, "other.jsonl")
	if err := os.Mkdir(asDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{missing, asDir, filepath.Join(dir, "notes.txt")} {
		_, err := LocateSession(p)
		if err == nil {
			t.Fatalf("%s: want an error", filepath.Base(p))
		}
		if strings.Contains(err.Error(), dir) {
			t.Errorf("%s: error %q contains the parent directory", filepath.Base(p), err)
		}
		if !strings.Contains(err.Error(), filepath.Base(p)) {
			t.Errorf("%s: error %q does not name the file", filepath.Base(p), err)
		}
	}
}
