package docker

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// DEVDESK-629 (F7): the login of an entry whose URL carries a path
// (http://127.0.0.1:30481/repository/docker-hosted) is looked up under its exact
// address first, then under its host[:port] as written, in GetStoredCreds and in
// RegistryLoginState alike. Never the host alone when the port differs, never a
// default port made explicit.

var errHelperNotFound = errors.New("credentials not found")

const nexusPathURL = "http://127.0.0.1:30481/repository/docker-hosted"

// keyedHelperRunner answers `get` per server URL (the helper's stdin), so a test
// can tell which key was asked. It records every server URL asked.
type keyedHelperRunner struct {
	secrets map[string]string // server URL -> "user:secret"
	asked   []string
}

func (r *keyedHelperRunner) LookPath() error { return nil }

func (r *keyedHelperRunner) Build(args ...string) *exec.Cmd { return nil }

func (r *keyedHelperRunner) Run(dc dockerCmd) ([]byte, error) {
	if dc.Helper == "" {
		return nil, nil
	}
	r.asked = append(r.asked, dc.Stdin)
	cred, ok := r.secrets[dc.Stdin]
	if !ok {
		return []byte(`credentials not found in native keychain`), errHelperNotFound
	}
	user, secret, _ := strings.Cut(cred, ":")
	out, _ := json.Marshal(map[string]string{"Username": user, "Secret": secret})
	return out, nil
}

// isolatedAuthFile points HOME at a temp dir, pins the docker engine and writes
// ~/.docker/config.json with cfg; the shared runner stays the stub's.
func isolatedAuthFile(t *testing.T, cfg map[string]any) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	useEngine(t, "docker")
	dir := filepath.Join(home, ".docker")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func inline(user, password string) map[string]string {
	return map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(user + ":" + password))}
}

func TestGetStoredCredsFallsBackToTheHostKey(t *testing.T) {
	tests := []struct {
		name     string
		auths    map[string]any
		url      string
		wantUser string // "" means not found
	}{
		{"host:port key, URL with a path", map[string]any{"127.0.0.1:30481": inline("host", "pw")}, nexusPathURL, "host"},
		{"exact key and host key: the exact key wins",
			map[string]any{"127.0.0.1:30481": inline("host", "pw"), "127.0.0.1:30481/repository/docker-hosted": inline("exact", "pw")},
			nexusPathURL, "exact"},
		{"bare host key, URL carries another port", map[string]any{"127.0.0.1": inline("host", "pw")}, nexusPathURL, ""},
		{"other port key", map[string]any{"127.0.0.1:30482": inline("host", "pw")}, nexusPathURL, ""},
		{"host key without the explicit default port", map[string]any{"nexus.example": inline("host", "pw")}, "https://nexus.example:443/r/x", ""},
		{"explicit default port key, URL without it", map[string]any{"nexus.example:443": inline("host", "pw")}, "https://nexus.example/r/x", ""},
		{"https:// variant of the host key", map[string]any{"https://nexus.example": inline("host", "pw")}, "https://nexus.example/repository/x", "host"},
		{"URL without a scheme", map[string]any{"nexus.example": inline("host", "pw")}, "nexus.example/repository/x", "host"},
		{"host key is case sensitive", map[string]any{"Nexus.example": inline("host", "pw")}, "https://nexus.example/repository/x", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub(t, &stubRunner{})
			isolatedAuthFile(t, map[string]any{"auths": tt.auths})

			user, _, ok := GetStoredCreds(tt.url)

			if tt.wantUser == "" {
				if ok {
					t.Fatalf("GetStoredCreds(%q) = %q, found; want nothing", tt.url, user)
				}
				return
			}
			if !ok || user != tt.wantUser {
				t.Errorf("GetStoredCreds(%q) = %q, %v; want %q", tt.url, user, ok, tt.wantUser)
			}
		})
	}
}

func TestGetStoredCredsAsksTheHelperWithTheHostKey(t *testing.T) {
	tests := []struct {
		name      string
		cfg       map[string]any
		wantAsked []string
	}{
		{"credHelpers on the host key", map[string]any{"credHelpers": map[string]string{"127.0.0.1:30481": "pass"}}, []string{"127.0.0.1:30481"}},
		{"credsStore: exact address first, then the host key", map[string]any{"credsStore": "desktop"},
			[]string{"127.0.0.1:30481/repository/docker-hosted", "127.0.0.1:30481"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &keyedHelperRunner{secrets: map[string]string{"127.0.0.1:30481": "host:pw"}}
			stub(t, &stubRunner{})
			runner = r // stub restores the previous runner on cleanup
			isolatedAuthFile(t, tt.cfg)

			user, password, ok := GetStoredCreds(nexusPathURL)

			if !ok || user != "host" || password != "pw" {
				t.Fatalf("GetStoredCreds = %q/%q, %v; want host/pw from the helper", user, password, ok)
			}
			if strings.Join(r.asked, "|") != strings.Join(tt.wantAsked, "|") {
				t.Errorf("helper asked for %v, want %v", r.asked, tt.wantAsked)
			}
		})
	}
}

func TestGetStoredCredsDoesNotAskTheHelperForAnotherPort(t *testing.T) {
	r := &keyedHelperRunner{secrets: map[string]string{"127.0.0.1": "host:pw"}}
	stub(t, &stubRunner{})
	runner = r
	isolatedAuthFile(t, map[string]any{"credsStore": "desktop"})

	if _, _, ok := GetStoredCreds(nexusPathURL); ok {
		t.Fatal("GetStoredCreds found a login stored for the bare host, but the URL names port 30481")
	}
	for _, asked := range r.asked {
		if asked == "127.0.0.1" {
			t.Errorf("the helper was asked for %q: the host alone must never be tried when the port differs", asked)
		}
	}
}

func TestRegistryLoginStateSeesAHostLogin(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]any
		secrets map[string]string
		url     string
		want    LoginState
	}{
		{"inline secret under the host key", map[string]any{"auths": map[string]any{"127.0.0.1:30481": inline("u", "p")}}, nil, nexusPathURL, LoginInline},
		{"empty entry under the host key and a credsStore",
			map[string]any{"auths": map[string]any{"127.0.0.1:30481": map[string]string{}}, "credsStore": "desktop"}, nil, nexusPathURL, LoginHelper},
		{"empty entry under the host key and a credHelper for it",
			map[string]any{"auths": map[string]any{"127.0.0.1:30481": map[string]string{}}, "credHelpers": map[string]string{"127.0.0.1:30481": "pass"}},
			nil, nexusPathURL, LoginHelper},
		{"credHelper for the host key that holds the login (podman)",
			map[string]any{"credHelpers": map[string]string{"127.0.0.1:30481": "pass"}},
			map[string]string{"127.0.0.1:30481": "u:p"}, nexusPathURL, LoginHelper},
		{"bare host key, another port", map[string]any{"auths": map[string]any{"127.0.0.1": inline("u", "p")}}, nil, nexusPathURL, LoginNone},
		{"explicit default port key, URL without it", map[string]any{"auths": map[string]any{"nexus.example:443": inline("u", "p")}}, nil, "https://nexus.example/r/x", LoginNone},
		{"https:// variant of the host key", map[string]any{"auths": map[string]any{"https://nexus.example": inline("u", "p")}}, nil, "https://nexus.example/repository/x", LoginInline},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub(t, &stubRunner{})
			runner = &keyedHelperRunner{secrets: tt.secrets}
			isolatedAuthFile(t, tt.cfg)

			if got := RegistryLoginState(tt.url); got != tt.want {
				t.Errorf("RegistryLoginState(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

// removeFromAuthFile, the auth-file half of logout, deletes the exact key only:
// the host's own key stays in the file and GetStoredCreds still reads it. This
// exercises that function alone, not the real `docker logout` (which a CLI or a
// credential helper may carry out differently), so it does not prove the host's
// login survives a logout in every setup.
func TestRegistryLogoutKeepsTheHostLogin(t *testing.T) {
	stub(t, &stubRunner{})
	isolatedAuthFile(t, map[string]any{"auths": map[string]any{
		"127.0.0.1:30481":                          inline("host", "pw"),
		"127.0.0.1:30481/repository/docker-hosted": inline("exact", "pw"),
	}})

	if err := removeFromAuthFile(nexusPathURL); err != nil {
		t.Fatal(err)
	}

	if _, _, ok := GetStoredCreds("http://127.0.0.1:30481"); !ok {
		t.Error("the host login was removed together with the path entry's own key")
	}
	if user, _, ok := GetStoredCreds(nexusPathURL); !ok || user != "host" {
		t.Errorf("after removing the exact key, GetStoredCreds = %q, %v; want the host login", user, ok)
	}
}
