package config

import (
	"errors"
	"fmt"
	"os"
)

// newContextConfig returns the configuration of a freshly created context:
// the defaults, with no monitors and no forge URL.
func newContextConfig() *Config {
	cfg := Default()
	cfg.Status.Components = []ComponentConfig{}
	cfg.Forge.URL = ""
	return cfg
}

// createContextFile creates path exclusively (it fails with os.ErrExist when
// the file is there) and writes cfg with the commented price example. A file
// whose write failed is removed so no truncated config is left behind.
func createContextFile(path string, cfg *Config) error {
	data, err := marshalContext(cfg, true)
	if err != nil {
		return err
	}
	if err := EnsureConfigDir(); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// CreateContext creates a new context with a default configuration
// Empty monitors, empty GitLab URL, other values default. The file carries the
// commented price example and is never overwritten.
func CreateContext(contextName string) error {
	if err := ValidateNewContextName(contextName); err != nil {
		return err
	}
	path, err := GetContextPath(contextName)
	if err != nil {
		return err
	}
	err = createContextFile(path, newContextConfig())
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("context '%s' already exists: %w", contextName, ErrContextExists)
	}
	return err
}

// WriteNewContext writes cfg as the context contextName, with the commented
// price example, replacing any file already there.
func WriteNewContext(cfg *Config, contextName string) error {
	if err := ValidateContextName(contextName); err != nil {
		return err
	}
	path, err := GetContextPath(contextName)
	if err != nil {
		return err
	}
	data, err := marshalContext(cfg, true)
	if err != nil {
		return err
	}
	if err := EnsureConfigDir(); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// CreateDefaultContextIfMissing creates config.yaml when the current context is
// "default" and the file is absent, and reports whether it did. An existing
// config.yaml, even an invalid one, is neither read nor rewritten. The
// creation is exclusive, so of several concurrent callers only one gets true.
func CreateDefaultContextIfMissing() (bool, error) {
	if CurrentContextName() != "default" {
		return false, nil
	}
	path, err := GetContextPath("default")
	if err != nil {
		return false, err
	}
	err = createContextFile(path, newContextConfig())
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrExist):
		return false, nil
	default:
		return false, err
	}
}
