package oci

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// These tests pin the typed errors of DEVDESK-298 (IS-18 criterion 1): a
// non-success status from the registry or the token endpoint is a
// *StatusError, and Error() keeps today's strings character for character.

func TestListRegistryTagsStatusError(t *testing.T) {
	tests := []struct {
		name          string
		code          int
		challenge     string
		wantChallenge string
		wantMsg       string
	}{
		{"401 without challenge", 401, "", "", "unexpected auth challenge: "},
		{"401 basic challenge", 401, `Basic realm="x"`, `Basic realm="x"`, `unexpected auth challenge: Basic realm="x"`},
		{"403", 403, "", "", "registry returned 403"},
		{"404", 404, "", "", "registry returned 404"},
		{"405", 405, "", "", "registry returned 405"},
		{"429", 429, "", "", "registry returned 429"},
		{"500", 500, "", "", "registry returned 500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.challenge != "" {
					w.Header().Set("Www-Authenticate", tt.challenge)
				}
				w.WriteHeader(tt.code)
			}))
			defer srv.Close()

			_, err := ListRegistryTags(srv.URL, "library/alpine", "", "")
			var se *StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v (%T), want a *StatusError", err, err)
			}
			if se.Code != tt.code {
				t.Errorf("Code = %d, want %d", se.Code, tt.code)
			}
			if se.Source != SourceRegistry {
				t.Errorf("Source = %v, want SourceRegistry", se.Source)
			}
			if se.Challenge != tt.wantChallenge {
				t.Errorf("Challenge = %q, want %q", se.Challenge, tt.wantChallenge)
			}
			if got := err.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestListRegistryTagsTokenEndpointStatusError(t *testing.T) {
	for _, code := range []int{401, 403, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					w.WriteHeader(code)
					return
				}
				w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="reg"`, srv.URL))
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer srv.Close()

			_, err := ListRegistryTags(srv.URL, "library/alpine", "", "")
			var se *StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v (%T), want a *StatusError", err, err)
			}
			if se.Code != code {
				t.Errorf("Code = %d, want %d", se.Code, code)
			}
			if se.Source != SourceToken {
				t.Errorf("Source = %v, want SourceToken", se.Source)
			}
			if se.Challenge != "" {
				t.Errorf("Challenge = %q, want empty", se.Challenge)
			}
			if want := fmt.Sprintf("token endpoint returned %d", code); err.Error() != want {
				t.Errorf("Error() = %q, want %q", err.Error(), want)
			}
		})
	}
}

// Failures that are not a status stay plain errors: a caller must not read a
// missing realm or a bad body as an HTTP verdict.
func TestListRegistryTagsNonStatusErrorsAreNotStatusErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler func(srv **httptest.Server) http.HandlerFunc
	}{
		{"undecodable tags body", func(_ **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) }
		}},
		{"bearer challenge missing realm", func(_ **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Www-Authenticate", `Bearer service="reg"`)
				w.WriteHeader(http.StatusUnauthorized)
			}
		}},
		{"token response without token", func(srv **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					_, _ = w.Write([]byte(`{}`))
					return
				}
				w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token"`, (*srv).URL))
				w.WriteHeader(http.StatusUnauthorized)
			}
		}},
		{"undecodable token body", func(srv **httptest.Server) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					_, _ = w.Write([]byte("not json"))
					return
				}
				w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token"`, (*srv).URL))
				w.WriteHeader(http.StatusUnauthorized)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var srv *httptest.Server
			srv = httptest.NewServer(tt.handler(&srv))
			defer srv.Close()

			_, err := ListRegistryTags(srv.URL, "library/alpine", "", "")
			if err == nil {
				t.Fatal("expected an error")
			}
			var se *StatusError
			if errors.As(err, &se) {
				t.Errorf("err = %v is a *StatusError, want a plain error", err)
			}
		})
	}
}

// A 401 received after a successful Bearer token exchange is a plain registry
// status, not an "unexpected auth challenge".
func TestListRegistryTags401AfterTokenExchange(t *testing.T) {
	srv := bearerRegistry(t, okToken, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	for name, list := range map[string]func() ([]string, error){
		"plain":   func() ([]string, error) { return ListRegistryTags(srv.URL, "x", "", "") },
		"context": func() ([]string, error) { return ListRegistryTagsContext(context.Background(), srv.URL, "x", "", "") },
	} {
		t.Run(name, func(t *testing.T) {
			_, err := list()
			var se *StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v (%T), want a *StatusError", err, err)
			}
			if se.Code != http.StatusUnauthorized || se.Source != SourceRegistry {
				t.Errorf("got Code %d Source %v, want 401 SourceRegistry", se.Code, se.Source)
			}
			if got := err.Error(); got != "registry returned 401" {
				t.Errorf("Error() = %q, want %q", got, "registry returned 401")
			}
		})
	}
}
