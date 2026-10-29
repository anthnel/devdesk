package agentusage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	transcriptExt = ".jsonl"
	metaExt       = ".meta.json"
	agentPrefix   = "agent-"
	subagentsDir  = "subagents"
)

// SubAgentFiles locates one sub-agent's files; Meta is "" if absent.
type SubAgentFiles struct{ ID, Transcript, Meta string }

// SessionFiles locates a session's transcripts.
type SessionFiles struct {
	ID, Parent string
	SubAgents  []SubAgentFiles
}

// ProjectsRoot returns the Claude Code projects directory:
// $CLAUDE_CONFIG_DIR/projects, else ~/.claude/projects.
func ProjectsRoot() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "projects"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

// LocateSession finds the sub-agent files of the session whose parent
// transcript is <dir>/<uuid>.jsonl, looking in <dir>/<uuid>/subagents/. A
// missing session directory means no sub-agents. Symbolic links are ignored
// and nothing is searched recursively.
func LocateSession(parentTranscript string) (SessionFiles, error) {
	return locateSession(parentTranscript, metaOpens)
}

// metaPresence tells whether the meta file at path exists as a plain file.
type metaPresence func(path string) bool

// metaOpens checks a meta file by opening it through openPlain.
func metaOpens(path string) bool {
	mf, _ := openPlain(path, false)
	if mf == nil {
		return false
	}
	_ = mf.Close()
	return true
}

// locateSession is LocateSession with the meta presence check chosen by the
// caller: a poller passes an Lstat-only one so it never opens a meta file.
func locateSession(parentTranscript string, present metaPresence) (SessionFiles, error) {
	base := filepath.Base(parentTranscript)
	if !strings.HasSuffix(base, transcriptExt) {
		return SessionFiles{}, fmt.Errorf("agentusage: %s does not end in %s", base, transcriptExt)
	}
	info, err := os.Lstat(parentTranscript)
	if err != nil {
		return SessionFiles{}, baseOnly(err)
	}
	if !info.Mode().IsRegular() {
		return SessionFiles{}, fmt.Errorf("agentusage: %s is not a regular file", base)
	}
	id := strings.TrimSuffix(base, transcriptExt)
	if id == "" {
		return SessionFiles{}, fmt.Errorf("agentusage: %s has no session id", base)
	}
	files := SessionFiles{ID: sanitizeLabel(id), Parent: parentTranscript}
	sessionDir := filepath.Join(filepath.Dir(parentTranscript), id)
	if fi, err := os.Lstat(sessionDir); err != nil || !fi.IsDir() {
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return files, nil // absent, or a link: no sub-agents
		}
		return files, baseOnly(err)
	}
	files.SubAgents, err = subAgentFiles(filepath.Join(sessionDir, subagentsDir), present)
	return files, baseOnly(err)
}

// subAgentFiles lists agent-<id>.jsonl files directly inside dir, sorted by
// ID, each with its meta file if one exists. A missing dir or a symbolic
// link yields nothing.
func subAgentFiles(dir string, present metaPresence) ([]SubAgentFiles, error) {
	f, err := openPlain(dir, true)
	if f == nil {
		if err == nil || errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	var out []SubAgentFiles
	for _, e := range entries {
		if sa, ok := subAgentEntry(dir, e, present); ok {
			out = append(out, sa)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// subAgentEntry turns a directory entry into a sub-agent, if it is a
// regular agent-<id>.jsonl file.
func subAgentEntry(dir string, e os.DirEntry, present metaPresence) (SubAgentFiles, bool) {
	name := e.Name()
	if !e.Type().IsRegular() || !strings.HasPrefix(name, agentPrefix) || !strings.HasSuffix(name, transcriptExt) {
		return SubAgentFiles{}, false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(name, agentPrefix), transcriptExt)
	if id == "" {
		return SubAgentFiles{}, false
	}
	sa := SubAgentFiles{ID: sanitizeLabel(id), Transcript: filepath.Join(dir, name)}
	metaPath := filepath.Join(dir, agentPrefix+id+metaExt)
	if present(metaPath) {
		sa.Meta = metaPath
	}
	return sa, true
}

// baseOnly rewrites a path error so it names the base name of the file, not
// the full path; errors.Is and errors.As on the cause keep working.
func baseOnly(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return &fs.PathError{Op: pe.Op, Path: filepath.Base(pe.Path), Err: pe.Err}
	}
	return err
}
