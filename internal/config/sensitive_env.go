package config

import (
	"fmt"

	"github.com/anthnel/devdesk/internal/secretenv"
)

// ContainersConfig holds the settings of the container views.
type ContainersConfig struct {
	// SensitiveEnv adds to the built-in patterns that mask environment values
	// (secretenv.DefaultPatterns). Each entry is a Go regular expression,
	// case-insensitive, searched in the variable name. It never removes a
	// default. No default value, no migration: an empty list is not written.
	SensitiveEnv []string `yaml:"sensitive_env,omitempty"`
}

// SensitiveEnvMatcher builds the matcher for the configured list. A bad entry
// does not fail the load: the error names the field, and the returned matcher
// is still usable (defaults and valid entries), so masking never drops.
func (c ContainersConfig) SensitiveEnvMatcher() (*secretenv.Matcher, error) {
	m, err := secretenv.New(c.SensitiveEnv)
	if err != nil {
		return m, fmt.Errorf("containers.sensitive_env: %w", err)
	}
	return m, nil
}
