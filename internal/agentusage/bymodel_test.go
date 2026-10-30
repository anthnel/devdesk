package agentusage

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
)

// DEVDESK-47 step 1: per-model tokens (Agent.ByModel) and the 1 h cache-write
// split (ModelUsage.CacheWrite1h). Tokens itself is not touched.

// splitLine builds a transcript line whose usage carries cache_creation with
// the given raw ephemeral_1h value ("" omits the cache_creation object).
// total is cache_creation_input_tokens.
func splitLine(id, model string, in, out, total int64, raw1h string) []byte {
	cc := ""
	if raw1h != "" {
		cc = fmt.Sprintf(`,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":%s}`, raw1h)
	}
	idKey := ""
	if id != "" {
		idKey = `"id":"` + id + `",`
	}
	return []byte(fmt.Sprintf(`{"version":"2.1.302","message":{%s"model":"%s","stop_reason":"end_turn","usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":0,"cache_creation_input_tokens":%d%s}}}`,
		idKey, model, in, out, total, cc))
}

func TestDecodeLineCacheWriteSplit(t *testing.T) {
	tests := []struct {
		name   string
		raw1h  string
		total  int64
		want1h int64
	}{
		{"5m and 1h split", "200", 300, 200},
		{"no cache_creation object", "", 300, 0},
		{"1h larger than the total is clamped", "500", 300, 300},
		{"1h equals the total", "300", 300, 300},
		{"negative 1h is 0", "-1", 300, 0},
		{"fractional 1h is 0", "1.5", 300, 0},
		{"string 1h is 0", `"x"`, 300, 0},
		{"null 1h is 0", "null", 300, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, kind := decodeLine(splitLine("m1", "m", 1, 2, tt.total, tt.raw1h))
			if kind != kindUsage {
				t.Fatalf("kind = %v, want kindUsage (a bad split must not make the line unreadable)", kind)
			}
			if want := (Tokens{Input: 1, Output: 2, CacheWrite: tt.total}); rec.Usage != want {
				t.Errorf("Usage = %+v, want %+v (token totals must not move)", rec.Usage, want)
			}
			if rec.CacheWrite1h != tt.want1h {
				t.Errorf("CacheWrite1h = %d, want %d", rec.CacheWrite1h, tt.want1h)
			}
		})
	}
}

func TestModelUsageCacheWrite5m(t *testing.T) {
	m := ModelUsage{Model: "m", Tokens: Tokens{CacheWrite: 300}, CacheWrite1h: 200}
	if got := m.CacheWrite5m(); got != 100 {
		t.Errorf("CacheWrite5m = %d, want 100", got)
	}
}

func TestAggregateByModel(t *testing.T) {
	type line struct {
		id, model string
		in, out   int64
		cw        int64
		raw1h     string
	}
	tests := []struct {
		name  string
		lines []line
		want  []ModelUsage
	}{
		{"two models, first-appearance order",
			[]line{
				{"m-a", "model-a", 1, 2, 300, "200"},
				{"m-b", "model-b", 10, 20, 50, ""},
				{"m-c", "model-a", 3, 4, 100, "100"},
			},
			[]ModelUsage{
				{Model: "model-a", Tokens: Tokens{Input: 4, Output: 6, CacheWrite: 400}, CacheWrite1h: 300},
				{Model: "model-b", Tokens: Tokens{Input: 10, Output: 20, CacheWrite: 50}},
			}},
		{"no model goes to a last bucket",
			[]line{
				{"m-a", "", 5, 5, 10, ""},
				{"m-b", "model-b", 1, 1, 0, ""},
			},
			[]ModelUsage{
				{Model: "model-b", Tokens: Tokens{Input: 1, Output: 1}},
				{Model: "", Tokens: Tokens{Input: 5, Output: 5, CacheWrite: 10}},
			}},
		{"1h clamped to the line's total",
			[]line{{"m-a", "model-a", 0, 0, 300, "500"}},
			[]ModelUsage{{Model: "model-a", Tokens: Tokens{CacheWrite: 300}, CacheWrite1h: 300}}},
		{"last line of a message wins with its split",
			[]line{
				{"m-a", "model-a", 1, 1, 300, ""},
				{"m-a", "model-a", 1, 9, 300, "200"},
			},
			[]ModelUsage{{Model: "model-a", Tokens: Tokens{Input: 1, Output: 9, CacheWrite: 300}, CacheWrite1h: 200}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestAgg([]Agent{{ID: "main", Main: true}})
			var off int64
			for _, l := range tt.lines {
				a.addLine(0, "p.jsonl", off, splitLine(l.id, l.model, l.in, l.out, l.cw, l.raw1h))
				off += 1000
			}
			ag := a.finish()[0]
			if !reflect.DeepEqual(ag.ByModel, tt.want) {
				t.Errorf("ByModel = %+v\nwant      %+v", ag.ByModel, tt.want)
			}
			assertByModelSumsToTokens(t, ag)
		})
	}
}

func assertByModelSumsToTokens(t *testing.T, ag Agent) {
	t.Helper()
	var sum Tokens
	for _, m := range ag.ByModel {
		sum = sum.Add(m.Tokens)
		if m.CacheWrite1h < 0 || m.CacheWrite1h > m.Tokens.CacheWrite {
			t.Errorf("model %q: CacheWrite1h %d outside [0, %d]", m.Model, m.CacheWrite1h, m.Tokens.CacheWrite)
		}
	}
	if sum != ag.Tokens {
		t.Errorf("sum of ByModel = %+v, Agent.Tokens = %+v", sum, ag.Tokens)
	}
}

func TestAggregateByModelEachAgentApart(t *testing.T) {
	a := twoAgents()
	a.addLine(0, "p.jsonl", 0, splitLine("m1", "model-a", 1, 1, 10, "4"))
	a.addLine(1, "a1.jsonl", 0, splitLine("m2", "model-b", 2, 2, 20, "20"))
	agents := a.finish()
	if got, want := agents[0].ByModel, []ModelUsage{{Model: "model-a", Tokens: Tokens{Input: 1, Output: 1, CacheWrite: 10}, CacheWrite1h: 4}}; !reflect.DeepEqual(got, want) {
		t.Errorf("main ByModel = %+v, want %+v", got, want)
	}
	if got, want := agents[1].ByModel, []ModelUsage{{Model: "model-b", Tokens: Tokens{Input: 2, Output: 2, CacheWrite: 20}, CacheWrite1h: 20}}; !reflect.DeepEqual(got, want) {
		t.Errorf("sub-agent ByModel = %+v, want %+v", got, want)
	}
}

func TestAggregateByModelSyntheticExcluded(t *testing.T) {
	a := newTestAgg([]Agent{{ID: "main", Main: true}})
	a.addLine(0, "p.jsonl", 0, splitLine("m1", "model-a", 1, 1, 0, ""))
	a.addLine(0, "p.jsonl", 10, splitLine("m2", "<synthetic>", 0, 0, 0, ""))
	ag := a.finish()[0]
	if len(ag.ByModel) != 1 || ag.ByModel[0].Model != "model-a" {
		t.Errorf("ByModel = %+v, want only model-a", ag.ByModel)
	}
	assertByModelSumsToTokens(t, ag)
}

// Past maxNoIDEntries the table spills the oldest id-less entries into
// totals; the per-model figures must spill with them.
func TestAggregateByModelSurvivesSpill(t *testing.T) {
	const n = maxNoIDEntries + 5000
	a := newTestAgg([]Agent{{ID: "main", Main: true}})
	models := []string{"model-a", "model-b"}
	for i := 0; i < n; i++ {
		raw := ""
		if i%3 == 0 {
			raw = "1"
		}
		a.addLine(0, "p.jsonl", int64(i)*1000, splitLine("", models[i%2], 1, 2, 3, raw))
	}
	ag := a.finish()[0]
	if want := (Tokens{Input: n, Output: 2 * n, CacheWrite: 3 * n}); ag.Tokens != want {
		t.Fatalf("Tokens = %+v, want %+v", ag.Tokens, want)
	}
	if len(ag.ByModel) != 2 {
		t.Fatalf("ByModel = %+v, want 2 entries", ag.ByModel)
	}
	assertByModelSumsToTokens(t, ag)
	var one int64
	for _, m := range ag.ByModel {
		one += m.CacheWrite1h
	}
	if want := int64((n + 2) / 3); one != want {
		t.Errorf("total CacheWrite1h = %d, want %d", one, want)
	}
}

// The oracle: ReadDelta/Apply over chunked appends equals ParseSession, now
// including ByModel and the split.
func TestReadDeltaByModelMatchesParse(t *testing.T) {
	var parent, sub bytes.Buffer
	for i, l := range [][]byte{
		splitLine("m1", "model-a", 1, 1, 300, "200"),
		splitLine("m2", "model-b", 2, 2, 50, ""),
		splitLine("m3", "", 3, 3, 7, "9"),
		splitLine("m1", "model-a", 1, 5, 300, "250"), // same message, later line
		splitLine("", "model-b", 4, 4, 10, "10"),
	} {
		parent.Write(l)
		parent.WriteByte('\n')
		if i%2 == 0 {
			sub.Write(splitLine(fmt.Sprintf("s%d", i), "model-c", 1, 1, 4, "1"))
			sub.WriteByte('\n')
		}
	}
	for _, size := range []int{1, 7, 64, 1 << 20} {
		t.Run(fmt.Sprintf("chunk %d", size), func(t *testing.T) {
			s := newSess(t)
			s.put(s.parent(), nil)
			s.putMeta("a1", metaFinished, 0)
			s.put(s.sub("a1"), nil)
			a := NewAggregate(s.parent())
			readApply(t, a)
			pd, sd := parent.Bytes(), sub.Bytes()
			for po, so := 0, 0; po < len(pd) || so < len(sd); {
				if po < len(pd) {
					e := min(po+size, len(pd))
					s.add(s.parent(), pd[po:e])
					po = e
				}
				if so < len(sd) {
					e := min(so+size, len(sd))
					s.add(s.sub("a1"), sd[so:e])
					so = e
				}
				readApply(t, a)
				assertMatchesParse(t, a, s)
			}
			u := a.Usage()
			if len(u.Agents[0].ByModel) != 3 {
				t.Errorf("main ByModel = %+v, want 3 entries (model-a, model-b, \"\")", u.Agents[0].ByModel)
			}
			for _, ag := range u.Agents {
				assertByModelSumsToTokens(t, ag)
			}
		})
	}
}

// Past maxModelsPerAgent named models the extra ones fold into the unnamed
// bucket: 64 named + "" = 65 entries, "" last, and nothing is lost.
func TestAggregateByModelCappedFoldsIntoTheUnnamedBucket(t *testing.T) {
	const extra = 30
	n := maxModelsPerAgent + extra
	a := newTestAgg([]Agent{{ID: "main", Main: true}})
	for i := 0; i < n; i++ {
		a.addLine(0, "p.jsonl", int64(i)*1000, splitLine(fmt.Sprintf("id%d", i), fmt.Sprintf("model-%d", i), 1, 2, 3, "1"))
	}
	ag := a.finish()[0]
	if len(ag.ByModel) != maxModelsPerAgent+1 {
		t.Fatalf("len(ByModel) = %d, want %d", len(ag.ByModel), maxModelsPerAgent+1)
	}
	last := ag.ByModel[len(ag.ByModel)-1]
	if last.Model != "" {
		t.Fatalf("last bucket = %q, want the unnamed one", last.Model)
	}
	if want := (Tokens{Input: extra, Output: 2 * extra, CacheWrite: 3 * extra}); last.Tokens != want || last.CacheWrite1h != extra {
		t.Errorf("unnamed bucket = %+v (1h %d), want the %d overflow messages", last.Tokens, last.CacheWrite1h, extra)
	}
	for i, m := range ag.ByModel[:maxModelsPerAgent] {
		if m.Model != fmt.Sprintf("model-%d", i) {
			t.Fatalf("ByModel[%d] = %q, want first-seen order", i, m.Model)
		}
	}
	if want := (Tokens{Input: int64(n), Output: 2 * int64(n), CacheWrite: 3 * int64(n)}); ag.Tokens != want {
		t.Errorf("Tokens = %+v, want %+v", ag.Tokens, want)
	}
	assertByModelSumsToTokens(t, ag)
}
