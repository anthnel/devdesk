package agentusage

import "io"

// ParseSession aggregates the usage of every agent of a session. It fails
// only if the parent transcript cannot be opened; a sub-agent that cannot be
// read stays in Agents with no tokens and a FileIssue. It is one full delta
// applied to an empty aggregate, the path the live reader takes (D5).
func ParseSession(files SessionFiles) (Usage, error) {
	a := NewAggregate(files.Parent)
	d := readSession(files, nil, a.Cursor(), timeNow())
	if d.parentErr != nil {
		return Usage{}, baseOnly(d.parentErr)
	}
	if err := a.Apply(d); err != nil {
		return Usage{}, err
	}
	return a.Usage(), nil
}

// parseReader folds r into t for agent; name is the file's sanitized base
// name and rank its position rank. It reports whether r ends with an
// incomplete line. A read error mid-stream keeps what was read and is
// recorded as an IssueReadError at the offset consumed; it is not an
// unreadable line.
func parseReader(t *table, agent, name, rank string, r io.Reader) bool {
	res, err := scanLines(r, MaxLineBytes, func(line []byte, off int64) {
		t.addLine(agent, name, pos{rank: rank, off: off}, line)
	})
	for _, off := range res.OversizedAt {
		t.addOversized(name, pos{rank: rank, off: off})
	}
	if err != nil {
		t.addFileIssue(name, pos{rank: rank, off: res.Consumed}, IssueReadError)
	}
	return len(res.Tail) > 0
}
