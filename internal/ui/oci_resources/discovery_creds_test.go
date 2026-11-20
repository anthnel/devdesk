package ociresources

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// DEVDESK-629 (M1): the login stored for the registry host (A) must never travel
// to the management host (B) when the two differ. F7 made GetStoredCreds find a
// login by host for a URL that carries a path, so the password was no longer empty
// and the lookup for B's own login was skipped.
func TestDiscoveryCredsNeverSendTheRegistryHostLoginToAnotherManagementHost(t *testing.T) {
	const (
		regURL  = "https://registry-a.example.test:5000/repository/docker-group"
		mgmtURL = "https://nexus-b.example.test:8081/repository/docker-group"
	)
	tests := []struct {
		name         string
		auths        map[string]any
		reg          config.RegistryItem
		user, pass   string
		wantUser     string
		wantPassword string
	}{
		{
			name:  "login for A only: nothing for B",
			auths: map[string]any{"registry-a.example.test:5000": map[string]string{"auth": encodeAuth("alice", "pass-of-a")}},
			reg:   config.RegistryItem{URL: regURL, ManagementURL: mgmtURL},
		},
		{
			name: "login for B: used",
			auths: map[string]any{
				"registry-a.example.test:5000": map[string]string{"auth": encodeAuth("alice", "pass-of-a")},
				"nexus-b.example.test:8081":    map[string]string{"auth": encodeAuth("bob", "pass-of-b")},
			},
			reg:          config.RegistryItem{URL: regURL, ManagementURL: mgmtURL},
			wantUser:     "bob",
			wantPassword: "pass-of-b",
		},
		{
			name:  "same host: the registry login is used as before",
			auths: map[string]any{"registry-a.example.test:5000": map[string]string{"auth": encodeAuth("alice", "pass-of-a")}},
			reg: config.RegistryItem{
				URL:           regURL,
				ManagementURL: "https://registry-a.example.test:5000/service/rest",
			},
			wantUser:     "alice",
			wantPassword: "pass-of-a",
		},
		{
			name:         "stated credentials outrank everything",
			auths:        map[string]any{"registry-a.example.test:5000": map[string]string{"auth": encodeAuth("alice", "pass-of-a")}},
			reg:          config.RegistryItem{URL: regURL, ManagementURL: mgmtURL},
			user:         "typed",
			pass:         "typed-pass",
			wantUser:     "typed",
			wantPassword: "typed-pass",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeDockerConfig(t, map[string]any{"auths": tt.auths})

			user, pass := discoveryCreds(tt.reg, tt.user, tt.pass)

			if user != tt.wantUser || pass != tt.wantPassword {
				t.Errorf("discoveryCreds = %q/%q, want %q/%q", user, pass, tt.wantUser, tt.wantPassword)
			}
		})
	}
}

// The wire view of M1: B, a real server, receives no Basic header built from A's
// login; an anonymous registry still sends nothing.
func TestTheManagementHostReceivesNoBasicFromTheRegistryHost(t *testing.T) {
	var gotAuth string
	var asked bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = true
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"type":"hosted","format":"docker"}`))
	}))
	defer srv.Close()

	// A is a name nothing listens on: it is only the key of the stored login.
	writeDockerConfig(t, map[string]any{
		"auths": map[string]any{"registry-a.example.test:5000": map[string]string{"auth": encodeAuth("alice", "pass-of-a")}},
	})

	reg := config.RegistryItem{
		URL:           "https://registry-a.example.test:5000/repository/docker-group",
		ManagementURL: srv.URL + "/repository/docker-group",
		Provider:      config.ProviderNexus,
	}
	run(t, detectRegistryGroupCmd(reg, ""))

	if !asked {
		t.Fatal("the management host was never asked")
	}
	if gotAuth != "" {
		t.Errorf("the management host received Authorization %q, want none", gotAuth)
	}

	asked, gotAuth = false, ""
	reg.AuthMode = config.AuthAnonymous
	run(t, detectRegistryGroupCmd(reg, ""))
	if !asked || gotAuth != "" {
		t.Errorf("anonymous: asked=%v Authorization=%q, want asked and none", asked, gotAuth)
	}
}
