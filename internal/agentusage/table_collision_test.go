package agentusage

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func collisionLine(id string, out int) string {
	return fmt.Sprintf(`{"message":{"id":%q,"model":"x","stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":%d,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`+"\n", id, out)
}

// Two sub-agents whose IDs sanitize to the same label keep their own row and
// their own counts, as they did when attribution went by slice index.
func TestParseSessionSanitizedIDCollision(t *testing.T) {
	long := strings.Repeat("a", 200)
	cases := map[string][2]string{
		"control bytes": {"\x01", "\x02"},
		"long prefix":   {long + "1", long + "2"},
	}
	for name, ids := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			parent := filepath.Join(dir, "p.jsonl")
			writeFile(t, parent, "")
			var subs []SubAgentFiles
			for i, id := range ids {
				tr := filepath.Join(dir, fmt.Sprintf("t%d.jsonl", i))
				body := collisionLine("m-"+fmt.Sprint(i), []int{1, 10}[i])
				writeFile(t, tr, body)
				subs = append(subs, SubAgentFiles{ID: id, Transcript: tr})
			}
			u, err := ParseSession(SessionFiles{ID: "s", Parent: parent, SubAgents: subs})
			if err != nil {
				t.Fatal(err)
			}
			if len(u.Agents) != 3 {
				t.Fatalf("got %d agents, want 3", len(u.Agents))
			}
			for i, want := range []int64{1, 10} {
				ag := u.Agents[i+1]
				if ag.Tokens.Output != want || ag.Messages != 1 {
					t.Errorf("sub-agent %d: output=%d messages=%d, want %d/1", i, ag.Tokens.Output, ag.Messages, want)
				}
			}
		})
	}
}

// merge leaves o untouched and shares no mutable state with t.
func TestTableMergeSharesNothing(t *testing.T) {
	build := func() *table {
		return feed([]tline{
			sl("a", 0, verLine("1.0", "m1")),
			sl("a", 10, []byte("garbage")),
			sl("a", 20, []byte(`{"message":{"model":"x","stop_reason":"end_turn","usage":{"output_tokens":3}}}`)),
		})
	}
	o := build()
	o.entries[msgKey{id: "m1"}].otherFiles = []string{"x.jsonl"}
	o.spill["a"] = &spill{messages: 1, models: modelSet{}}
	o.spill["a"].models.add("x")
	snap := build()
	snap.entries[msgKey{id: "m1"}].otherFiles = []string{"x.jsonl"}
	snap.spill["a"] = &spill{messages: 1, models: modelSet{}}
	snap.spill["a"].models.add("x")

	dst := newTable()
	dst.merge(o)
	if !reflect.DeepEqual(o, snap) {
		t.Fatal("merge modified its argument")
	}
	// mutate dst deeply, o must not move
	for _, e := range dst.entries {
		e.otherFiles = append(e.otherFiles[:0], "mut")
		e.tokens.Output += 100
	}
	dst.spill["a"].messages += 5
	dst.spill["a"].models.add("mutated")
	for k := range dst.capped {
		dst.capped[k] = nil
	}
	dst.versions["new"] = pos{}
	if !reflect.DeepEqual(o, snap) {
		t.Fatal("mutating t after merge changed o")
	}
	// and the other way round
	dst2 := newTable()
	dst2.merge(o)
	for _, e := range o.entries {
		e.otherFiles = append(e.otherFiles[:0], "mut")
		e.tokens.Output += 100
	}
	o.spill["a"].models.add("mutated")
	for _, e := range dst2.entries {
		if len(e.otherFiles) > 0 && e.otherFiles[0] == "mut" || e.tokens.Output >= 100 {
			t.Fatal("mutating o after merge changed t")
		}
	}
	if len(dst2.spill["a"].models.list) != 1 {
		t.Fatal("spill models shared with o")
	}
}
