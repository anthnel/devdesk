package agentusage

// Agent is the usage of one agent of a session: the orchestrator (Main) or a
// sub-agent.
type Agent struct {
	ID              string
	Main            bool
	Type            string // sub-agent type from its meta file; "" if unknown
	Models          []string
	Tokens          Tokens
	Messages        int // distinct message ids
	PartialMessages int // messages whose last line has no stop reason
	State           State
	SpawnDepth      int
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
}

// Total returns the tokens of every agent.
func (u Usage) Total() Tokens {
	var t Tokens
	for _, a := range u.Agents {
		t = t.Add(a.Tokens)
	}
	return t
}
