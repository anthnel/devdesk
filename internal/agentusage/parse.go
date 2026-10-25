package agentusage

import (
	"io"
	"path/filepath"
	"sort"
)

// ParseSession aggregates the usage of every agent of a session. It fails
// only if the parent transcript cannot be opened; a sub-agent that cannot be
// read stays in Agents with no tokens and a FileIssue.
func ParseSession(files SessionFiles) (Usage, error) {
	subs := append([]SubAgentFiles(nil), files.SubAgents...)
	sort.Slice(subs, func(i, j int) bool { return subs[i].ID < subs[j].ID })
	agg := newAggregator([]Agent{{Main: true, State: StateUnknown}})
	if err := parseFile(agg, 0, files.Parent); err != nil {
		return Usage{}, baseOnly(err)
	}
	for _, sa := range subs {
		agg.agents = append(agg.agents, agentFromMeta(&agg.diag, sa))
		if err := parseFile(agg, len(agg.agents)-1, sa.Transcript); err != nil {
			agg.addIssue(FileIssue{File: sanitizeLabel(filepath.Base(sa.Transcript)), Kind: IssueOpenFailed})
		}
	}
	return Usage{
		SessionID:   sanitizeLabel(files.ID),
		Agents:      agg.finish(),
		Versions:    agg.versions(),
		Diagnostics: agg.diag,
	}, nil
}

// agentFromMeta builds a sub-agent's identity from its meta file, counting
// a missing or unreadable one in diag.
func agentFromMeta(diag *Diagnostics, sa SubAgentFiles) Agent {
	ag := Agent{ID: sanitizeLabel(sa.ID)}
	if sa.Meta == "" {
		diag.MetaMissing++
		return ag
	}
	m, err := readMeta(sa.Meta)
	if err != nil {
		diag.MetaUnreadable++
		return ag
	}
	ag.Type, ag.State, ag.SpawnDepth = m.AgentType, m.state(), m.SpawnDepth
	return ag
}

// parseFile folds one transcript into the aggregator for agent idx. It
// fails only if the file cannot be opened.
func parseFile(agg *aggregator, idx int, path string) error {
	f, err := openPlain(path, false)
	if err != nil {
		return err
	}
	if f == nil {
		return errNotRegular
	}
	defer func() { _ = f.Close() }()
	parseReader(agg, idx, sanitizeLabel(filepath.Base(path)), f)
	return nil
}

// parseReader folds r into agg for agent idx; name is the file's sanitized
// base name. A read error mid-stream keeps what was read and is recorded as
// an IssueReadError at the offset consumed; it is not an unreadable line.
func parseReader(agg *aggregator, idx int, name string, r io.Reader) {
	start := len(agg.diag.Files)
	res, err := scanLines(r, MaxLineBytes, func(line []byte, off int64) {
		agg.addLine(idx, name, off, line)
	})
	for _, off := range res.OversizedAt {
		agg.diag.OversizedLines++
		agg.addIssue(FileIssue{File: name, Offset: off, Kind: IssueOversized})
	}
	if err != nil {
		agg.addIssue(FileIssue{File: name, Offset: res.Consumed, Kind: IssueReadError})
	}
	if len(res.Tail) > 0 {
		agg.diag.TruncatedFiles = append(agg.diag.TruncatedFiles, name)
	}
	issues := agg.diag.Files[start:]
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Offset < issues[j].Offset })
}
