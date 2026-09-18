package template

import (
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/credentials"
)

// A token authenticates one host: it must never be offered to another, and the
// catalog can be shared, so the entry cannot be what decides.
func TestCredentialsAreOnlyOfferedToTheirOwnHost(t *testing.T) {
	cfg := config.Default()
	cfg.Forge.URL = "https://gitlab.example.com"
	cfg.Registry.URL = "https://registry.example.com"
	cfg.Registry.Username = "ada"
	secrets := credentials.NewMemoryStorage()
	_ = secrets.Save(cfg.Forge.URL, "forge-token")
	_ = secrets.Save(cfg.Registry.URL, "registry-password")

	tests := []struct {
		name string
		src  Source
		want Credentials
	}{
		{"git on the forge", Source{Kind: KindGit, URL: "https://gitlab.example.com/a/b.git"}, Credentials{Token: "forge-token"}},
		{"git over ssh on the forge", Source{Kind: KindGit, URL: "git@gitlab.example.com:a/b.git"}, Credentials{Token: "forge-token"}},
		{"git elsewhere", Source{Kind: KindGit, URL: "https://github.com/a/b.git"}, Credentials{}},
		{"oci on the registry", Source{Kind: KindOCI, URL: "https://registry.example.com"}, Credentials{Username: "ada", Password: "registry-password"}},
		{"oci elsewhere", Source{Kind: KindOCI, URL: "https://ghcr.io"}, Credentials{}},
		{"local", Source{Kind: KindLocal, Path: "/x"}, Credentials{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CredentialsFor(cfg, secrets, tc.src); got != tc.want {
				t.Errorf("credentialsFor() = %+v, want %+v", got, tc.want)
			}
		})
	}

	if got := CredentialsFor(cfg, nil, tests[0].src); got != (Credentials{}) {
		t.Errorf("with no secret store, credentials = %+v, want none", got)
	}
}
