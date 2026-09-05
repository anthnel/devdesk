package mcp

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthnel/devdesk/internal/credentials"
)

// TokenURL is the key the server's bearer token is stored under.
//
// The Storage interface is addressed by URL because every other secret DevDesk
// holds belongs to one — a forge, a registry. This one belongs to DevDesk
// itself, so it gets a sentinel rather than a host. The keyring namespaces the
// account by context on top of it, so two contexts do not share a token.
const TokenURL = "devdesk://mcp"

// tokenBytes is how much entropy the token carries. 32 bytes is what a bearer
// guarding an interface that clones and scans should cost to guess, and it is
// the size at which nobody is tempted to type it by hand rather than paste it.
const tokenBytes = 32

// ResolveToken returns this context's server token, creating one the first time.
//
// It refuses a store that does not persist, and the refusal is the point. A
// token regenerated every launch would break the agent's configuration once per
// session, silently — and the failure would be read as the agent's, never as
// the secret store's. `credentials.Select` falls back to memory when no host
// store answers, so this is a real path and not a theoretical one.
//
// It is never written to a file DevDesk owns: that is §3.9's rule, and a
// server token is exactly the kind of secret that would otherwise end up in
// config.yaml "because it is only a local one".
func ResolveToken(sel credentials.Selection) (string, error) {
	if !sel.Persists() {
		return "", fmt.Errorf("the MCP server needs a secret store that survives the session, and there is none: %s", sel.Detail)
	}

	if token, err := sel.Storage.Load(TokenURL); err == nil && token != "" {
		return token, nil
	}

	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate an MCP token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	if err := sel.Storage.Save(TokenURL, token); err != nil {
		return "", fmt.Errorf("store the MCP token: %w", err)
	}
	return token, nil
}

// Authorize wraps a handler in the bearer check.
//
// The token is not carried on Env, and that is deliberate: Env is handed to
// every tool's register closure, so a token reachable from there is one that
// finds its way into an answer eventually. Authorization is the transport's
// business and stays at the transport.
//
// A request without the right bearer is refused before the body is read. The
// comparison is constant-time — the server answers on the loopback, where an
// attacker gets as many attempts as it likes with no network jitter to hide a
// timing difference, which is the case where it actually matters.
//
// §3.38 needed none of this: stdio has no authentication because the process
// *is* the user. A port that clones and scans is reachable by every process on
// the machine — an npm postinstall, an editor extension — so the token is not
// an option. The sandbox's network policy protects the sandbox; it does not
// protect the host from itself.
func Authorize(next http.Handler, token string) http.Handler {
	want := []byte(token)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
