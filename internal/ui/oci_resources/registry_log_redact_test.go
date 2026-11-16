package ociresources

import (
	"errors"
	"strings"
	"testing"
)

// DEVDESK-412 final audit: the registries tab logs the address of a registry for
// login, logout and group detection. The registry config holds addresses with or
// without a scheme, and either may carry a password. The log keeps the host (it
// says which registry) and not the password.
func TestRegistryTabLogsDoNotCarryThePassword(t *testing.T) {
	const secret = "S3CRETPW"
	for _, raw := range []string{
		"https://alice:" + secret + "@r.example:5000",
		"alice:" + secret + "@r.example:5000",
	} {
		t.Run(raw, func(t *testing.T) {
			m := loadedModel(t)
			for name, msg := range map[string][]any{
				"login failed":     {RegistryLoginCompleteMsg{RegistryURL: raw, Err: errors.New("boom")}},
				"login ok":         {RegistryLoginCompleteMsg{RegistryURL: raw}},
				"logout failed":    {RegistryLogoutCompleteMsg{RegistryURL: raw, Err: errors.New("boom")}},
				"logout ok":        {RegistryLogoutCompleteMsg{RegistryURL: raw}},
				"detection failed": {RegistryGroupDetectedMsg{Slug: "x", RegistryURL: raw, Err: errors.New("boom")}},
			} {
				t.Run(name, func(t *testing.T) {
					logs := captureLog(t)
					for _, one := range msg {
						switch v := one.(type) {
						case RegistryLoginCompleteMsg:
							feed(t, m, v)
						case RegistryLogoutCompleteMsg:
							feed(t, m, v)
						case RegistryGroupDetectedMsg:
							feed(t, m, v)
						}
					}
					if !strings.Contains(logs.String(), "r.example") {
						t.Fatalf("the registry was not logged (the test checks nothing): %q", logs.String())
					}
					if strings.Contains(logs.String(), secret) {
						t.Errorf("the log holds the password: %q", logs.String())
					}
				})
			}
		})
	}
}
