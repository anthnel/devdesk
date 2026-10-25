package agentusage

import (
	"encoding/json"
	"errors"
	"io"
)

// State tells whether a sub-agent has finished.
type State int

const (
	StateUnknown State = iota
	StateFinished
	StateUnfinished
)

// meta is the subset of agent-<id>.meta.json that is read. The description
// and worktree path keys are deliberately absent: they are never decoded.
type meta struct {
	AgentType  string
	Finished   *bool
	SpawnDepth int
}

// wireMeta keeps every field raw: one of an unexpected type costs that field
// only. A finished value that is not a boolean yields StateUnknown, not an
// error.
type wireMeta struct {
	AgentType  json.RawMessage `json:"agentType"`
	Finished   json.RawMessage `json:"finished"`
	SpawnDepth json.RawMessage `json:"spawnDepth"`
}

// maxMetaBytes is the largest meta file read; a bigger one is refused.
const maxMetaBytes = 1 << 20

// Fixed messages because encoding/json errors can quote file content.
var (
	errMalformedMeta = errors.New("agentusage: malformed meta file")
	errMetaTooBig    = errors.New("agentusage: meta file too large")
	errMetaNotPlain  = errors.New("agentusage: meta file is not a regular file")
)

// readMeta reads a meta file. A missing file yields an error satisfying
// errors.Is(err, os.ErrNotExist). A symbolic link or non-regular file is
// refused, and reading is bounded by maxMetaBytes.
func readMeta(path string) (meta, error) {
	f, err := openPlain(path, false)
	if err != nil {
		return meta{}, err
	}
	if f == nil {
		return meta{}, errMetaNotPlain
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxMetaBytes+1))
	if err != nil {
		return meta{}, err
	}
	if len(b) > maxMetaBytes {
		return meta{}, errMetaTooBig
	}
	var w wireMeta
	if json.Unmarshal(b, &w) != nil {
		return meta{}, errMalformedMeta
	}
	m := meta{AgentType: sanitizeLabel(optString(w.AgentType))}
	var fin bool
	if json.Unmarshal(w.Finished, &fin) == nil && string(w.Finished) != "null" {
		m.Finished = &fin // absent, null or a wrong type: stays nil
	}
	if len(w.SpawnDepth) > 0 {
		_ = json.Unmarshal(w.SpawnDepth, &m.SpawnDepth) // wrong type: stays 0
		m.SpawnDepth = max(m.SpawnDepth, 0)
	}
	return m, nil
}

// state maps the finished flag; absent or null is StateUnknown.
func (m meta) state() State {
	switch {
	case m.Finished == nil:
		return StateUnknown
	case *m.Finished:
		return StateFinished
	}
	return StateUnfinished
}
