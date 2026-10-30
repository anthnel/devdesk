package agentusage

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

// lineKind classifies one transcript line.
type lineKind int

const (
	kindUsage      lineKind = iota // message.usage present: counts
	kindNoUsage                    // message with an id, but no usage object
	kindIgnored                    // valid JSON without a message, or a message with neither id, model nor usage
	kindUnreadable                 // not JSON, truncated, or bad counters
	kindSynthetic                  // model "<synthetic>": never counts
)

// record is what decodeLine extracts from a line; only these fields are read.
type record struct {
	MessageID string
	Model     string
	Version   string
	Usage     Tokens
	StopNull  bool
	At        time.Time // root timestamp in UTC; zero if absent or not a date
	Tools     []toolRef // tool_use blocks of a non-synthetic assistant line
}

// toolRef is one tool_use block of an assistant line.
type toolRef struct {
	id  string // tool_use id; "" if absent or not a string
	abi int    // the line's apiBlockIndex; -1 when absent
	idx int    // position of the block in message.content
}

// wireBlock keeps a content block's type and id only: nothing else of a block
// (text, tool input, tool output) is ever declared, so none is kept.
type wireBlock struct{ Type, ID string }

// toolUseType is the content block type of a tool call.
const toolUseType = "tool_use"

// blockAt is a tool_use block with its position in message.content.
type blockAt struct {
	idx int
	wireBlock
}

// wireBlocks decodes message.content keeping the tool_use blocks only, as
// type and id: no text, tool input or tool output is retained. Content of
// another shape, and elements of another shape, yield no block and no error,
// so a malformed content costs the tool count of its line and nothing else.
type wireBlocks []blockAt

// UnmarshalJSON implements json.Unmarshaler.
func (bs *wireBlocks) UnmarshalJSON(data []byte) error {
	*bs = nil
	// Unmarshal into a slice rather than streaming through a json.Decoder: the
	// decoder copies every element, tool input included, which is several
	// times slower and allocates a multiple of the line's size.
	// encoding/json keeps an empty slot for an element of the wrong type and
	// reports an UnmarshalTypeError at the end, so positions are preserved
	// (a numeric id leaves Type set); any other error, a syntax error say,
	// ends the scan without keeping a partial result.
	var raw []wireBlock
	if err := json.Unmarshal(data, &raw); err != nil {
		var te *json.UnmarshalTypeError
		if !errors.As(err, &te) {
			return nil
		}
	}
	for i, b := range raw {
		if b.Type == toolUseType {
			*bs = append(*bs, blockAt{idx: i, wireBlock: b})
		}
	}
	return nil
}

// assistantType is the root type of the lines whose tool_use blocks count.
const assistantType = "assistant"

// syntheticModel is the model name of locally generated, never-billed turns.
const syntheticModel = "<synthetic>"

// wireLine mirrors the only keys read from a transcript line. Content,
// tools and descriptions are deliberately not declared, so encoding/json
// skips them without keeping them. Of the root, only the type, the timestamp,
// apiBlockIndex and the version are read, besides the message; of
// message.content, only the type and id of tool_use blocks (wireBlocks).
type wireLine struct {
	Type          json.RawMessage `json:"type"`
	Timestamp     json.RawMessage `json:"timestamp"`
	APIBlockIndex json.RawMessage `json:"apiBlockIndex"`
	Version       json.RawMessage `json:"version"`
	Message       *wireMessage    `json:"message"`
}

type wireMessage struct {
	ID         string          `json:"id"`
	Model      json.RawMessage `json:"model"`
	StopReason json.RawMessage `json:"stop_reason"`
	Content    wireBlocks      `json:"content"`
	Usage      *wireUsage      `json:"usage"`
}

// wireUsage keeps counters raw so a negative, fractional or non-numeric
// value can be told apart from an absent one.
type wireUsage struct {
	Input      json.RawMessage `json:"input_tokens"`
	Output     json.RawMessage `json:"output_tokens"`
	CacheRead  json.RawMessage `json:"cache_read_input_tokens"`
	CacheWrite json.RawMessage `json:"cache_creation_input_tokens"`
}

// decodeLine extracts the usage fields of one transcript line. A message
// without usage counts as kindNoUsage only if it has an id or a model (an
// assistant turn whose usage went missing or was renamed). It never
// reports a decoder error: those can quote line content, so a bad line is
// just kindUnreadable.
func decodeLine(b []byte) (record, lineKind) {
	var w wireLine
	if err := json.Unmarshal(b, &w); err != nil {
		return record{}, kindUnreadable
	}
	m := w.Message
	at := parseTimestamp(w.Timestamp)
	if m == nil {
		return record{Version: sanitizeLabel(optString(w.Version)), At: at}, kindIgnored
	}
	rec := record{
		MessageID: m.ID,
		Model:     sanitizeLabel(optString(m.Model)),
		Version:   sanitizeLabel(optString(w.Version)),
		StopNull:  isNullOrAbsent(m.StopReason),
		At:        at,
	}
	if rec.Model != syntheticModel && optString(w.Type) == assistantType {
		rec.Tools = toolRefs(m.Content, parseBlockIndex(w.APIBlockIndex))
	}
	switch {
	case rec.Model == syntheticModel:
		return rec, kindSynthetic
	case m.Usage == nil && m.ID == "" && rec.Model == "":
		// User and tool-result lines carry a message without id, usage or model.
		return rec, kindIgnored
	case m.Usage == nil:
		return rec, kindNoUsage
	}
	tokens, ok := m.Usage.tokens()
	if !ok {
		return record{}, kindUnreadable
	}
	rec.Usage = tokens
	return rec, kindUsage
}

// tokens converts the raw counters; ok is false if any is not a
// non-negative integer.
func (u *wireUsage) tokens() (Tokens, bool) {
	var t Tokens
	for _, c := range []struct {
		raw json.RawMessage
		dst *int64
	}{{u.Input, &t.Input}, {u.Output, &t.Output}, {u.CacheRead, &t.CacheRead}, {u.CacheWrite, &t.CacheWrite}} {
		n, ok := parseCounter(c.raw)
		if !ok {
			return Tokens{}, false
		}
		*c.dst = n
	}
	return t, true
}

// parseCounter reads one counter; absent or null is 0.
func parseCounter(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, true
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	return n, err == nil && n >= 0
}

// optString returns the JSON string held by raw, or "" if raw is absent or
// of another type: metadata of an unexpected type must not cost the line.
func optString(raw json.RawMessage) string {
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// isNullOrAbsent reports whether raw is a missing key or a JSON null. Any
// other value, whatever its type, counts as a stop reason being set.
func isNullOrAbsent(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}

// parseTimestamp reads a root timestamp as an instant in UTC; anything else
// (absent, null, not a string, not RFC 3339) is the zero time.
func parseTimestamp(raw json.RawMessage) time.Time {
	s := optString(raw)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// parseBlockIndex reads apiBlockIndex; -1 if absent or not a non-negative integer.
func parseBlockIndex(raw json.RawMessage) int {
	n, err := strconv.Atoi(string(raw))
	if err != nil || n < 0 {
		return -1
	}
	return n
}

// toolRefs lists the tool calls of a line from its decoded content blocks.
func toolRefs(blocks wireBlocks, abi int) []toolRef {
	var refs []toolRef
	for _, b := range blocks {
		refs = append(refs, toolRef{id: b.ID, abi: abi, idx: b.idx})
	}
	return refs
}
