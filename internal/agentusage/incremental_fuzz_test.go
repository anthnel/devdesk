package agentusage

import (
	"bytes"
	"strings"
	"testing"
)

// fuzzMaxInput keeps one fuzz execution fast. A parent longer than
// MaxLineBytes is not skipped, whether it is a seed or a mutated input (the
// skip only applies between fuzzMaxInput and MaxLineBytes), so the over-long
// line path stays reachable while mutation explores around it.
const fuzzMaxInput = 64 << 10

// FuzzIncrementalMatchesFull: the input is cut into chunks appended one by
// one to a parent and a sub-agent transcript, with one ReadDelta+Apply after
// each; the final aggregate must equal ParseSession on the final files, and
// nothing may panic.
func FuzzIncrementalMatchesFull(f *testing.F) {
	ok := string(msgLine("m1", "claude-f", "end_turn", 1, 2, 3, 4)) + "\n"
	ok2 := string(msgLine("m1", "claude-f", "tool_use", 5, 6, 7, 8)) + "\n"
	f.Add([]byte(ok+ok2), []byte(ok), uint8(7))
	f.Add([]byte(strings.ReplaceAll(ok+ok2, "\n", "\r\n")), []byte(ok), uint8(1))
	f.Add([]byte(ok[:len(ok)/2]), []byte(ok+ok[:10]), uint8(3))
	f.Add([]byte(`{"message":{"content":"héllo`+"\xe2\x82"), []byte(ok), uint8(2)) // UTF-8 cut mid-rune
	f.Add([]byte(ok+"not json {{{\n\n"+ok2), []byte("[]\n\"x\"\n123\n"), uint8(5))
	f.Add([]byte(ok), []byte(ok2), uint8(0))
	// Timestamped lines with tool_use blocks: streamed repeats, a replaying
	// api-request-blob, a tool result, out-of-order and non-UTC timestamps.
	act := cat(nl(toolLine("m1", 0, "toolu_1", at(10, 5, 0))), nl(toolLine("m1", 0, "toolu_1", at(10, 1, 0))),
		nl(toolLine("m1", 1, "toolu_SENTINEL-DO-NOT-LEAK_1", "2026-10-24T12:04:00+02:00")),
		nl(blobLine("toolu_1")), nl(userLine(at(10, 9, 0))), nl(toolLine("", 0, "", "")))
	f.Add(act, cat(nl(toolLine("s1", 2, "toolu_2", at(10, 3, 0))), nl(blobLine("toolu_2", "toolu_2"))), uint8(0))
	f.Add(act, act, uint8(11))
	f.Add(act[:len(act)/2], act[len(act)/3:], uint8(5))
	f.Add(bytes.Repeat([]byte("a"), MaxLineBytes+10), []byte(ok), uint8(200)) // over-long line

	f.Fuzz(func(t *testing.T, parent, sub []byte, step uint8) {
		if len(parent) > fuzzMaxInput && len(parent) <= MaxLineBytes {
			t.Skip("input too large")
		}
		if len(sub) > fuzzMaxInput {
			t.Skip("input too large")
		}
		s := newSess(t)
		size := int(step)%64 + 1
		if step == 0 {
			size = 0 // line by line
		}
		s.put(s.parent(), nil) // the parent transcript exists before the first read
		a := NewAggregate(s.parent())
		srcs := []*src{{path: s.parent(), data: parent}, {path: s.sub("f1"), data: sub}}
		offs := make([]int, len(srcs))
		readApply(t, a)
		for pending := true; pending; {
			pending = false
			for i, sr := range srcs {
				if offs[i] >= len(sr.data) {
					continue
				}
				pending = true
				end := nextChunk(sr.data, offs[i], size)
				if size > 0 && len(sr.data) > 4096 {
					end = nextChunk(sr.data, offs[i], max(size, len(sr.data)/16))
				}
				s.add(sr.path, sr.data[offs[i]:end])
				offs[i] = end
				readApply(t, a)
			}
		}
		readApply(t, a)
		assertMatchesParse(t, a, s)
	})
}
