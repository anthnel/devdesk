package oci

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

// DEVDESK-412 step 8 (group C): exchangeBearerTokenContext does not echo the raw
// realm when it cannot parse it, and applies checkTokenRealm itself, so no caller
// can reach a token endpoint in clear behind an https registry by skipping
// registryDoContext.
//
// Signature change these tests assume, to be confirmed:
//
//	exchangeBearerTokenContext(ctx, registryURL, challenge, username, password string)
//	exchangeBearerToken(registryURL, challenge, username, password string)

// F4: the error of a realm that does not parse holds no userinfo, no query and
// not the raw realm.
func TestExchangeBearerTokenParseErrorDoesNotEchoTheRealm(t *testing.T) {
	for _, tc := range []struct{ name, realm string }{
		{"bad escape", "https://alice:" + realmTestPassword + "@auth.example/%zz?sig=SECRET"},
		{"bad port", "https://alice:" + realmTestPassword + "@auth.example:bad/token?sig=SECRET"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			challenge := `realm="` + tc.realm + `",service="registry.example"`

			_, err := exchangeBearerTokenContext(context.Background(), "https://registry.example", challenge, "", "")

			if err == nil {
				t.Fatal("err = nil, want a parse failure")
			}
			for _, leak := range []string{realmTestPassword, "SECRET", "sig=", tc.realm} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("error %q repeats %q", err, leak)
				}
			}
		})
	}
}

// F3: a direct call refuses an http realm behind an https registry, with no
// request, whether or not credentials are given.
func TestExchangeBearerTokenRefusesAnInsecureRealmItself(t *testing.T) {
	var hits hitLog
	realm := httptest.NewServer(tokenHandler(&hits))
	t.Cleanup(realm.Close)
	challenge := `realm="` + realm.URL + `/token",service="registry.example"`

	for _, creds := range realmCreds {
		t.Run(creds.name, func(t *testing.T) {
			_, err := exchangeBearerTokenContext(context.Background(), "https://registry.example", challenge, creds.user, creds.pass)
			if !errors.Is(err, errInsecureTokenRealm) {
				t.Fatalf("err = %v, want errInsecureTokenRealm", err)
			}
			if strings.Contains(err.Error(), realmTestPassword) || strings.Contains(err.Error(), "/token") {
				t.Errorf("error %q holds the password or the realm path", err)
			}
		})
		t.Run(creds.name+"/without context", func(t *testing.T) {
			_, err := exchangeBearerToken("https://registry.example", challenge, creds.user, creds.pass)
			if !errors.Is(err, errInsecureTokenRealm) {
				t.Fatalf("err = %v, want errInsecureTokenRealm", err)
			}
		})
	}
	if n := hits.count(); n != 0 {
		t.Errorf("the realm received %d requests, want 0", n)
	}
}

// The check does not refuse what was allowed: an http registry may use an http
// realm, and an https registry an https one.
func TestExchangeBearerTokenStillServesAllowedRealms(t *testing.T) {
	var hits hitLog
	plain := httptest.NewServer(tokenHandler(&hits))
	t.Cleanup(plain.Close)
	challenge := `realm="` + plain.URL + `/token"`

	tok, err := exchangeBearerTokenContext(context.Background(), "http://registry.example", challenge, "", "")
	if err != nil || tok != "tok" {
		t.Fatalf("http registry, http realm: %q, %v; want tok", tok, err)
	}

	secure := httptest.NewTLSServer(tokenHandler(&hits))
	t.Cleanup(secure.Close)
	old := registryHTTPClient
	c := *old
	c.Transport = secure.Client().Transport
	registryHTTPClient = &c
	t.Cleanup(func() { registryHTTPClient = old })
	tok, err = exchangeBearerToken("https://registry.example", `realm="`+secure.URL+`/token"`, "", "")
	if err != nil || tok != "tok" {
		t.Fatalf("https registry, https realm: %q, %v; want tok", tok, err)
	}
}
