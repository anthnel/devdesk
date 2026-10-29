package agentusage

import (
	"cmp"
	"slices"
)

// pos orders lines the way ParseSession reads them (D3).
type pos struct {
	rank string // "0" parent; "1"+agentID+"\x00"+baseName for a sub-agent
	off  int64
}

// parentRank is the rank of the parent transcript; it sorts before every
// sub-agent rank.
const parentRank = "0"

// subAgentRank is the rank of a sub-agent's transcript: sub-agents are read by
// ID, then by base name.
func subAgentRank(agentID, name string) string { return "1" + agentID + "\x00" + name }

// transcriptRank is subAgentRank made unique per transcript: the raw ID and
// the path break ties between sub-agents whose sanitized IDs and base names
// coincide. It is a function of the transcript alone, so it is stable from one
// pass to the next, and it doubles as the agent's key in a table.
func transcriptRank(agentID, name, rawID, path string) string {
	return subAgentRank(agentID, name) + "\x00" + rawID + "\x00" + path
}

// compare orders positions by rank, then by offset.
func (p pos) compare(o pos) int {
	if c := cmp.Compare(p.rank, o.rank); c != 0 {
		return c
	}
	return cmp.Compare(p.off, o.off)
}

// msgKey identifies one message of a session. A message with an id is keyed
// by the id alone; one without is keyed by where it was read (file rank and
// offset).
type msgKey struct {
	id   string
	file string
	off  int64
}

// entry is the last state seen of one message. Which line is "last" is a
// function of positions, never of arrival.
type entry struct {
	first, last pos
	agent       string // agent ID of the last line; "" for the parent
	file        string // base name of one file the message was seen in
	otherFiles  []string
	model       string
	tokens      Tokens
	stopNull    bool
}

// addFile remembers that the message was also seen in the file named name.
func (e *entry) addFile(name string) {
	if e.file == name || slices.Contains(e.otherFiles, name) {
		return
	}
	e.otherFiles = append(e.otherFiles, name)
}

// absorb folds o into e: the greatest position gives the state, the smallest
// gives first, and the files are united.
func (e *entry) absorb(o *entry) {
	if o.last.compare(e.last) >= 0 {
		e.last, e.agent, e.model, e.tokens, e.stopNull = o.last, o.agent, o.model, o.tokens, o.stopNull
	}
	if o.first.compare(e.first) < 0 {
		e.first = o.first
	}
	e.addFile(o.file)
	for _, f := range o.otherFiles {
		e.addFile(f)
	}
}

// spill totals the lines that no longer get a table entry.
type spill struct {
	tokens   Tokens
	messages int
	partial  int
	models   modelSet
}

// issueAt is a FileIssue with the position of the line it describes.
type issueAt struct {
	p  pos
	fi FileIssue
}

// compare orders issues by position, then kind, then file.
func (a issueAt) compare(b issueAt) int {
	if c := a.p.compare(b.p); c != 0 {
		return c
	}
	if c := cmp.Compare(a.fi.Kind, b.fi.Kind); c != 0 {
		return c
	}
	return cmp.Compare(a.fi.File, b.fi.File)
}

// table folds lines into per-key last states, ordered by position (D3). It
// is both the content of a Delta and the state of an Aggregate. Two declared
// exceptions to order-freedom: which no-id lines spill past maxNoIDEntries
// (the smallest positions of each merge are kept, see mergeCapped), and which
// models fill a spill's maxModelsPerAgent (arrival order).
type table struct {
	entries map[msgKey]*entry
	noID    int // entries holding a line without message id
	spill   map[string]*spill
	// counters of lines; CrossFileDuplicates is derived by finish.
	unreadable, noUsage, noMessageID, oversized int
	// capped holds the unreadable/oversized issues per file name, trimmed to
	// the maxFileIssuesPerFile smallest positions; stateIssues holds the
	// other kinds, which occur once per file.
	capped      map[string][]issueAt
	stateIssues []issueAt
	// versions maps a distinct client version to its first position, at most
	// maxVersions of them (the smallest positions).
	versions map[string]pos
}

func newTable() *table {
	return &table{
		entries:  map[msgKey]*entry{},
		spill:    map[string]*spill{},
		capped:   map[string][]issueAt{},
		versions: map[string]pos{},
	}
}

// addLine decodes one line of the file named name for agent ("" for the
// parent) and folds it. Synthetic and ignored lines never count.
func (t *table) addLine(agent, name string, p pos, line []byte) {
	r, kind := decodeLine(line)
	if kind != kindUnreadable {
		t.noteVersion(r.Version, p)
	}
	switch kind {
	case kindUsage:
		t.addUsage(agent, name, p, r)
	case kindNoUsage:
		t.noUsage++
	case kindUnreadable:
		t.unreadable++
		t.addFileIssue(name, p, IssueUnreadable)
	case kindIgnored, kindSynthetic:
	}
}

// addOversized counts a line longer than MaxLineBytes that started at p.
func (t *table) addOversized(name string, p pos) {
	t.oversized++
	t.addFileIssue(name, p, IssueOversized)
}

// addUsage folds one usage record in.
func (t *table) addUsage(agent, name string, p pos, r record) {
	key := msgKey{id: r.MessageID}
	if r.MessageID == "" {
		key = msgKey{file: p.rank, off: p.off}
		t.noMessageID++
		if _, ok := t.entries[key]; !ok {
			if t.noID >= maxNoIDEntries {
				t.addSpill(agent, r)
				return
			}
			t.noID++
		}
	}
	t.put(key, &entry{
		first: p, last: p, agent: agent, file: name,
		model: r.Model, tokens: r.Usage, stopNull: r.StopNull,
	})
}

// put folds e into the table under key; the table keeps its own copy.
func (t *table) put(key msgKey, e *entry) {
	if cur, ok := t.entries[key]; ok {
		cur.absorb(e)
		return
	}
	c := *e
	c.otherFiles = slices.Clone(e.otherFiles)
	t.entries[key] = &c
}

// addSpill counts a no-id line that gets no table entry.
func (t *table) addSpill(agent string, r record) {
	sp := t.spill[agent]
	if sp == nil {
		sp = &spill{}
		t.spill[agent] = sp
	}
	sp.absorbRecord(r)
}

// spillEntry totals an entry that gets no slot in the table.
func (t *table) spillEntry(e *entry) {
	sp := t.spill[e.agent]
	if sp == nil {
		sp = &spill{}
		t.spill[e.agent] = sp
	}
	sp.tokens = sp.tokens.Add(e.tokens)
	sp.messages++
	if e.stopNull {
		sp.partial++
	}
	sp.models.add(e.model)
}

func (s *spill) absorbRecord(r record) {
	s.tokens = s.tokens.Add(r.Usage)
	s.messages++
	if r.StopNull {
		s.partial++
	}
	s.models.add(r.Model)
}

// noteVersion records a client version with its first position, keeping the
// maxVersions smallest.
func (t *table) noteVersion(v string, p pos) {
	v = sanitizeLabel(v)
	if v == "" {
		return
	}
	if cur, ok := t.versions[v]; ok {
		if p.compare(cur) < 0 {
			t.versions[v] = p
		}
		return
	}
	if len(t.versions) >= maxVersions {
		worst, wp := "", pos{}
		for k, kp := range t.versions {
			if worst == "" || kp.compare(wp) > 0 {
				worst, wp = k, kp
			}
		}
		if p.compare(wp) >= 0 {
			return
		}
		delete(t.versions, worst)
	}
	t.versions[v] = p
}

// addFileIssue records an issue of the file named name. Unreadable and
// oversized ones are kept at most maxFileIssuesPerFile per file (the counters
// are not capped); the other kinds occur once per file and are always kept.
func (t *table) addFileIssue(name string, p pos, kind IssueKind) {
	it := issueAt{p: p, fi: FileIssue{File: name, Offset: p.off, Kind: kind}}
	if kind != IssueUnreadable && kind != IssueOversized {
		t.stateIssues = append(t.stateIssues, it)
		return
	}
	t.capped[name] = trimIssues(append(t.capped[name], it), 2*maxFileIssuesPerFile)
}

// trimIssues keeps the maxFileIssuesPerFile smallest issues once s has grown
// to limit entries.
func trimIssues(s []issueAt, limit int) []issueAt {
	if len(s) < limit {
		return s
	}
	slices.SortFunc(s, issueAt.compare)
	return s[:min(len(s), maxFileIssuesPerFile)]
}

// merge folds o into t as if o's lines had been added to t. o is not modified
// and shares nothing with t afterwards.
func (t *table) merge(o *table) { t.mergeSkipping(o, nil) }

// mergeCapped is merge with the per-session bound on lines without id: the
// ones o adds past maxNoIDEntries are spilled instead of getting an entry,
// the earliest positions being kept. Which lines spill therefore follows
// position, not arrival, but the totals do not depend on it.
func (t *table) mergeCapped(o *table) {
	var fresh []msgKey
	for k := range o.entries {
		if _, had := t.entries[k]; k.id == "" && !had {
			fresh = append(fresh, k)
		}
	}
	room := max(maxNoIDEntries-t.noID, 0)
	if len(fresh) <= room {
		t.merge(o)
		return
	}
	slices.SortFunc(fresh, func(a, b msgKey) int { return o.entries[a].first.compare(o.entries[b].first) })
	skip := make(map[msgKey]bool, len(fresh)-room)
	for _, k := range fresh[room:] {
		skip[k] = true
	}
	t.mergeSkipping(o, skip)
}

// mergeSkipping is merge, except that the entries named by skip are spilled.
func (t *table) mergeSkipping(o *table, skip map[msgKey]bool) {
	for k, e := range o.entries {
		if skip[k] {
			t.spillEntry(e)
			continue
		}
		_, had := t.entries[k]
		t.put(k, e)
		if !had && k.id == "" {
			t.noID++
		}
	}
	for agent, sp := range o.spill {
		dst := t.spill[agent]
		if dst == nil {
			dst = &spill{}
			t.spill[agent] = dst
		}
		dst.tokens = dst.tokens.Add(sp.tokens)
		dst.messages += sp.messages
		dst.partial += sp.partial
		for _, m := range sp.models.list {
			dst.models.add(m)
		}
	}
	t.unreadable += o.unreadable
	t.noUsage += o.noUsage
	t.noMessageID += o.noMessageID
	t.oversized += o.oversized
	for name, s := range o.capped {
		t.capped[name] = trimIssues(append(t.capped[name], s...), 2*maxFileIssuesPerFile)
	}
	t.stateIssues = append(t.stateIssues, o.stateIssues...)
	for v, p := range o.versions {
		t.noteVersion(v, p)
	}
}

// orderedKeys returns the entry keys by first position: the order messages
// first appear in a sequential read.
func (t *table) orderedKeys() []msgKey {
	keys := make([]msgKey, 0, len(t.entries))
	for k := range t.entries {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b msgKey) int {
		if c := t.entries[a].first.compare(t.entries[b].first); c != 0 {
			return c
		}
		return cmp.Or(cmp.Compare(a.id, b.id), cmp.Compare(a.file, b.file), cmp.Compare(a.off, b.off))
	})
	return keys
}

// finish computes the counters of the agents given (identity fields set, the
// parent flagged Main), the line diagnostics and the versions, all deep
// copies. Agents are matched by key (ID when unset); the Main agent matches
// the parent's "".
func (t *table) finish(agents []Agent) ([]Agent, Diagnostics, []string) {
	return t.finishWith(agents, nil)
}

// finishWith is finish with extra file issues, kept in position order among
// the table's own: a session reader passes the states of its cursors.
func (t *table) finishWith(agents []Agent, extra []issueAt) ([]Agent, Diagnostics, []string) {
	out := slices.Clone(agents)
	idx := make(map[string]int, len(out))
	for i := range out {
		out[i].Tokens, out[i].Models = Tokens{}, nil
		out[i].Messages, out[i].PartialMessages = 0, 0
		id := cmp.Or(out[i].key, out[i].ID)
		if out[i].Main {
			id = ""
		}
		out[i].key = ""
		idx[id] = i
	}
	models := make([]modelSet, len(out))
	cross := 0
	for _, k := range t.orderedKeys() {
		e := t.entries[k]
		cross += len(e.otherFiles)
		if i, ok := idx[e.agent]; ok {
			addEntry(&out[i], &models[i], e)
		}
	}
	for agent, sp := range t.spill {
		if i, ok := idx[agent]; ok {
			addSpill(&out[i], &models[i], sp)
		}
	}
	for i := range out {
		out[i].Models = models[i].list
	}
	return out, t.diagnostics(cross, extra), t.orderedVersions()
}

// addEntry adds one message's final state to its agent.
func addEntry(ag *Agent, ms *modelSet, e *entry) {
	ag.Tokens = ag.Tokens.Add(e.tokens)
	ag.Messages++
	if e.stopNull {
		ag.PartialMessages++
	}
	ms.add(e.model)
}

// addSpill adds a spill's totals to its agent.
func addSpill(ag *Agent, ms *modelSet, sp *spill) {
	ag.Tokens = ag.Tokens.Add(sp.tokens)
	ag.Messages += sp.messages
	ag.PartialMessages += sp.partial
	for _, m := range sp.models.list {
		ms.add(m)
	}
}

// diagnostics builds the line diagnostics; cross is the number of cross-file
// duplicates found by finish and extra holds issues that are not the table's.
func (t *table) diagnostics(cross int, extra []issueAt) Diagnostics {
	d := Diagnostics{
		UnreadableLines: t.unreadable, OversizedLines: t.oversized,
		NoUsage: t.noUsage, NoMessageID: t.noMessageID, CrossFileDuplicates: cross,
	}
	all := append(slices.Clone(t.stateIssues), extra...)
	for _, s := range t.capped {
		sorted := slices.Clone(s)
		slices.SortFunc(sorted, issueAt.compare)
		all = append(all, sorted[:min(len(sorted), maxFileIssuesPerFile)]...)
	}
	slices.SortFunc(all, issueAt.compare)
	for _, it := range all {
		d.Files = append(d.Files, it.fi)
	}
	return d
}

// orderedVersions returns the versions by first position.
func (t *table) orderedVersions() []string {
	var vs []string
	for v := range t.versions {
		vs = append(vs, v)
	}
	slices.SortFunc(vs, func(a, b string) int { return t.versions[a].compare(t.versions[b]) })
	return vs
}

// hasLines reports whether t holds anything folded from a complete line.
func (t *table) hasLines() bool {
	if t == nil {
		return false
	}
	return len(t.entries) > 0 || len(t.spill) > 0 || t.unreadable+t.noUsage+t.noMessageID+t.oversized > 0
}
