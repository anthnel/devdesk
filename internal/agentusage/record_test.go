package agentusage

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const sentinel = "SENTINEL-DO-NOT-LEAK"

// Synthetic fixtures only: no real transcript content.
const fullLine = `{"type":"assistant","version":"2.1.302","uuid":"u1","message":{"id":"msg_1","model":"claude-x","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":300,"cache_creation_input_tokens":4000}}}`

func usageLine(usage string) string {
	return `{"version":"2.1.302","message":{"id":"msg_1","model":"m","stop_reason":"end_turn","usage":` + usage + `}}`
}

func TestDecodeLine(t *testing.T) {
	full := Tokens{Input: 10, Output: 20, CacheRead: 300, CacheWrite: 4000}
	tests := []struct {
		name    string
		line    string
		kind    lineKind
		wantRec *record // nil: record not checked
	}{
		{"full assistant line", fullLine, kindUsage,
			&record{MessageID: "msg_1", Model: "claude-x", Version: "2.1.302", Usage: full}},
		{"thinking_tokens present", usageLine(`{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":300,"cache_creation_input_tokens":4000,"output_tokens_details":{"thinking_tokens":5}}`), kindUsage,
			&record{MessageID: "msg_1", Model: "m", Version: "2.1.302", Usage: full}},
		{"extras null", usageLine(`{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":300,"cache_creation_input_tokens":4000,"output_tokens_details":null,"service_tier":null,"speed":null,"inference_geo":null,"iterations":null,"fallback_credit":null}`), kindUsage,
			&record{MessageID: "msg_1", Model: "m", Version: "2.1.302", Usage: full}},
		{"extras populated", usageLine(`{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":300,"cache_creation_input_tokens":4000,"service_tier":"standard","speed":"fast","inference_geo":"eu","iterations":[{"a":1}],"fallback_credit":{"x":1}}`), kindUsage,
			&record{MessageID: "msg_1", Model: "m", Version: "2.1.302", Usage: full}},
		{"unknown keys at every level", `{"zzz":1,"version":"2.1.302","message":{"id":"msg_1","model":"m","stop_reason":"x","novel":{"a":[1]},"usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4,"brand_new":true}}}`, kindUsage,
			&record{MessageID: "msg_1", Model: "m", Version: "2.1.302", Usage: Tokens{1, 2, 3, 4}}},
		{"root type is not consulted", `{"type":"user","message":{"id":"m","model":"m","stop_reason":"x","usage":{"input_tokens":1,"output_tokens":1}}}`, kindUsage, nil},
		{"usage missing counters default to 0", usageLine(`{"output_tokens":7}`), kindUsage,
			&record{MessageID: "msg_1", Model: "m", Version: "2.1.302", Usage: Tokens{Output: 7}}},
		{"usage renamed", `{"message":{"id":"msg_1","model":"m","token_usage":{"input_tokens":1}}}`, kindNoUsage, nil},
		{"usage absent", `{"message":{"id":"msg_1","model":"m","stop_reason":"end_turn"}}`, kindNoUsage, nil},
		{"unknown type without message", `{"type":"queue-operation","operation":"enqueue"}`, kindIgnored, nil},
		{"empty object", `{}`, kindIgnored, nil},
		{"not json", `hello world`, kindUnreadable, nil},
		{"truncated json", fullLine[:len(fullLine)/2], kindUnreadable, nil},
		{"empty line", ``, kindUnreadable, nil},
		{"synthetic model", `{"message":{"id":"m","model":"<synthetic>","stop_reason":"stop_sequence","usage":{"input_tokens":0,"output_tokens":0}}}`, kindSynthetic, nil},
		// Policy: a malformed counter makes the whole line unreadable
		// (never a silent 0, never a negative total).
		{"negative counter", usageLine(`{"input_tokens":-5,"output_tokens":1}`), kindUnreadable, nil},
		{"non numeric counter", usageLine(`{"input_tokens":"abc","output_tokens":1}`), kindUnreadable, nil},
		{"fractional counter", usageLine(`{"input_tokens":1.5,"output_tokens":1}`), kindUnreadable, nil},
		{"missing id with usage", `{"message":{"model":"m","stop_reason":"x","usage":{"input_tokens":1,"output_tokens":2}}}`, kindUsage,
			&record{MessageID: "", Model: "m", Usage: Tokens{Input: 1, Output: 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, kind := decodeLine([]byte(tt.line))
			if kind != tt.kind {
				t.Fatalf("kind = %d, want %d", kind, tt.kind)
			}
			if tt.wantRec != nil {
				want := *tt.wantRec
				want.StopNull = rec.StopNull // checked separately
				if !reflect.DeepEqual(rec, want) {
					t.Fatalf("record = %+v, want %+v", rec, want)
				}
			}
		})
	}
}

func TestDecodeLineStopNull(t *testing.T) {
	tests := []struct {
		name, msg string
		want      bool
	}{
		{"null", `"stop_reason":null,`, true},
		{"absent", ``, true},
		{"end_turn", `"stop_reason":"end_turn",`, false},
		{"tool_use", `"stop_reason":"tool_use",`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := `{"message":{"id":"m","model":"m",` + tt.msg + `"usage":{"input_tokens":1,"output_tokens":1}}}`
			rec, kind := decodeLine([]byte(line))
			if kind != kindUsage {
				t.Fatalf("kind = %d, want usage", kind)
			}
			if rec.StopNull != tt.want {
				t.Fatalf("StopNull = %v, want %v", rec.StopNull, tt.want)
			}
		})
	}
}

func TestDecodeLineNeverLeaksContent(t *testing.T) {
	line := `{"type":"assistant","version":"2.1.302","cwd":"` + sentinel + `","summary":"` + sentinel + `",` +
		`"message":{"id":"msg_1","model":"m","stop_reason":"end_turn","description":"` + sentinel + `",` +
		`"content":[{"type":"text","text":"` + sentinel + `"},{"type":"tool_use","name":"` + sentinel + `","input":{"command":"` + sentinel + `"}}],` +
		`"usage":{"input_tokens":1,"output_tokens":2,"service_tier":"` + sentinel + `"}}}`
	rec, kind := decodeLine([]byte(line))
	if kind != kindUsage {
		t.Fatalf("kind = %d, want usage", kind)
	}
	if got := strings.Join([]string{rec.MessageID, rec.Model, rec.Version}, "|"); strings.Contains(got, sentinel) {
		t.Fatalf("sentinel leaked into record: %+v", rec)
	}
	if rec.Usage != (Tokens{Input: 1, Output: 2}) {
		t.Fatalf("usage = %+v", rec.Usage)
	}
}

func FuzzDecodeLine(f *testing.F) {
	for _, s := range []string{
		fullLine,
		usageLine(`{"input_tokens":1,"output_tokens":2,"output_tokens_details":null,"service_tier":null}`),
		usageLine(`{"input_tokens":-5}`),
		usageLine(`{"input_tokens":"abc"}`),
		`{"message":{"id":"m","model":"m","token_usage":{}}}`,
		`{"type":"queue-operation"}`,
		`{"message":{"id":"m","model":"<synthetic>","usage":{}}}`,
		`{"message":{"model":"m","stop_reason":null,"usage":{"input_tokens":1}}}`,
		`{"cwd":"` + sentinel + `","message":{"id":"m","content":[{"text":"` + sentinel + `"}],"usage":{"input_tokens":1}}}`,
		`not json`, `{"message":`, ``,
		// Regression: the same key twice; encoding/json merges the objects.
		`{"message":{"model":"a"},"message":{"id":"x"}}`,
		// Regression: encoding/json matches "messAge" to "message".
		`{"messAge":{"model":"0","usage":{"input_tokens":1}}}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		rec, kind := decodeLine(b)
		if kind < kindUsage || kind > kindSynthetic {
			t.Fatalf("invalid kind %d", kind)
		}
		if rec.Usage.Input < 0 || rec.Usage.Output < 0 || rec.Usage.CacheRead < 0 || rec.Usage.CacheWrite < 0 {
			t.Fatalf("negative counter: %+v", rec.Usage)
		}
		// A decoded string may only come from a string the input holds under
		// the matching key; the reference is a plain map decoding.
		var ref map[string]any
		if json.Unmarshal(b, &ref) != nil {
			if kind != kindUnreadable {
				t.Fatalf("kind %d for input a reference decoder rejects", kind)
			}
			return
		}
		// encoding/json matches keys case-insensitively, so "messAge" is
		// "message"; and values under several such keys (even byte-identical
		// ones) are merged, so every occurrence is a legitimate source. The
		// reference therefore walks the raw tokens instead of a map, which
		// would keep only the last duplicate.
		msgs := refValues(b, "message")
		var ids, models []string
		for _, m := range msgs {
			ids = append(ids, refStrings(m, "id")...)
			models = append(models, refStrings(m, "model")...)
		}
		for _, c := range []struct {
			got  string
			srcs []string
			key  string
		}{{rec.MessageID, ids, "id"}, {rec.Model, models, "model"}, {rec.Version, refStrings(b, "version"), "version"}} {
			if c.got == "" {
				continue
			}
			found := false
			for _, s := range c.srcs {
				found = found || s == c.got
			}
			if !found {
				t.Fatalf("record field %q not found under %q in reference decoding", c.got, c.key)
			}
		}
	})
}

// refValues returns the raw value of every top-level key of the object b that
// matches key case-insensitively, duplicates included.
func refValues(b []byte, key string) [][]byte {
	dec := json.NewDecoder(bytes.NewReader(b))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil
	}
	var out [][]byte
	for dec.More() {
		k, err := dec.Token()
		ks, isStr := k.(string)
		if err != nil || !isStr {
			return out
		}
		var raw json.RawMessage
		if dec.Decode(&raw) != nil {
			return out
		}
		if strings.EqualFold(ks, key) {
			out = append(out, raw)
		}
	}
	return out
}

// refStrings returns the string values among refValues(b, key).
func refStrings(b []byte, key string) []string {
	var out []string
	for _, raw := range refValues(b, key) {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			out = append(out, s)
		}
	}
	return out
}

// Behavior change: an unexpected type for version, model or stop_reason must
// not make the line unreadable; its counters still count.
func TestDecodeLineUnexpectedMetadataTypes(t *testing.T) {
	const u = `"usage":{"input_tokens":1,"output_tokens":2}`
	tests := []struct {
		name, line string
		want       record
	}{
		{"version number", `{"version":2,"message":{"id":"m","model":"x","stop_reason":"e",` + u + `}}`,
			record{MessageID: "m", Model: "x", Version: "", Usage: Tokens{Input: 1, Output: 2}}},
		{"version object", `{"version":{"a":1},"message":{"id":"m","model":"x","stop_reason":"e",` + u + `}}`,
			record{MessageID: "m", Model: "x", Version: "", Usage: Tokens{Input: 1, Output: 2}}},
		{"model number", `{"version":"v","message":{"id":"m","model":7,"stop_reason":"e",` + u + `}}`,
			record{MessageID: "m", Model: "", Version: "v", Usage: Tokens{Input: 1, Output: 2}}},
		{"model object", `{"version":"v","message":{"id":"m","model":{"n":1},"stop_reason":"e",` + u + `}}`,
			record{MessageID: "m", Model: "", Version: "v", Usage: Tokens{Input: 1, Output: 2}}},
		{"stop_reason number", `{"version":"v","message":{"id":"m","model":"x","stop_reason":3,` + u + `}}`,
			record{MessageID: "m", Model: "x", Version: "v", Usage: Tokens{Input: 1, Output: 2}, StopNull: false}},
		{"stop_reason object", `{"version":"v","message":{"id":"m","model":"x","stop_reason":{"a":1},` + u + `}}`,
			record{MessageID: "m", Model: "x", Version: "v", Usage: Tokens{Input: 1, Output: 2}, StopNull: false}},
		{"stop_reason array", `{"version":"v","message":{"id":"m","model":"x","stop_reason":[],` + u + `}}`,
			record{MessageID: "m", Model: "x", Version: "v", Usage: Tokens{Input: 1, Output: 2}, StopNull: false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, kind := decodeLine([]byte(tt.line))
			if kind != kindUsage {
				t.Fatalf("kind = %d, want usage", kind)
			}
			if !reflect.DeepEqual(rec, tt.want) {
				t.Fatalf("record = %+v, want %+v", rec, tt.want)
			}
		})
	}
}

// Unchanged policy: a wrong-typed id or a malformed counter is unreadable.
func TestDecodeLineStillUnreadable(t *testing.T) {
	tests := []struct{ name, line string }{
		{"id number", `{"message":{"id":5,"model":"m","usage":{"input_tokens":1}}}`},
		{"id object", `{"message":{"id":{"a":1},"model":"m","usage":{"input_tokens":1}}}`},
		{"counter object", usageLine(`{"input_tokens":{"a":1}}`)},
		{"counter bool", usageLine(`{"input_tokens":true}`)},
		{"int64 overflow", usageLine(`{"input_tokens":9223372036854775808}`)},
		{"exponent", usageLine(`{"input_tokens":1e3}`)},
		{"quoted number", usageLine(`{"input_tokens":"10"}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, kind := decodeLine([]byte(tt.line)); kind != kindUnreadable {
				t.Fatalf("kind = %d, want unreadable", kind)
			}
		})
	}
}

func TestDecodeLineNullsAndWhitespace(t *testing.T) {
	if _, kind := decodeLine([]byte(`{"message":{"id":"m","model":"m","usage":null}}`)); kind != kindNoUsage {
		t.Fatalf("usage:null kind = %d, want noUsage", kind)
	}
	if _, kind := decodeLine([]byte(`{"message":null}`)); kind != kindIgnored {
		t.Fatalf("message:null kind = %d, want ignored", kind)
	}
	rec, kind := decodeLine([]byte(usageLine(`{"input_tokens": 12 ,"output_tokens":	3
}`)))
	if kind != kindUsage || rec.Usage != (Tokens{Input: 12, Output: 3}) {
		t.Fatalf("whitespace around numbers: kind %d, usage %+v", kind, rec.Usage)
	}
}

// Documents current encoding/json behavior: keys match case-insensitively
// and, on duplicates, the last occurrence wins. Not a contract we chose.
func TestDecodeLineDuplicateAndCaseKeys(t *testing.T) {
	rec, kind := decodeLine([]byte(`{"message":{"id":"first","id":"last","model":"m","usage":{"input_tokens":1,"input_tokens":9}}}`))
	if kind != kindUsage || rec.MessageID != "last" || rec.Usage.Input != 9 {
		t.Fatalf("duplicate keys: kind %d, rec %+v", kind, rec)
	}
	rec, kind = decodeLine([]byte(`{"Message":{"ID":"a","Model":"m","Usage":{"Input_Tokens":4}}}`))
	if kind != kindUsage || rec.MessageID != "a" || rec.Usage.Input != 4 {
		t.Fatalf("case-insensitive keys: kind %d, rec %+v", kind, rec)
	}
	rec, _ = decodeLine([]byte(`{"message":{"id":"lower","ID":"upper","model":"m","usage":{}}}`))
	if rec.MessageID != "upper" {
		t.Fatalf("id = %q, want upper (last, case-insensitive)", rec.MessageID)
	}
}

// The aggregator collects versions from every decodable line, so decodeLine
// must report the version for lines that carry no usage, are ignored, or are
// synthetic, not only for counted ones.
func TestDecodeLineReportsVersionOnEveryDecodableKind(t *testing.T) {
	tests := []struct {
		name, line string
		kind       lineKind
	}{
		{"usage", `{"version":"7.7.7","message":{"id":"m","model":"x","usage":{"input_tokens":1}}}`, kindUsage},
		{"no usage", `{"version":"7.7.7","message":{"id":"m","model":"x"}}`, kindNoUsage},
		{"synthetic", `{"version":"7.7.7","message":{"id":"m","model":"<synthetic>","usage":{"input_tokens":1}}}`, kindSynthetic},
		{"ignored", `{"version":"7.7.7","type":"queue-operation"}`, kindIgnored},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, kind := decodeLine([]byte(tt.line))
			if kind != tt.kind {
				t.Fatalf("kind = %d, want %d", kind, tt.kind)
			}
			if rec.Version != "7.7.7" {
				t.Fatalf("Version = %q, want 7.7.7", rec.Version)
			}
		})
	}
}
