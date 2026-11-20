package ociresources

import (
	"net/url"
	"testing"

	"github.com/anthnel/devdesk/internal/docker"
)

// DEVDESK-629 (F7): a login stored under the host[:port] of an entry whose URL
// carries a path reads as logged in on the Registries tab, the same lookup the
// requests use (docker.GetStoredCreds).
func TestLoggedColumnSeesAHostLoginForAPathEntry(t *testing.T) {
	const pathURL = "http://127.0.0.1:30481/repository/docker-hosted"
	u, _ := url.Parse(pathURL)
	writeDockerConfig(t, map[string]any{
		"auths": map[string]any{u.Host: map[string]string{"auth": encodeAuth("anthnel", "s3cret")}},
	})

	msg := run(t, checkRegistryLoginStatusCmd([]string{pathURL})).(RegistryLoginStatusMsg)

	state, ok := msg.Status[pathURL]
	if !ok {
		t.Fatalf("Status = %v, want an answer for %s", msg.Status, pathURL)
	}
	if !state.LoggedIn() || state != docker.LoginInline {
		t.Errorf("Status[%s] = %v, want LoginInline: the login is stored under %s", pathURL, state, u.Host)
	}
}
