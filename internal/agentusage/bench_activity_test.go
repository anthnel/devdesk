package agentusage

import (
	"encoding/json"
	"strings"
	"testing"
)

// The content array is now walked for each block's type and id. Guard: a
// line with a 1 MiB tool input (the real worst case is 1.3 MB) must not cost
// much more than the same line decoded the way main did, with no content.

// baselineLine is the shape main decoded: version and message usage only.
type baselineLine struct {
	Version json.RawMessage `json:"version"`
	Message *struct {
		ID         string          `json:"id"`
		Model      json.RawMessage `json:"model"`
		StopReason json.RawMessage `json:"stop_reason"`
		Usage      *wireUsage      `json:"usage"`
	} `json:"message"`
}

func largeToolInputLine(size int) []byte {
	pad := strings.Repeat("x", size)
	return []byte(`{"type":"assistant","version":"2.1.302","apiBlockIndex":0,"timestamp":"2026-10-24T10:00:00.000Z",` +
		`"message":{"id":"m1","model":"model-a","stop_reason":"tool_use","content":[{"type":"text","text":"hi"},` +
		`{"type":"tool_use","id":"toolu_1","name":"Write","input":{"content":"` + pad + `"}}],` +
		`"usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}}`)
}

// BenchmarkDecodeLineLargeToolInput compares decodeLine with the decoding of
// main on one assistant line holding a 1 MiB tool input. Flag in the PR if
// "decodeLine" is more than twice "baseline".
func BenchmarkDecodeLineLargeToolInput(b *testing.B) {
	line := largeToolInputLine(1 << 20)
	b.Run("decodeLine", func(b *testing.B) {
		b.SetBytes(int64(len(line)))
		b.ReportAllocs()
		for range b.N {
			if rec, kind := decodeLine(line); kind != kindUsage || len(rec.Tools) != 1 {
				b.Fatalf("kind=%d tools=%v", kind, rec.Tools)
			}
		}
	})
	b.Run("baseline", func(b *testing.B) {
		b.SetBytes(int64(len(line)))
		b.ReportAllocs()
		for range b.N {
			var w baselineLine
			if err := json.Unmarshal(line, &w); err != nil || w.Message == nil {
				b.Fatal(err)
			}
		}
	})
}
