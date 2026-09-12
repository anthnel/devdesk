package docker

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/anthnel/devdesk/internal/engine"
)

// dockerHubKeys lists all keys Docker uses for Docker Hub in ~/.docker/config.json.
// `docker login docker.io` stores credentials under "https://index.docker.io/v1/".
var dockerHubKeys = []string{
	"docker.io",
	"registry-1.docker.io",
	"https://index.docker.io/v1/",
	"https://index.docker.io/v1",
	"https://registry-1.docker.io",
}

// registryCandidates returns all URL variants to look up in the engine's auth
// file.
func registryCandidates(registryURL string) []string {
	base := strings.TrimSuffix(registryURL, "/")

	// Check if this URL is any known Docker Hub alias; if so, return the full set.
	for _, alias := range dockerHubKeys {
		if strings.EqualFold(base, strings.TrimSuffix(alias, "/")) {
			return dockerHubKeys
		}
	}

	// Generic: bare hostname, https://, http:// variants.
	candidates := []string{base}
	if strings.HasPrefix(base, "https://") {
		candidates = append(candidates, strings.TrimPrefix(base, "https://"))
	} else if strings.HasPrefix(base, "http://") {
		candidates = append(candidates, strings.TrimPrefix(base, "http://"))
	} else {
		candidates = append(candidates, "https://"+base, "http://"+base)
	}
	return candidates
}

// authPath returns the engine's registry credential file — ~/.docker/config.json
// under docker, ${XDG_RUNTIME_DIR}/containers/auth.json or
// ~/.config/containers/auth.json under podman.
//
// The JSON is the same shape on both sides (`auths`, `credHelpers`,
// `credsStore`); only the location moves, which is why every reader and writer
// below is unchanged apart from where it points. It is read *and written*
// directly rather than through the CLI because `logout` leaves alias keys
// behind — that is what made the path a friction rather than a detail (§3.67).
func authPath() (string, error) {
	path := engine.Current().AuthPath()
	if path == "" {
		return "", errors.New("cannot locate the container engine's credential file")
	}
	return path, nil
}

// readAuthFile reads and unmarshals the engine's credential file into v.
func readAuthFile(v any) error {
	path, err := authPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// RegistryLogin authenticates with a Docker/OCI registry using `docker login`.
// The password is passed via stdin to avoid exposing it in the process list.
func RegistryLogin(registryURL, username, password string) error {
	if err := requireEngine(); err != nil {
		return err
	}
	output, err := runner.Run(dockerCmd{
		Args:     []string{"login", registryURL, "-u", username, "--password-stdin"},
		Stdin:    password,
		Combined: true,
	})
	if err != nil {
		return errWithOutput(cmdLabel("login"), output)
	}
	return nil
}

// IsRegistryLoggedIn reports whether stored credentials exist for registryURL
// by inspecting ~/.docker/config.json.
func IsRegistryLoggedIn(registryURL string) bool {
	var cfg struct {
		Auths map[string]json.RawMessage `json:"auths"`
	}
	if err := readAuthFile(&cfg); err != nil {
		return false
	}
	for _, c := range registryCandidates(registryURL) {
		if _, ok := cfg.Auths[c]; ok {
			return true
		}
	}
	return false
}

// RegistryLogout removes stored credentials for an OCI registry.
// It runs `<engine> logout` (cleans system credential stores) then directly
// removes all matching keys from the engine's auth file to handle cases where
// logout leaves behind alias entries (e.g. docker.io vs https://index.docker.io/v1/).
func RegistryLogout(registryURL string) error {
	if err := requireEngine(); err != nil {
		return err
	}
	if err := mutate(cmdLabel("logout"), "logout", registryURL); err != nil {
		return err
	}
	// Also remove all matching alias keys directly from config.json.
	_ = removeFromAuthFile(registryURL)
	return nil
}

// removeFromAuthFile removes all URL variants of registryURL from the auths
// section of the engine's credential file. Errors are non-fatal (best effort).
func removeFromAuthFile(registryURL string) error {
	configPath, err := authPath()
	if err != nil {
		return err
	}

	var raw map[string]json.RawMessage
	if err := readAuthFile(&raw); err != nil {
		return err
	}
	authsRaw, ok := raw["auths"]
	if !ok {
		return nil
	}
	var auths map[string]json.RawMessage
	if err := json.Unmarshal(authsRaw, &auths); err != nil {
		return err
	}

	changed := false
	for _, candidate := range registryCandidates(registryURL) {
		if _, exists := auths[candidate]; exists {
			delete(auths, candidate)
			changed = true
		}
	}
	if !changed {
		return nil
	}

	updated, err := json.Marshal(auths)
	if err != nil {
		return err
	}
	raw["auths"] = updated
	out, err := json.MarshalIndent(raw, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, out, 0600)
}

// GetStoredCreds retrieves registry credentials for registryURL from the
// engine's auth file. It tries the per-registry credential helper
// (credHelpers), then the global credsStore, and finally falls back to the
// inline base64-encoded auth field. Returns ok=false when no credentials are
// found.
func GetStoredCreds(registryURL string) (username, password string, ok bool) {
	var cfg struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
		CredsStore  string            `json:"credsStore"`
		CredHelpers map[string]string `json:"credHelpers"`
	}
	if err := readAuthFile(&cfg); err != nil {
		return "", "", false
	}

	// Normalise the URL to a bare hostname for helper lookups.
	hostname := strings.TrimSuffix(registryURL, "/")
	for _, prefix := range []string{"https://", "http://"} {
		hostname = strings.TrimPrefix(hostname, prefix)
	}

	helper := cfg.CredHelpers[hostname]
	if helper == "" {
		helper = cfg.CredsStore
	}
	if helper != "" {
		if u, p, ok := getCredsFromHelper(helper, hostname); ok {
			return u, p, true
		}
	}

	for _, candidate := range registryCandidates(registryURL) {
		if entry, ok := cfg.Auths[candidate]; ok && entry.Auth != "" {
			if u, p, ok := decodeAuth(entry.Auth); ok {
				return u, p, true
			}
		}
	}
	return "", "", false
}

// decodeAuth splits a base64 "username:password" auth field.
func decodeAuth(auth string) (username, password string, ok bool) {
	decoded, err := base64.StdEncoding.DecodeString(auth)
	if err != nil {
		return "", "", false
	}
	if idx := strings.IndexByte(string(decoded), ':'); idx >= 0 {
		return string(decoded[:idx]), string(decoded[idx+1:]), true
	}
	return "", "", false
}

// getCredsFromHelper calls `docker-credential-<helper> get` and parses the JSON response.
func getCredsFromHelper(helper, serverURL string) (username, password string, ok bool) {
	out, err := runner.Run(dockerCmd{
		Args:   []string{"get"},
		Stdin:  serverURL,
		Helper: helper,
	})
	if err != nil {
		return "", "", false
	}
	var result struct {
		Username string `json:"Username"`
		Secret   string `json:"Secret"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", "", false
	}
	if result.Secret == "" {
		return "", "", false
	}
	return result.Username, result.Secret, true
}

// RegistryAlias pairs a URL prefix with a short alias for display purposes.
type RegistryAlias struct {
	URL   string
	Alias string
}

// ApplyAliases replaces registry URL prefixes in an image name with their configured alias.
// Example: URL="gitlab.com/my-group", Alias="gl" turns
// "gitlab.com/my-group/my-image:1.0" into "gl/my-image:1.0".
func ApplyAliases(imageName string, aliases []RegistryAlias) string {
	for _, a := range aliases {
		if a.Alias == "" || a.URL == "" {
			continue
		}
		prefix := strings.TrimSuffix(a.URL, "/") + "/"
		if strings.HasPrefix(imageName, prefix) {
			return a.Alias + "/" + strings.TrimPrefix(imageName, prefix)
		}
	}
	return imageName
}
