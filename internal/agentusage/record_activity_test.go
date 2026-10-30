package agentusage

import (
	"reflect"
	"testing"
	"time"
)

// DEVDESK-46 step 1: decodeLine also reads the root type, the root timestamp
// (UTC) and, in message.content, the type and id of each block. Synthetic
// transcripts only.

const usageObj = `"usage":{"input_tokens":1,"output_tokens":2}`

func tsAt(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return at.UTC()
}

func TestDecodeLineActivity(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		kind  lineKind
		at    string // RFC 3339; "" means the zero time
		tools []toolRef
	}{
		{"assistant with one tool_use", `{"type":"assistant","apiBlockIndex":0,"timestamp":"2026-10-24T07:38:02.203Z","message":{"id":"msg_1","model":"m","stop_reason":null,"content":[{"type":"tool_use","id":"toolu_1","name":"X","input":{}}],` + usageObj + `}}`,
			kindUsage, "2026-10-24T07:38:02.203Z", []toolRef{{id: "toolu_1", abi: 0, idx: 0}}},
		{"thinking then two tool_use", `{"type":"assistant","apiBlockIndex":3,"message":{"id":"msg_1","model":"m","content":[{"type":"thinking","thinking":"x"},{"type":"tool_use","id":"a"},{"type":"tool_use","id":"b"}],` + usageObj + `}}`,
			kindUsage, "", []toolRef{{id: "a", abi: 3, idx: 1}, {id: "b", abi: 3, idx: 2}}},
		{"text block only", `{"type":"assistant","apiBlockIndex":0,"message":{"id":"msg_1","model":"m","content":[{"type":"text","text":"hi"}],` + usageObj + `}}`,
			kindUsage, "", nil},
		{"apiBlockIndex absent is -1", `{"type":"assistant","message":{"id":"msg_1","model":"m","content":[{"type":"tool_use","id":"a"}],` + usageObj + `}}`,
			kindUsage, "", []toolRef{{id: "a", abi: -1, idx: 0}}},
		{"api-request-blob replaying tool_use counts none", `{"type":"api-request-blob","timestamp":null,"message":{"content":[{"type":"tool_use","id":"a"},{"type":"tool_use","id":"b"}]}}`,
			kindIgnored, "", nil},
		{"user line, string content, timestamp", `{"type":"user","timestamp":"2026-10-24T07:38:02Z","message":{"role":"user","content":"hello"}}`,
			kindIgnored, "2026-10-24T07:38:02Z", nil},
		{"user line carrying a tool_use is not counted", `{"type":"user","message":{"id":"m","model":"m","content":[{"type":"tool_use","id":"a"}],` + usageObj + `}}`,
			kindUsage, "", nil},
		{"synthetic assistant with tool_use", `{"type":"assistant","timestamp":"2026-10-24T07:38:02Z","message":{"id":"m","model":"<synthetic>","content":[{"type":"tool_use","id":"a"}],` + usageObj + `}}`,
			kindSynthetic, "2026-10-24T07:38:02Z", nil},
		{"no root type counts no tool", `{"message":{"id":"m","model":"m","content":[{"type":"tool_use","id":"a"}],` + usageObj + `}}`,
			kindUsage, "", nil},
		{"content is an object", `{"type":"assistant","message":{"id":"m","model":"m","content":{"type":"tool_use","id":"a"},` + usageObj + `}}`,
			kindUsage, "", nil},
		{"content is a number", `{"type":"assistant","message":{"id":"m","model":"m","content":5,` + usageObj + `}}`,
			kindUsage, "", nil},
		{"content is null", `{"type":"assistant","message":{"id":"m","model":"m","content":null,` + usageObj + `}}`,
			kindUsage, "", nil},
		{"block type is a number", `{"type":"assistant","message":{"id":"m","model":"m","content":[{"type":7,"id":"a"}],` + usageObj + `}}`,
			kindUsage, "", nil},
		{"block id is a number: absent id", `{"type":"assistant","apiBlockIndex":2,"message":{"id":"m","model":"m","content":[{"type":"tool_use","id":9}],` + usageObj + `}}`,
			kindUsage, "", []toolRef{{id: "", abi: 2, idx: 0}}},
		{"malformed elements after a good block", `{"type":"assistant","apiBlockIndex":0,"message":{"id":"m","model":"m","content":[{"type":"tool_use","id":"a"},"str",5,null,[1]],` + usageObj + `}}`,
			kindUsage, "", []toolRef{{id: "a", abi: 0, idx: 0}}},
		{"timestamp not a date", `{"type":"assistant","timestamp":"yesterday","message":{"id":"m","model":"m",` + usageObj + `}}`, kindUsage, "", nil},
		{"timestamp a number", `{"type":"assistant","timestamp":123,"message":{"id":"m","model":"m",` + usageObj + `}}`, kindUsage, "", nil},
		{"timestamp null", `{"type":"assistant","timestamp":null,"message":{"id":"m","model":"m",` + usageObj + `}}`, kindUsage, "", nil},
		{"timestamp absent", `{"type":"assistant","message":{"id":"m","model":"m",` + usageObj + `}}`, kindUsage, "", nil},
		{"timestamp on a line without message", `{"type":"attachment","timestamp":"2026-10-24T07:38:02Z"}`, kindIgnored, "2026-10-24T07:38:02Z", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, kind := decodeLine([]byte(tt.line))
			if kind != tt.kind {
				t.Fatalf("kind = %d, want %d", kind, tt.kind)
			}
			var wantAt time.Time
			if tt.at != "" {
				wantAt = tsAt(t, tt.at)
			}
			if !rec.At.Equal(wantAt) || rec.At.IsZero() != wantAt.IsZero() {
				t.Errorf("At = %v, want %v", rec.At, wantAt)
			}
			if len(rec.Tools) != len(tt.tools) || (len(tt.tools) > 0 && !reflect.DeepEqual(rec.Tools, tt.tools)) {
				t.Errorf("Tools = %+v, want %+v", rec.Tools, tt.tools)
			}
		})
	}
}

// A malformed content array costs the tool count of that line, never its usage.
func TestDecodeLineBadContentKeepsUsage(t *testing.T) {
	rec, kind := decodeLine([]byte(`{"type":"assistant","message":{"id":"m","model":"m","content":{"x":1},"usage":{"input_tokens":4,"output_tokens":5}}}`))
	if kind != kindUsage || rec.Usage.Input != 4 || rec.Usage.Output != 5 {
		t.Fatalf("kind=%d usage=%+v", kind, rec.Usage)
	}
}

func TestDecodeLineTimestampIsNormalisedToUTC(t *testing.T) {
	rec, _ := decodeLine([]byte(`{"type":"user","timestamp":"2026-10-24T09:38:02+02:00"}`))
	if want := tsAt(t, "2026-10-24T07:38:02Z"); !rec.At.Equal(want) {
		t.Fatalf("At = %v, want %v", rec.At, want)
	}
	if rec.At.Location() != time.UTC {
		t.Errorf("Location = %v, want UTC", rec.At.Location())
	}
	// Two spellings of one instant must be indistinguishable to DeepEqual.
	z, _ := decodeLine([]byte(`{"type":"user","timestamp":"2026-10-24T07:38:02Z"}`))
	if !reflect.DeepEqual(rec.At, z.At) {
		t.Errorf("Z and +02:00 spellings differ under DeepEqual: %#v vs %#v", rec.At, z.At)
	}
}

// Privacy: of a content block only the type and the id are ever declared.
func TestWireBlockDeclaresOnlyTypeAndID(t *testing.T) {
	rt := reflect.TypeOf(wireBlock{})
	var got []string
	for i := range rt.NumField() {
		got = append(got, rt.Field(i).Name)
	}
	if !reflect.DeepEqual(got, []string{"Type", "ID"}) {
		t.Fatalf("wireBlock fields = %v, want [Type ID]", got)
	}
}
