package agentusage

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"time"
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
	write1h     int64 // of tokens.CacheWrite, written with the 1 h cache
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
		e.last, e.agent, e.model, e.tokens, e.write1h, e.stopNull = o.last, o.agent, o.model, o.tokens, o.write1h, o.stopNull
	}
	if o.first.compare(e.first) < 0 {
		e.first = o.first
	}
	e.addFile(o.file)
	for _, f := range o.otherFiles {
		e.addFile(f)
	}
}

// toolKey identifies one tool call of a message: its tool_use id, or, when
// the block has none, its place (apiBlockIndex and rank in content).
type toolKey struct {
	msg, id  string
	abi, idx int
}

// boundedID returns s, or, if it is longer than maxLabelLen, the hex SHA-256 of
// s (64 bytes): a fixed size, and no two long ids share a key.
func boundedID(s string) string {
	if len(s) <= maxLabelLen {
		return s
	}
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// toolSeen is where a tool call was last seen; the agent of the greatest
// position gets the call, like a message's tokens.
type toolSeen struct {
	p     pos
	agent string
}

// span is the earliest and latest timestamp seen for an agent.
type span struct{ first, last time.Time }

// fold widens s to include at.
func (s *span) fold(at time.Time) {
	if s.first.IsZero() || at.Before(s.first) {
		s.first = at
	}
	if at.After(s.last) {
		s.last = at
	}
}

// spill totals the lines that no longer get a table entry.
type spill struct {
	tokens   Tokens
	messages int
	partial  int
	models   modelSet
	byModel  modelTally
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
	// tools holds the tool calls of lines with a message id, deduplicated by
	// toolKey; noIDTools counts, per agent, those of lines without one (each
	// line is its own event, so a counter is exact and needs no key). spans is
	// the timestamp range per agent. Only tools is capped (maxToolEntries, the
	// overflow going to extraTools); the others are bounded by the agents.
	tools     map[toolKey]toolSeen
	noIDTools map[string]int
	// extraTools counts, per agent, the calls past maxToolEntries, which get
	// no entry. Like the cap on lines without id, which calls spill follows
	// position on a merge but arrival on a single pass.
	extraTools map[string]int
	spans      map[string]*span
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
	// billed is the cost-state line of the parent with the greatest position,
	// valid or not; parentEnd is the greatest position of any complete line of
	// the parent, readable or not. Both are maxima, so merging is commutative
	// and an incremental read agrees with a full one.
	billed    *billedAt
	parentEnd pos
}

// billedAt is a parent cost-state line.
type billedAt struct {
	p       pos
	usd     float64
	ok      bool   // totalCostUSD was a finite number >= 0
	session string // sessionId as sanitized; "" when absent
}

func newTable() *table {
	return &table{
		entries:    map[msgKey]*entry{},
		spill:      map[string]*spill{},
		tools:      map[toolKey]toolSeen{},
		noIDTools:  map[string]int{},
		extraTools: map[string]int{},
		spans:      map[string]*span{},
		capped:     map[string][]issueAt{},
		versions:   map[string]pos{},
	}
}

// addLine decodes one line of the file named name for agent ("" for the
// parent) and folds it. Synthetic and ignored lines never count.
func (t *table) addLine(agent, name string, p pos, line []byte) {
	r, kind := decodeLine(line)
	t.noteParentEnd(p)
	if kind == kindCostState {
		// No timestamp, version or count: only the parent's one is kept.
		if p.rank == parentRank {
			t.noteBilled(billedAt{p: p, usd: r.CostUSD, ok: r.CostOK, session: r.CostSession})
		}
		return
	}
	if kind != kindUnreadable {
		t.noteVersion(r.Version, p)
		t.noteActivity(agent, p, r)
	}
	switch kind {
	case kindUsage:
		t.addUsage(agent, name, p, r)
	case kindNoUsage:
		t.noUsage++
	case kindUnreadable:
		t.unreadable++
		t.addFileIssue(name, p, IssueUnreadable)
	case kindIgnored, kindSynthetic, kindCostState:
	}
}

// noteParentEnd moves parentEnd up to p when p is a line of the parent.
func (t *table) noteParentEnd(p pos) {
	if p.rank == parentRank && p.compare(t.parentEnd) > 0 {
		t.parentEnd = p
	}
}

// noteBilled keeps the parent cost-state of the greatest position.
func (t *table) noteBilled(b billedAt) {
	if t.billed == nil || b.p.compare(t.billed.p) > 0 {
		t.billed = &b
	}
}

// billedFor returns the session's recorded cost: known only when the last
// complete line of the parent is a valid cost-state of session (or one that
// names none). A line after it, even an unreadable one, means the session
// went on and the figure is stale.
func (t *table) billedFor(session string) Billed {
	b := t.billed
	if b == nil || !b.ok || b.p != t.parentEnd || (b.session != "" && b.session != session) {
		return Billed{}
	}
	return Billed{USD: b.usd, Known: true}
}

// noteActivity folds the timestamp and the tool calls of a readable line.
func (t *table) noteActivity(agent string, p pos, r record) {
	if !r.At.IsZero() {
		t.foldSpan(agent, r.At)
	}
	if r.MessageID == "" {
		if n := countLineTools(r.Tools); n > 0 {
			t.noIDTools[agent] += n
		}
		return
	}
	msg := boundedID(r.MessageID)
	for _, ref := range r.Tools {
		k := toolKey{msg: msg, id: boundedID(ref.id)}
		// Without a tool_use id the call is keyed by place: apiBlockIndex and
		// rank in content. Limit: with neither an id nor an apiBlockIndex, two
		// calls of one message split over two lines both fall on idx 0 and
		// count once (an undercount). Claude Code 2.1.302 always writes an id.
		if ref.id == "" {
			k.abi, k.idx = ref.abi, ref.idx
		}
		t.putTool(k, toolSeen{p: p, agent: agent})
	}
}

// countLineTools counts the tool calls of one line without message id: a
// tool_use id repeated in the line is one call, blocks without id each count.
func countLineTools(refs []toolRef) int {
	seen := make(map[string]bool, len(refs))
	n := 0
	for _, ref := range refs {
		if ref.id != "" && seen[ref.id] {
			continue
		}
		seen[ref.id] = true
		n++
	}
	return n
}

// foldSpan widens the span of agent to include at.
func (t *table) foldSpan(agent string, at time.Time) {
	sp := t.spans[agent]
	if sp == nil {
		sp = &span{}
		t.spans[agent] = sp
	}
	sp.fold(at)
}

// putTool keeps the greatest position seen for k. A call new to a full table
// is counted in extraTools instead.
func (t *table) putTool(k toolKey, s toolSeen) {
	cur, ok := t.tools[k]
	switch {
	case ok && s.p.compare(cur.p) < 0:
	case !ok && len(t.tools) >= maxToolEntries:
		t.extraTools[s.agent]++
	default:
		t.tools[k] = s
	}
}

// maxSelectInsertion is the largest excess trimTools selects by insertion;
// past it a full sort is cheaper.
const maxSelectInsertion = 256

// trimTools moves the entries past maxToolEntries, the greatest positions
// first, to extraTools. With a table already at the cap, as on every poll of
// a long session, the excess is small: the entries to evict are selected in
// one pass instead of sorting every key.
func (t *table) trimTools() {
	excess := len(t.tools) - maxToolEntries
	if excess <= 0 {
		return
	}
	var evict []toolKey
	if excess > maxSelectInsertion {
		evict = t.sortedToolKeys()[maxToolEntries:]
	} else {
		evict = t.greatestToolKeys(excess)
	}
	for _, k := range evict {
		t.extraTools[t.tools[k].agent]++
		delete(t.tools, k)
	}
}

// compareTools orders two tool calls by position, then by key: a total order,
// so the entries kept do not depend on map iteration.
func (t *table) compareTools(a, b toolKey) int {
	return cmp.Or(t.tools[a].p.compare(t.tools[b].p),
		cmp.Compare(a.msg, b.msg), cmp.Compare(a.id, b.id), cmp.Compare(a.abi, b.abi), cmp.Compare(a.idx, b.idx))
}

// sortedToolKeys returns every tool key in ascending order.
func (t *table) sortedToolKeys() []toolKey {
	keys := make([]toolKey, 0, len(t.tools))
	for k := range t.tools {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, t.compareTools)
	return keys
}

// greatestToolKeys returns the n greatest tool keys in ascending order,
// scanning the table once.
func (t *table) greatestToolKeys(n int) []toolKey {
	top := make([]toolKey, 0, n)
	for k := range t.tools {
		if len(top) == n {
			if t.compareTools(k, top[0]) <= 0 {
				continue
			}
			copy(top, top[1:])
			top = top[:n-1]
		}
		i, _ := slices.BinarySearchFunc(top, k, t.compareTools)
		top = slices.Insert(top, i, k)
	}
	return top
}

// addOversized counts a line longer than MaxLineBytes that started at p.
func (t *table) addOversized(name string, p pos) {
	t.oversized++
	t.noteParentEnd(p)
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
		model: r.Model, tokens: r.Usage, write1h: r.CacheWrite1h, stopNull: r.StopNull,
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
	sp.byModel.add(e.model, e.tokens, e.write1h)
}

func (s *spill) absorbRecord(r record) {
	s.tokens = s.tokens.Add(r.Usage)
	s.messages++
	if r.StopNull {
		s.partial++
	}
	s.models.add(r.Model)
	s.byModel.add(r.Model, r.Usage, r.CacheWrite1h)
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
		dst.byModel.merge(&sp.byModel)
	}
	t.mergeActivity(o)
	t.noteParentEnd(o.parentEnd)
	if o.billed != nil {
		t.noteBilled(*o.billed)
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

// mergeActivity folds o's tool calls and spans into t.
func (t *table) mergeActivity(o *table) {
	for k, s := range o.tools {
		if cur, ok := t.tools[k]; !ok || s.p.compare(cur.p) >= 0 {
			t.tools[k] = s // the cap is applied once below, by position
		}
	}
	t.trimTools()
	for agent, n := range o.noIDTools {
		t.noIDTools[agent] += n
	}
	for agent, n := range o.extraTools {
		t.extraTools[agent] += n
	}
	for agent, sp := range o.spans {
		t.foldSpan(agent, sp.first)
		t.foldSpan(agent, sp.last)
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
		out[i].Tokens, out[i].Models, out[i].ByModel = Tokens{}, nil, nil
		out[i].Messages, out[i].PartialMessages = 0, 0
		out[i].ToolCalls, out[i].FirstActivity, out[i].LastActivity = 0, time.Time{}, time.Time{}
		id := cmp.Or(out[i].key, out[i].ID)
		if out[i].Main {
			id = ""
		}
		out[i].key = ""
		idx[id] = i
	}
	models := make([]modelSet, len(out))
	tallies := make([]modelTally, len(out))
	cross := 0
	for _, k := range t.orderedKeys() {
		e := t.entries[k]
		cross += len(e.otherFiles)
		if i, ok := idx[e.agent]; ok {
			addEntry(&out[i], &models[i], &tallies[i], e)
		}
	}
	for agent, sp := range t.spill {
		if i, ok := idx[agent]; ok {
			addSpill(&out[i], &models[i], &tallies[i], sp)
		}
	}
	for i := range out {
		out[i].Models, out[i].ByModel = models[i].list, tallies[i].result()
	}
	t.addActivity(out, idx)
	return out, t.diagnostics(cross, extra), t.orderedVersions()
}

// addActivity sets the tool calls and the activity span of the agents.
func (t *table) addActivity(out []Agent, idx map[string]int) {
	for _, s := range t.tools {
		if i, ok := idx[s.agent]; ok {
			out[i].ToolCalls++
		}
	}
	for _, counts := range []map[string]int{t.noIDTools, t.extraTools} {
		for agent, n := range counts {
			if i, ok := idx[agent]; ok {
				out[i].ToolCalls += n
			}
		}
	}
	for agent, sp := range t.spans {
		if i, ok := idx[agent]; ok {
			out[i].FirstActivity, out[i].LastActivity = sp.first, sp.last
		}
	}
}

// addEntry adds one message's final state to its agent.
func addEntry(ag *Agent, ms *modelSet, mt *modelTally, e *entry) {
	ag.Tokens = ag.Tokens.Add(e.tokens)
	ag.Messages++
	if e.stopNull {
		ag.PartialMessages++
	}
	ms.add(e.model)
	mt.add(e.model, e.tokens, e.write1h)
}

// addSpill adds a spill's totals to its agent.
func addSpill(ag *Agent, ms *modelSet, mt *modelTally, sp *spill) {
	ag.Tokens = ag.Tokens.Add(sp.tokens)
	ag.Messages += sp.messages
	ag.PartialMessages += sp.partial
	for _, m := range sp.models.list {
		ms.add(m)
	}
	mt.merge(&sp.byModel)
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
	return t.billed != nil || len(t.entries) > 0 || len(t.spill) > 0 || len(t.spans) > 0 || len(t.tools) > 0 || len(t.noIDTools) > 0 || len(t.extraTools) > 0 || t.unreadable+t.noUsage+t.noMessageID+t.oversized > 0
}
