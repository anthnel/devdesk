package agentusage

import "time"

// Agent is the usage of one agent of a session: the orchestrator (Main) or a
// sub-agent.
type Agent struct {
	ID              string
	Main            bool
	Type            string // sub-agent type from its meta file; "" if unknown
	Models          []string
	Tokens          Tokens
	ByModel         []ModelUsage // per-model split of Tokens; the "" bucket (no model, or past maxModelsPerAgent) last
	Messages        int          // distinct message ids
	PartialMessages int          // messages whose last line has no stop reason
	State           State
	SpawnDepth      int
	ToolCalls       int       // distinct tool_use blocks of the agent's assistant lines
	FirstActivity   time.Time // earliest line timestamp (UTC); zero if none
	LastActivity    time.Time // latest line timestamp (UTC); zero if none
	// key is the table's identity for the agent's lines (the rank of its
	// transcript); empty means ID. finish clears it from its output.
	key string
}

// ModelUsage is the part of an agent's tokens billed to one model, with the
// share of its cache writes that used the 1 h cache. Summed over an agent's
// ByModel it equals Agent.Tokens.
type ModelUsage struct {
	Model        string // "" for messages that name no model, and for models past maxModelsPerAgent
	Tokens       Tokens
	CacheWrite1h int64 // of Tokens.CacheWrite, written with the 1 h cache
}

// CacheWrite5m is the cache writes that used the 5 min cache.
func (m ModelUsage) CacheWrite5m() int64 { return m.Tokens.CacheWrite - m.CacheWrite1h }

// Duration is LastActivity - FirstActivity, or 0 unless both are known.
func (a Agent) Duration() time.Duration {
	if a.FirstActivity.IsZero() || a.LastActivity.IsZero() {
		return 0
	}
	return a.LastActivity.Sub(a.FirstActivity)
}

// PartialOutput reports whether some message of the agent was still being
// written (no stop reason on its last line), so its output count is a floor.
func (a Agent) PartialOutput() bool { return a.PartialMessages > 0 }

// IssueKind classifies a line that could not be counted.
type IssueKind int

const (
	IssueUnreadable IssueKind = iota // not JSON, truncated, or bad counters
	IssueOversized                   // longer than MaxLineBytes
	IssueOpenFailed                  // a sub-agent transcript could not be opened
	IssueReadError                   // reading stopped on an I/O error at Offset
)

// FileIssue locates one skipped line. File is a base name, never a path.
type FileIssue struct {
	File   string
	Offset int64
	Kind   IssueKind
}

// Diagnostics counts what parsing skipped or had to guess.
type Diagnostics struct {
	UnreadableLines     int
	OversizedLines      int
	NoUsage             int
	NoMessageID         int
	CrossFileDuplicates int
	MetaMissing         int
	MetaUnreadable      int
	TruncatedFiles      []string
	Files               []FileIssue
}

// Degraded reports whether anything was skipped or guessed.
func (d Diagnostics) Degraded() bool {
	return d.UnreadableLines > 0 || d.OversizedLines > 0 || d.NoUsage > 0 ||
		d.NoMessageID > 0 || d.CrossFileDuplicates > 0 || d.MetaMissing > 0 ||
		d.MetaUnreadable > 0 || len(d.TruncatedFiles) > 0 || len(d.Files) > 0
}

// Usage is the parsed usage of one session.
type Usage struct {
	SessionID   string
	Agents      []Agent
	Versions    []string
	Diagnostics Diagnostics
	Billed      Billed
}

// Billed is the session cost Claude Code recorded itself, in the last
// cost-state line of the parent transcript.
type Billed struct {
	USD float64 // 0 unless Known
	// Known is false while the session runs (no cost-state yet), when any line
	// follows the last cost-state (the session was resumed), or when that line
	// is invalid or names another session.
	Known bool
}

// Total returns the tokens of every agent.
func (u Usage) Total() Tokens {
	var t Tokens
	for _, a := range u.Agents {
		t = t.Add(a.Tokens)
	}
	return t
}
