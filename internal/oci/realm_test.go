package oci

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// DEVDESK-629 step 1 (F1): a Bearer realm on another origin than the request that
// received the challenge is refused when credentials are configured, before any
// request to it. Without credentials the anonymous token request still goes out.
// The error names the realm's host only.

func realmChallenge(realm string) string {
	return `realm="` + realm + `",service="s",scope="repository:x:pull"`
}

func TestCheckRealmOrigin(t *testing.T) {
	for _, tt := range []struct {
		name      string
		registry  string
		challenge string
		creds     bool
		refused   bool
		hostShown string   // host the error must name, when refused
		noLeak    []string // texts the error must not carry, when refused
	}{
		{"same origin", "https://reg.example", realmChallenge("https://reg.example/token"), true, false, "", nil},
		{"another host, credentials", "https://reg.example", realmChallenge("https://auth.example/token"), true, true, "auth.example", []string{"/token"}},
		{"another host, no credentials", "https://reg.example", realmChallenge("https://auth.example/token"), false, false, "", nil},
		{"another port", "https://reg.example:5000", realmChallenge("https://reg.example:5001/t"), true, true, "reg.example:5001", nil},
		{"host and host:443 are two origins", "https://reg.example", realmChallenge("https://reg.example:443/t"), true, true, "reg.example:443", nil},
		{"another scheme", "http://reg.example", realmChallenge("https://reg.example/t"), true, true, "reg.example", nil},
		{"same origin, registry URL with a path", "https://reg.example/v2/x", realmChallenge("https://reg.example/token"), true, false, "", nil},

		{"docker hub exception", "https://registry-1.docker.io/v2/x", realmChallenge("https://auth.docker.io/token"), true, false, "", nil},
		{"docker hub exception does not take a port", "https://registry-1.docker.io", realmChallenge("https://auth.docker.io:443/token"), true, true, "auth.docker.io:443", nil},
		{"docker hub exception needs https on the registry", "http://registry-1.docker.io", realmChallenge("https://auth.docker.io/token"), true, true, "auth.docker.io", nil},
		{"docker hub exception needs https on the realm", "https://registry-1.docker.io", realmChallenge("http://auth.docker.io/token"), true, true, "auth.docker.io", nil},
		{"docker.io is not registry-1.docker.io", "https://docker.io", realmChallenge("https://auth.docker.io/token"), true, true, "auth.docker.io", nil},
		{"docker hub exception is one way", "https://auth.docker.io", realmChallenge("https://registry-1.docker.io/t"), true, true, "registry-1.docker.io", nil},
		{"docker hub realm for another registry", "https://reg.example", realmChallenge("https://auth.docker.io/token"), true, true, "auth.docker.io", nil},

		{"gitlab.com exception", "https://registry.gitlab.com", realmChallenge("https://gitlab.com/jwt/auth"), true, false, "", nil},
		{"gitlab.com exception does not take a port", "https://registry.gitlab.com", realmChallenge("https://gitlab.com:443/jwt/auth"), true, true, "gitlab.com:443", nil},
		{"gitlab.com exception needs https on the registry", "http://registry.gitlab.com", realmChallenge("https://gitlab.com/jwt/auth"), true, true, "gitlab.com", nil},
		{"gitlab.com exception is one way", "https://gitlab.com", realmChallenge("https://registry.gitlab.com/jwt/auth"), true, true, "registry.gitlab.com", nil},
		{"self-hosted gitlab stays refused", "https://registry.example.com", realmChallenge("https://gitlab.example.com/jwt/auth"), true, true, "gitlab.example.com", nil},
		{"gitlab.com realm for another registry", "https://reg.example", realmChallenge("https://gitlab.com/jwt/auth"), true, true, "gitlab.com", nil},

		{"userinfo and query never named", "https://reg.example", realmChallenge("https://user:pw@evil.example/t?secret=x"), true, true, "evil.example", []string{"user", "pw", "secret", "/t"}},
		{"unreadable registry URL", "://bad", realmChallenge("https://auth.example/t"), true, true, "auth.example", nil},
		{"no realm", "https://reg.example", `service="s"`, true, false, "", nil},
		{"unparsable realm", "https://reg.example", `realm="%zz"`, true, false, "", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := checkRealmOrigin(tt.registry, tt.challenge, tt.creds)
			if !tt.refused {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, ErrTokenRealmOffOrigin) {
				t.Fatalf("err = %v, want ErrTokenRealmOffOrigin", err)
			}
			if !strings.Contains(err.Error(), tt.hostShown) {
				t.Errorf("err = %q, want the host %q named", err, tt.hostShown)
			}
			for _, leak := range tt.noLeak {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("err = %q leaks %q", err, leak)
				}
			}
			if !strings.Contains(err.Error(), "token realm on another host") {
				t.Errorf("err = %q, want the text the footer shows", err)
			}
		})
	}
}

// The exceptions are two, frozen: adding one is a deliberate change of this test
// (DEVDESK-629, decision of 2026-11-20: Docker Hub and GitLab.com).
func TestRealmExceptionsAreFrozen(t *testing.T) {
	want := []realmException{
		{registryHost: "registry-1.docker.io", realmHost: "auth.docker.io"},
		{registryHost: "registry.gitlab.com", realmHost: "gitlab.com"},
	}
	if !reflect.DeepEqual(realmExceptions, want) {
		t.Errorf("realmExceptions = %+v, want exactly %+v", realmExceptions, want)
	}
}
