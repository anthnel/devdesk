package config

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// examplePricesMarker is the first line of ExamplePricesBlock. Files on disk
// are recognized by it, so it must never change.
const examplePricesMarker = "# Optional: price table for the :agent-usage cost estimate."

// ExamplePricesBlock is the commented agent_usage.prices example appended to a
// context file while it holds no price. Every line is a comment.
const ExamplePricesBlock = examplePricesMarker + `
# Uncomment, then replace the model name and the numbers with your own.
# Rates are in USD per million tokens; take the current ones from your
# provider's price page. The numbers below only show the shape, they are not
# real prices.
#
# agent_usage:
#   prices:
#     claude-example-model:
#       input: 3
#       output: 15
#       cache_write_5m: 3.75
#       cache_write_1h: 6
#       cache_read: 0.3
`

// carriesExampleBlock reports whether data holds a whole line equal to the
// example marker. Trailing spaces, tabs and \r are ignored; leading ones are not.
func carriesExampleBlock(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimRight(line, " \t\r") == examplePricesMarker {
			return true
		}
	}
	return false
}

// marshalContext encodes cfg as YAML. With withExample, the commented price
// example is appended, but only when cfg holds no price entry.
func marshalContext(cfg *Config, withExample bool) ([]byte, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if withExample && len(cfg.AgentUsage.Prices) == 0 {
		data = append(data, "\n"+ExamplePricesBlock...)
	}
	return data, nil
}
