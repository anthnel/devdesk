package enginefixture

import (
	"fmt"
	"strings"
)

// Supported lists, per tool, the engine versions DevDesk supports: N first, then
// N-1.
type Supported map[Tool][]string

// versionKeys are the keys of tools/engine-vms/versions.env that ParseVersions
// reads, per tool, N then N-1. kind and kubectl join with OC-59 and OC-68.
var versionKeys = []struct {
	tool Tool
	keys [2]string
}{
	{Docker, [2]string{"DOCKER_N_VERSION", "DOCKER_N1_VERSION"}},
	{Podman, [2]string{"PODMAN_N_VERSION", "PODMAN_N1_VERSION"}},
}

// ParseVersions reads the supported engine versions from the text of
// tools/engine-vms/versions.env, the one place that owns them: plain NAME=value
// lines (the file is sourced by bash), "#" comments, also at the end of a line.
// The other keys are ignored. A missing key, an empty value or a value that is not
// a dotted version number, or a blank around the "=" (which bash does not read as an
// assignment), is an error that names the key. As in bash, the last assignment of a
// key wins.
func ParseVersions(data []byte) (Supported, error) {
	values := map[string]string{}
	spaced := map[string]bool{} // the last assignment of the key has a blank around the "="
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rawKey, rawValue, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value, _, _ := strings.Cut(rawValue, "#")
		key := strings.TrimSpace(rawKey)
		values[key] = strings.TrimSpace(value)
		// bash reads "KEY = v" as a command and "KEY= v" as an empty KEY then a command.
		spaced[key] = key != rawKey || (strings.TrimSpace(value) != "" && value != strings.TrimLeft(value, " \t"))
	}
	supported := Supported{}
	for _, t := range versionKeys {
		for _, key := range t.keys {
			value, ok := values[key]
			switch {
			case !ok:
				return nil, fmt.Errorf("enginefixture: versions.env has no %s", key)
			case spaced[key]:
				return nil, fmt.Errorf("enginefixture: versions.env: %s has a blank around the \"=\", which bash does not read as an assignment", key)
			case value == "":
				return nil, fmt.Errorf("enginefixture: versions.env: %s is empty", key)
			case !versionPattern.MatchString(value):
				return nil, fmt.Errorf("enginefixture: versions.env: %s is not a dotted version number", key)
			}
			supported[t.tool] = append(supported[t.tool], value)
		}
	}
	return supported, nil
}
