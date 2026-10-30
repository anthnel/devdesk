package config

import "gopkg.in/yaml.v3"

// AgentUsageConfig holds the settings of the :agent-usage view. It has no
// default and no migration: an empty section is never written to the file.
type AgentUsageConfig struct {
	// Prices maps a model name, kept exactly as written, to its price per
	// million tokens. Validation (negative, NaN, Inf) belongs to the pricing
	// package, not to the loader.
	Prices map[string]ModelPrice `yaml:"prices,omitempty"`
}

// ModelPrice is the price of one model, per rate. Each rate is optional.
type ModelPrice struct {
	Input        Rate `yaml:"input,omitempty"`
	Output       Rate `yaml:"output,omitempty"`
	CacheWrite5m Rate `yaml:"cache_write_5m,omitempty"`
	CacheWrite1h Rate `yaml:"cache_write_1h,omitempty"`
	CacheRead    Rate `yaml:"cache_read,omitempty"`
}

// RateState tells whether a Rate was absent, unreadable, or a number.
type RateState int

const (
	// RateMissing means the key was absent (or null) in the file.
	RateMissing RateState = iota
	// RateInvalid means the value was not a number (text, list, map, bool).
	RateInvalid
	// RateSet means the value decoded as a number, whatever the number is.
	RateSet
)

// Rate is a tolerant price: a badly typed value costs its own entry, never
// the load of the whole context. The zero value is RateMissing.
type Rate struct {
	state RateState
	value float64
	raw   string     // text of an invalid scalar, written back unchanged
	node  *yaml.Node // copy of an invalid non-scalar (list, map, alias), written back unchanged
}

// Value returns the number and the state; the number is meaningful only for
// RateSet.
func (r Rate) Value() (float64, RateState) { return r.value, r.state }

// IsZero reports a missing rate so that omitempty skips it.
func (r Rate) IsZero() bool { return r.state == RateMissing }

// UnmarshalYAML never returns an error: anything that is not an int or float
// scalar becomes RateInvalid.
func (r *Rate) UnmarshalYAML(node *yaml.Node) error {
	*r = Rate{state: RateInvalid}
	if node.Kind != yaml.ScalarNode {
		cp := *node
		r.node = &cp
		return nil
	}
	r.raw = node.Value
	if tag := node.ShortTag(); tag != "!!int" && tag != "!!float" {
		return nil
	}
	var f float64
	if err := node.Decode(&f); err != nil {
		return nil
	}
	*r = Rate{state: RateSet, value: f}
	return nil
}

// MarshalYAML writes the number back, or an invalid value as it was read: the
// original node for a list, map or alias, the raw text for a scalar.
func (r Rate) MarshalYAML() (any, error) {
	if r.state == RateSet {
		return r.value, nil
	}
	if r.node != nil {
		return r.node, nil
	}
	return r.raw, nil
}
