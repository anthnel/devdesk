package config

import (
	"bytes"
	"os"

	"gopkg.in/yaml.v3"
)

// yamlIndent matches what yaml.Marshal produces, so a file rewritten by
// RemoveLegacySecrets keeps the shape SaveContext would have given it.
const yamlIndent = 4

// LegacySecrets are the plaintext secrets an earlier DevDesk wrote into a
// context file: `gitlab.token` and `registry.password`. Both fields are gone
// from the schema (§3.9), which means unmarshalling drops them silently — so
// they are read straight from the file instead, moved into the host secret
// store, and then removed.
//
// The URLs travel with them because that is the key a secret is filed under.
type LegacySecrets struct {
	GitLabURL        string
	GitLabToken      string
	RegistryURL      string
	RegistryPassword string
}

// Empty reports whether the file held no plaintext secret at all — the normal
// case, and the one where nothing needs migrating.
func (l LegacySecrets) Empty() bool {
	return l.GitLabToken == "" && l.RegistryPassword == ""
}

// ReadLegacySecrets pulls the two plaintext fields out of a context file.
//
// A missing file is not an error: there is simply nothing to migrate.
func ReadLegacySecrets(contextName string) (LegacySecrets, error) {
	root, err := readContextTree(contextName)
	if err != nil || root == nil {
		return LegacySecrets{}, err
	}

	gitlab := mappingValue(root, "gitlab")
	registry := mappingValue(root, "registry")

	return LegacySecrets{
		GitLabURL:        scalarValue(gitlab, "url"),
		GitLabToken:      scalarValue(gitlab, "token"),
		RegistryURL:      scalarValue(registry, "url"),
		RegistryPassword: scalarValue(registry, "password"),
	}, nil
}

// RemoveLegacySecrets rewrites a context file without the secrets named by the
// two flags. The caller passes true only for a secret it has already stored
// somewhere safe, so a store that refused a write leaves the file alone rather
// than losing the secret altogether.
//
// It edits the parsed YAML tree rather than round-tripping through Config,
// which would rewrite every key the user has — including the defaults
// applyDefaults filled in — and drop their comments. Deleting two keys should
// not reformat somebody's configuration file.
func RemoveLegacySecrets(contextName string, gitlabToken, registryPassword bool) error {
	root, err := readContextTree(contextName)
	if err != nil || root == nil {
		return err
	}

	removed := false
	if gitlabToken {
		removed = deleteKey(mappingValue(root, "gitlab"), "token")
	}
	if registryPassword {
		removed = deleteKey(mappingValue(root, "registry"), "password") || removed
	}
	if !removed {
		return nil
	}

	configPath, err := GetContextPath(contextName)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(root); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}

	return os.WriteFile(configPath, buf.Bytes(), 0600)
}

// readContextTree parses a context file into its YAML tree. It returns a nil
// node — and no error — when the file does not exist or holds no mapping.
func readContextTree(contextName string) (*yaml.Node, error) {
	if err := ValidateContextName(contextName); err != nil {
		return nil, err
	}

	configPath, err := GetContextPath(contextName)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil
	}
	return doc.Content[0], nil
}

// mappingValue returns the value node a key holds in a mapping, or nil. A
// mapping node stores its pairs flat: key, value, key, value.
func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// scalarValue returns a string-valued key from a mapping, or "".
func scalarValue(mapping *yaml.Node, key string) string {
	value := mappingValue(mapping, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return value.Value
}

// deleteKey drops a key and its value from a mapping, reporting whether it was
// there.
func deleteKey(mapping *yaml.Node, key string) bool {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return true
		}
	}
	return false
}
