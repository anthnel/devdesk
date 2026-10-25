package agentusage

import (
	"encoding/json"
	"strconv"
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
}

// syntheticModel is the model name of locally generated, never-billed turns.
const syntheticModel = "<synthetic>"

// wireLine mirrors the only keys read from a transcript line. Content,
// tools and descriptions are deliberately not declared, so encoding/json
// skips them without keeping them. The root "type" key is not consulted.
type wireLine struct {
	Version json.RawMessage `json:"version"`
	Message *wireMessage    `json:"message"`
}

type wireMessage struct {
	ID         string          `json:"id"`
	Model      json.RawMessage `json:"model"`
	StopReason json.RawMessage `json:"stop_reason"`
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
	if m == nil {
		return record{Version: sanitizeLabel(optString(w.Version))}, kindIgnored
	}
	rec := record{
		MessageID: m.ID,
		Model:     sanitizeLabel(optString(m.Model)),
		Version:   sanitizeLabel(optString(w.Version)),
		StopNull:  isNullOrAbsent(m.StopReason),
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
