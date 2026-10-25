package agentusage

// msgKey identifies one message of a session. A message with an id is keyed
// by the id alone; one without is keyed by where it was read.
type msgKey struct {
	id   string
	file string
	off  int64
}

// entry is the last state seen of one message.
type entry struct {
	agent    int
	file     string
	model    string
	tokens   Tokens
	stopNull bool
}

// aggregator folds records into per-agent totals, last line wins per
// message id. Agents must be added in a fixed order (parent, then
// sub-agents by ID); a message seen in several files goes to the last.
type aggregator struct {
	agents  []Agent
	diag    Diagnostics
	entries map[msgKey]*entry
	order   []msgKey // first appearance, for stable model order
	seen    map[string]bool
	version []string // distinct versions, first appearance order
	// fileIssues counts the capped FileIssue kinds kept per file name.
	fileIssues map[string]int
	noID       int            // table entries holding a line without message id
	spill      map[int]*spill // per agent: no-id lines past maxNoIDEntries
}

// spill totals the lines that no longer get a table entry.
type spill struct {
	tokens   Tokens
	messages int
	partial  int
	models   modelSet
}

// newAggregator starts from agents carrying their identity (ID, Main, Type,
// State, SpawnDepth); the counters are filled by add and finish.
func newAggregator(agents []Agent) *aggregator {
	return &aggregator{
		agents: agents, entries: map[msgKey]*entry{}, seen: map[string]bool{},
		fileIssues: map[string]int{}, spill: map[int]*spill{},
	}
}

// addIssue records a skipped line. Unreadable and oversized lines are kept
// at most maxFileIssuesPerFile per file (the counters are not capped); the
// other kinds occur once per file and are always kept.
func (a *aggregator) addIssue(i FileIssue) {
	if i.Kind == IssueUnreadable || i.Kind == IssueOversized {
		if a.fileIssues[i.File] >= maxFileIssuesPerFile {
			return
		}
		a.fileIssues[i.File]++
	}
	a.diag.Files = append(a.diag.Files, i)
}

// addLine decodes one line of agent agentIdx's file (file is a base name) and folds it in,
// counting what is skipped. Synthetic and ignored lines never count.
func (a *aggregator) addLine(agentIdx int, file string, off int64, line []byte) {
	r, kind := decodeLine(line)
	if kind != kindUnreadable {
		a.noteVersion(r.Version)
	}
	switch kind {
	case kindUsage:
		a.add(agentIdx, file, off, r)
	case kindNoUsage:
		a.diag.NoUsage++
	case kindUnreadable:
		a.diag.UnreadableLines++
		a.addIssue(FileIssue{File: file, Offset: off, Kind: IssueUnreadable})
	case kindIgnored, kindSynthetic:
	}
}

// noteVersion records a client version the first time it is seen, up to
// maxVersions.
func (a *aggregator) noteVersion(v string) {
	v = sanitizeLabel(v)
	if v == "" || a.seen[v] || len(a.version) >= maxVersions {
		return
	}
	a.seen[v] = true
	a.version = append(a.version, v)
}

// versions returns the distinct versions seen, in order of first appearance.
func (a *aggregator) versions() []string { return a.version }

// add folds one usage record in. file and off identify the line when the
// record has no message id.
func (a *aggregator) add(agentIdx int, file string, off int64, r record) {
	key := msgKey{id: r.MessageID}
	if r.MessageID == "" {
		key = msgKey{file: file, off: off}
		a.diag.NoMessageID++
		if a.noID >= maxNoIDEntries {
			a.addSpill(agentIdx, r)
			return
		}
		a.noID++
	}
	e, ok := a.entries[key]
	if !ok {
		e = &entry{}
		a.entries[key] = e
		a.order = append(a.order, key)
	} else if e.file != file {
		a.diag.CrossFileDuplicates++
	}
	*e = entry{agent: agentIdx, file: file, model: r.Model, tokens: r.Usage, stopNull: r.StopNull}
}

// addSpill counts a no-id line that gets no table entry.
func (a *aggregator) addSpill(agentIdx int, r record) {
	sp := a.spill[agentIdx]
	if sp == nil {
		sp = &spill{}
		a.spill[agentIdx] = sp
	}
	sp.tokens = sp.tokens.Add(r.Usage)
	sp.messages++
	if r.StopNull {
		sp.partial++
	}
	sp.models.add(r.Model)
}

// finish returns the agents with Tokens, Models, Messages and
// PartialMessages computed.
func (a *aggregator) finish() []Agent {
	models := make([]modelSet, len(a.agents))
	for i := range a.agents {
		a.agents[i].Tokens, a.agents[i].Models = Tokens{}, nil
		a.agents[i].Messages, a.agents[i].PartialMessages = 0, 0
	}
	for _, key := range a.order {
		e := a.entries[key]
		ag := &a.agents[e.agent]
		ag.Tokens = ag.Tokens.Add(e.tokens)
		ag.Messages++
		if e.stopNull {
			ag.PartialMessages++
		}
		models[e.agent].add(e.model)
	}
	for i, sp := range a.spill {
		ag := &a.agents[i]
		ag.Tokens = ag.Tokens.Add(sp.tokens)
		ag.Messages += sp.messages
		ag.PartialMessages += sp.partial
		for _, m := range sp.models.list {
			models[i].add(m)
		}
	}
	for i := range a.agents {
		a.agents[i].Models = models[i].list
	}
	return a.agents
}
