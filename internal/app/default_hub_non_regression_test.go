package app

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/command"
	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
	"github.com/anthnel/devdesk/internal/testutil/contextfiles"
)

// DEVDESK-665 (S-03), criterion 1 through the router: a context directory with
// files of every shape, a start on one of them (the secret sweep of useSecrets
// included) and a switch to each of the others. No switch adds a registry; a
// file with no legacy secret keeps its bytes, and the one that has a secret is
// rewritten by MigrateLegacySecrets alone (the expected exception).

func registryURLsOf(cfg *config.Config) []string {
	var urls []string
	for _, r := range cfg.Registry.Registries {
		urls = append(urls, r.URL)
	}
	return urls
}

func TestExistingContextDirectory_StartAndSwitchAddNoRegistry(t *testing.T) {
	store := isolateHome(t)
	if err := config.EnsureConfigDir(); err != nil {
		t.Fatal(err)
	}

	const defaultBody = "registry:\n    registries: []\n"
	write := func(name, body string) string {
		t.Helper()
		p, err := config.GetContextPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("default", defaultBody)
	shapes := contextfiles.Shapes()
	paths := map[string]string{}
	for _, s := range shapes {
		paths[s.Name] = write(s.Name, s.Content)
	}

	// The start: what New does once the terminal size is known.
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	a := newWithSize(cfg, 120, 40)
	a.views[command.ViewDashboard] = &fakeView{}
	a.resize(120, 40)
	a.useSecrets(credentials.Select(a.currentContext, cfg.App.SecretBackend))
	if got := registryURLsOf(a.config); len(got) != 0 {
		t.Fatalf("the start on the default context holds registries %v, want none", got)
	}

	for _, s := range shapes {
		drive(t, a, a.switchContext(s.Name))

		if a.currentContext != s.Name {
			t.Fatalf("%s: the switch ended on %q", s.Name, a.currentContext)
		}
		if got := registryURLsOf(a.config); !slices.Equal(got, s.URLs) {
			t.Errorf("%s: the router holds registries %v, want %v", s.Name, got, s.URLs)
		}
		data, err := os.ReadFile(paths[s.Name])
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case s.LegacySecret:
			if strings.Contains(string(data), "password") {
				t.Errorf("%s: the legacy password is still in the file:\n%s", s.Name, data)
			}
			if strings.Contains(string(data), "docker.io") {
				t.Errorf("%s: the sweep wrote docker.io:\n%s", s.Name, data)
			}
			if len(a.sharedState.SecretNotices) == 0 {
				t.Errorf("%s: the sweep reported nothing", s.Name)
			}
			if len(store.Entries()) == 0 {
				t.Errorf("%s: the secret did not reach the store", s.Name)
			}
		default:
			if string(data) != s.Content {
				t.Errorf("%s: the switch rewrote the file.\nbefore:\n%s\nafter:\n%s", s.Name, s.Content, data)
			}
		}

		// The session saves its config on the next edit: that save adds nothing.
		if err := config.Save(a.config); err != nil {
			t.Fatal(err)
		}
		reloaded, err := config.LoadContext(s.Name)
		if err != nil {
			t.Fatal(err)
		}
		if got := registryURLsOf(reloaded); !slices.Equal(got, s.URLs) {
			t.Errorf("%s: after a save the registries are %v, want %v", s.Name, got, s.URLs)
		}
	}

	// Back to the context the session started on, and a last look at it.
	drive(t, a, a.switchContext("default"))
	if got := registryURLsOf(a.config); len(got) != 0 {
		t.Errorf("back on the default context: registries %v, want none", got)
	}
}
