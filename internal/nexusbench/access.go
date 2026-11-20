package nexusbench

import (
	"context"
	"net/http"
	"slices"
	"strings"
)

// The two settings a recording of the `access` campaign changes on the bench, and
// the read that goes with them (DEVDESK-580, PR4, W5 and W6). They exist for the
// guarded scenario test, which switches a setting, records the fixtures of that
// state in-process, then brings the bench back with Provision: they are bench
// tools, not features of DevDesk. Like RestoreUpstream they refuse a REST origin
// that is not the loopback, send the stored admin credentials and nothing else,
// and never put a password in an error.

// authenticatingRealm is the realm without which the admin account cannot sign in
// to repair the bench.
const authenticatingRealm = "NexusAuthenticatingRealm"

// accessClient is the admin client of a brick: the context is checked first (a
// cancelled one is returned as is), then the origin, then the stored credentials.
func accessClient(ctx context.Context, baseURL string, store SecretStore, p Params) (*client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := requireLoopbackBase(baseURL); err != nil {
		return nil, err
	}
	c, _, err := adminFromStore(ctx, baseURL, store)
	if err != nil {
		return nil, accessError(ctx, err, p, baseURL)
	}
	return c, nil
}

// accessError returns a cancelled context as is and any other failure as a
// *BenchError.
func accessError(ctx context.Context, err error, p Params, baseURL string) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return asBenchError(err, p, baseURL)
}

// ActiveRealms reads the realms that are active on the bench, in the order the
// instance lists them.
func ActiveRealms(ctx context.Context, baseURL string, store SecretStore, p Params) ([]string, error) {
	c, err := accessClient(ctx, baseURL, store, p)
	if err != nil {
		return nil, err
	}
	defer c.closeIdle()
	var active []string
	if err := c.read(ctx, "read the active realms", activeRealms, nil, &active); err != nil {
		return nil, accessError(ctx, err, p, baseURL)
	}
	return active, nil
}

// SetActiveRealms replaces the active realms with realms, in that order, in one
// PUT. A list that would lock the bench out (none, without
// NexusAuthenticatingRealm, or with a blank name) is refused before any request:
// Provision, which puts the bench back, signs in as admin.
func SetActiveRealms(ctx context.Context, baseURL string, store SecretStore, p Params, realms []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := requireSafeRealms(realms); err != nil {
		return err
	}
	c, err := accessClient(ctx, baseURL, store, p)
	if err != nil {
		return err
	}
	defer c.closeIdle()
	if err := c.write(ctx, "set the active realms", http.MethodPut, activeRealms, realms); err != nil {
		return accessError(ctx, err, p, baseURL)
	}
	return nil
}

// requireSafeRealms refuses a list of realms the bench could not be repaired from.
func requireSafeRealms(realms []string) error {
	for _, r := range realms {
		if strings.TrimSpace(r) == "" {
			return &BenchError{
				Kind:  Unclassified,
				Cause: "a realm name is blank, and Nexus would not take the list as given",
				Check: "give the name of every realm to keep active",
			}
		}
	}
	if !slices.Contains(realms, authenticatingRealm) {
		return &BenchError{
			Kind:  Unclassified,
			Cause: "the list of realms does not hold " + authenticatingRealm + ", so the admin account could not sign in again",
			Check: "keep " + authenticatingRealm + " in the list",
		}
	}
	return nil
}

// SetAnonymousAccess enables or disables anonymous access, keeping the user and
// the realm of the setting as the instance has them: it sends the object the
// provisioning sends, in one PUT.
func SetAnonymousAccess(ctx context.Context, baseURL string, store SecretStore, p Params, enabled bool) error {
	c, err := accessClient(ctx, baseURL, store, p)
	if err != nil {
		return err
	}
	defer c.closeIdle()
	if err := c.setAnonymous(ctx, enabled); err != nil {
		return accessError(ctx, err, p, baseURL)
	}
	return nil
}
