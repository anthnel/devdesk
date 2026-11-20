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
		// Edge cases frozen by the security audit: host comparison is exact (no suffix
		// match, no case folding, no trailing-dot folding), and a spoofed userinfo is
		// never named. A change to suffix or case-insensitive matching fails here.
		{"userinfo hides the real host", "https://reg.example", realmChallenge("https://reg@evil.example/token"), true, true, "evil.example", []string{"reg@"}},
		{"userinfo imitates the registry host", "https://reg.example", realmChallenge("https://reg.example@evil.example/token"), true, true, "evil.example", []string{"reg.example@"}},
		{"userinfo does not make a same origin", "https://reg.example", realmChallenge("https://reg.example@reg.example:444/token"), true, true, "reg.example:444", []string{"reg.example@"}},
		{"gitlab.com as a prefix of the realm host", "https://registry.gitlab.com", realmChallenge("https://gitlab.com.evil.example/jwt/auth"), true, true, "gitlab.com.evil.example", nil},
		{"gitlab registry host as a prefix of the realm host", "https://registry.gitlab.com", realmChallenge("https://registry.gitlab.com.evil.example/jwt/auth"), true, true, "registry.gitlab.com.evil.example", nil},
		{"gitlab registry host as a suffix of the registry", "https://evil.registry.gitlab.com", realmChallenge("https://gitlab.com/jwt/auth"), true, true, "gitlab.com", nil},
		{"gitlab realm as a subdomain", "https://registry.gitlab.com", realmChallenge("https://x.gitlab.com/jwt/auth"), true, true, "x.gitlab.com", nil},
		{"docker hub realm as a subdomain", "https://registry-1.docker.io", realmChallenge("https://x.auth.docker.io/token"), true, true, "x.auth.docker.io", nil},
		{"docker hub realm as a prefix", "https://registry-1.docker.io", realmChallenge("https://auth.docker.io.evil.example/token"), true, true, "auth.docker.io.evil.example", nil},
		{"docker hub realm on the bare domain", "https://registry-1.docker.io", realmChallenge("https://docker.io/token"), true, true, "docker.io", nil},
		{"docker hub registry as a suffix of the registry", "https://evil.registry-1.docker.io", realmChallenge("https://auth.docker.io/token"), true, true, "auth.docker.io", nil},
		{"upper-case realm host is another origin", "https://reg.example", realmChallenge("https://REG.example/token"), true, true, "REG.example", nil},
		{"upper-case registry host is another origin", "https://REG.example", realmChallenge("https://reg.example/token"), true, true, "reg.example", nil},
		{"upper-case docker hub realm host", "https://registry-1.docker.io", realmChallenge("https://AUTH.DOCKER.IO/token"), true, true, "AUTH.DOCKER.IO", nil},
		{"upper-case docker hub registry host", "https://REGISTRY-1.DOCKER.IO", realmChallenge("https://auth.docker.io/token"), true, true, "auth.docker.io", nil},
		{"upper-case gitlab realm host", "https://registry.gitlab.com", realmChallenge("https://GITLAB.COM/jwt/auth"), true, true, "GITLAB.COM", nil},
		{"trailing dot on the realm host", "https://reg.example", realmChallenge("https://reg.example./token"), true, true, "reg.example.", nil},
		{"trailing dot on the registry host", "https://reg.example.", realmChallenge("https://reg.example/token"), true, true, "reg.example", nil},
		{"trailing dot on the docker hub realm host", "https://registry-1.docker.io", realmChallenge("https://auth.docker.io./token"), true, true, "auth.docker.io.", nil},
		{"trailing dot on the docker hub registry host", "https://registry-1.docker.io.", realmChallenge("https://auth.docker.io/token"), true, true, "auth.docker.io", nil},
		{"trailing dot on the gitlab realm host", "https://registry.gitlab.com", realmChallenge("https://gitlab.com./jwt/auth"), true, true, "gitlab.com.", nil},
		{"malformed scheme separator, one slash", "https://reg.example", realmChallenge("https:/evil.example/token"), true, true, "", []string{"evil.example"}},
		{"malformed scheme separator, no host", "https://reg.example", realmChallenge("https:evil.example/token"), true, true, "", []string{"evil.example"}},
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
