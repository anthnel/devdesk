package agentusage

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxSessions is the most sessions ListSessions keeps. A var so tests can lower it.
var maxSessions = 10000

// cwdProbeBytes is how much of a transcript is read looking for a cwd. It is
// twice MaxLineBytes so one maximal line can be skipped. A var so tests can
// lower it.
var cwdProbeBytes int64 = 2 * MaxLineBytes

// maxPathLen is the longest Project kept, in bytes.
const maxPathLen = 4096

// Session is one session found under a projects root. Parent is a full path
// for LocateSession and is never logged. Project is the working directory read
// from the transcript (sanitized, capped at maxPathLen), or "" if none.
type Session struct {
	ID      string
	Parent  string
	Project string
	ModTime time.Time
}

// Listing is what ListSessions found. Omitted counts the sessions not listed:
// linked or non-regular transcripts, linked or unreadable project directories,
// and those past maxSessions.
type Listing struct {
	Sessions []Session // newest ModTime first, ties by ID ascending
	Omitted  int
}

// ListSessions lists <root>/<project>/<uuid>.jsonl, one level deep and never
// recursively. A missing root is an empty Listing and no error; a root that is
// not a directory or cannot be read is an error naming the base name only.
// The root itself may be a symbolic link (the user may link ~/.claude/projects
// elsewhere, and it is their own choice); anything below it may not.
func ListSessions(root string) (Listing, error) {
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return Listing{}, nil
	}
	if err != nil {
		return Listing{}, baseOnly(err)
	}
	if !info.IsDir() {
		return Listing{}, fmt.Errorf("agentusage: %s is not a directory", filepath.Base(root))
	}
	projects, err := os.ReadDir(root)
	if err != nil {
		return Listing{}, baseOnly(err)
	}
	var (
		list  Listing
		found []Session
	)
	for _, p := range projects {
		switch {
		case p.Type()&os.ModeSymlink != 0:
			list.Omitted++
		case p.IsDir():
			sessions, omitted := projectSessions(filepath.Join(root, p.Name()))
			found = append(found, sessions...)
			list.Omitted += omitted
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if !found[i].ModTime.Equal(found[j].ModTime) {
			return found[i].ModTime.After(found[j].ModTime)
		}
		return found[i].ID < found[j].ID
	})
	if len(found) > maxSessions {
		list.Omitted += len(found) - maxSessions
		found = found[:maxSessions]
	}
	for i := range found {
		found[i].Project = readProject(found[i].Parent)
	}
	list.Sessions = found
	return list, nil
}

// projectSessions returns the sessions directly inside dir, without their
// Project, and how many entries were refused. An unreadable dir counts as one.
func projectSessions(dir string) ([]Session, int) {
	f, _ := openPlain(dir, true)
	if f == nil {
		return nil, 1 // unreadable, or a link swapped in since the root was listed
	}
	defer func() { _ = f.Close() }()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, 1
	}
	var (
		out     []Session
		omitted int
	)
	for _, e := range entries {
		name := e.Name()
		id := strings.TrimSuffix(name, transcriptExt)
		if id == "" || id == name || strings.HasPrefix(name, agentPrefix) || e.IsDir() {
			continue // not a session: other file, old sub-agent layout, session directory
		}
		info, err := e.Info()
		if err != nil || !e.Type().IsRegular() {
			omitted++
			continue
		}
		out = append(out, Session{ID: sanitizeLabel(id), Parent: filepath.Join(dir, name), ModTime: info.ModTime()})
	}
	return out, omitted
}

// readProject returns the first non-empty string cwd found in the first
// cwdProbeBytes of the transcript at path, sanitized. Any failure, or no cwd,
// yields "". Nothing else of the transcript is kept.
func readProject(path string) string {
	f, _ := openPlain(path, false)
	if f == nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	r := &stopReader{r: io.LimitReader(f, cwdProbeBytes)}
	_, _ = scanLines(r, MaxLineBytes, func(line []byte, _ int64) {
		if r.stopped {
			return
		}
		var rec struct {
			Cwd json.RawMessage `json:"cwd"`
		}
		if json.Unmarshal(line, &rec) != nil {
			return
		}
		if cwd := optString(rec.Cwd); cwd != "" {
			r.project, r.stopped = sanitize(cwd, maxPathLen), true
		}
	})
	return r.project
}

// stopReader reads r until stopped is set, then reports io.EOF, so a scan ends
// once the cwd is found. project carries the result out of the callback.
type stopReader struct {
	r       io.Reader
	stopped bool
	project string
}

func (s *stopReader) Read(p []byte) (int, error) {
	if s.stopped {
		return 0, io.EOF
	}
	return s.r.Read(p)
}
