package nexusbench

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// DEVDESK-580, PR4 (W5 and W6): the access scenarios record the fixtures of a bench
// whose Docker Bearer Token realm is off, then of one whose anonymous access is
// off. The library gives the scenario test three bricks, all reserved to the bench:
//
//	ActiveRealms(ctx, baseURL, store, p) ([]string, error)
//	SetActiveRealms(ctx, baseURL, store, p, realms) error
//	SetAnonymousAccess(ctx, baseURL, store, p, enabled) error
//
// (the scenario test itself, TestIntegrationDevDeskAccessVariants, is guarded by
// DEVDESK_NEXUS_BENCH_SCENARIOS). They are tested against the in-memory Nexus of fakenexus_test.go,
// with the guards of the other writes of the package (RestoreUpstream,
// UnblockProxy):
//   - a REST origin that is not the loopback is refused, before the stored
//     credentials are read and before anything is sent;
//   - the credentials are the stored admin ones (a bench with none stored is a
//     refusal that says to run `up`), and every request carries the admin account;
//   - the payload is exactly the setting asked for: for the realms the list as
//     given, in its order; for anonymous access the whole object the provisioning
//     itself sends ("enabled", "userId", "realmName", the realm kept as the
//     instance has it), one PUT each, and no other write;
//   - a list of realms that would lock the bench out (none, or without
//     NexusAuthenticatingRealm, or with a blank name) is refused before any request;
//   - every error is a *BenchError, a cancelled context excepted (returned as is),
//     and no error carries a password;
//   - Provision, which the scenario runs to put the bench back, repairs both
//     settings (the test at the end of the file chains them).

// ---- doubles ----------------------------------------------------------------

// loadCounter counts the reads of the secrets: a refused origin must not even read
// them.
type loadCounter struct {
	SecretStore
	loads int
}

func (s *loadCounter) Load() (Secrets, bool, error) {
	s.loads++
	return s.SecretStore.Load()
}

// accessCall is one brick, run against a base URL with a store.
type accessCall struct {
	name string
	run  func(ctx context.Context, baseURL string, store SecretStore, p Params) error
}

func accessCalls() []accessCall {
	return []accessCall{
		{"ActiveRealms", func(ctx context.Context, baseURL string, store SecretStore, p Params) error {
			_, err := ActiveRealms(ctx, baseURL, store, p)
			return err
		}},
		{"SetActiveRealms", func(ctx context.Context, baseURL string, store SecretStore, p Params) error {
			return SetActiveRealms(ctx, baseURL, store, p, []string{"NexusAuthenticatingRealm"})
		}},
		{"SetAnonymousAccess", func(ctx context.Context, baseURL string, store SecretStore, p Params) error {
			return SetAnonymousAccess(ctx, baseURL, store, p, false)
		}},
	}
}

// writesFrom is "METHOD path" of every request that is not a GET, from n on.
func writesFrom(b *bench, n int) []string {
	var out []string
	for _, r := range b.requestsFrom(n) {
		if r.Method != http.MethodGet {
			out = append(out, r.Method+" "+r.Path)
		}
	}
	return out
}

// ---- what they send ---------------------------------------------------------

func TestActiveRealmsReadsWhatTheInstanceLists(t *testing.T) {
	b := provisionedBench(t)
	first := len(b.nexus.Requests())
	got, err := ActiveRealms(context.Background(), b.nexus.URL(), b.store, b.p)
	if err != nil {
		t.Fatalf("ActiveRealms: %v", err)
	}
	if want := []string{"NexusAuthenticatingRealm", "DockerToken"}; !slices.Equal(got, want) {
		t.Errorf("ActiveRealms = %v, want %v", got, want)
	}
	if w := writesFrom(b, first); len(w) != 0 {
		t.Errorf("a read wrote: %v", w)
	}
	for _, r := range b.requestsFrom(first) {
		if r.User != adminUserID {
			t.Errorf("%s %s was sent as %q, want admin", r.Method, r.Path, r.User)
		}
	}
}

func TestSetActiveRealmsPutsExactlyTheListItIsGiven(t *testing.T) {
	tests := []struct {
		name   string
		realms []string
	}{
		{"DockerToken removed", []string{"NexusAuthenticatingRealm"}},
		{"put back, in the order given", []string{"NexusAuthenticatingRealm", "DockerToken"}},
		{"another order is kept", []string{"DockerToken", "NexusAuthenticatingRealm"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := provisionedBench(t)
			first := len(b.nexus.Requests())
			if err := SetActiveRealms(context.Background(), b.nexus.URL(), b.store, b.p, tt.realms); err != nil {
				t.Fatalf("SetActiveRealms: %v", err)
			}
			if got := b.nexus.Realms(); !slices.Equal(got, tt.realms) {
				t.Errorf("active realms = %v, want %v", got, tt.realms)
			}
			if w := writesFrom(b, first); !slices.Equal(w, []string{"PUT " + pathRealms}) {
				t.Errorf("writes = %v, want one PUT %s and nothing else", w, pathRealms)
			}
			for _, r := range b.requestsFrom(first) {
				if r.User != adminUserID || r.EmptyPassword {
					t.Errorf("%s %s sent as %q (empty password %v), want the admin account", r.Method, r.Path, r.User, r.EmptyPassword)
				}
				if r.Method == http.MethodPut {
					var sent []string
					if err := json.Unmarshal([]byte(r.Body), &sent); err != nil || !slices.Equal(sent, tt.realms) {
						t.Errorf("PUT body = %q, want the JSON array %v", r.Body, tt.realms)
					}
					if strings.TrimSpace(r.Body) != r.Body || strings.Contains(r.Body, " ") {
						t.Errorf("PUT body = %q, want compact JSON with nothing added", r.Body)
					}
				}
			}
		})
	}
}

// A list that would lock the admin out, or that the instance would take as nothing,
// is refused before the first request: the bench could not be repaired by Provision,
// which signs in as admin.
func TestSetActiveRealmsRefusesWhatWouldLockTheBenchOut(t *testing.T) {
	tests := []struct {
		name   string
		realms []string
	}{
		{"nil", nil},
		{"empty", []string{}},
		{"without the authenticating realm", []string{"DockerToken"}},
		{"another realm only", []string{"NexusAuthorizingRealm"}},
		{"a blank name", []string{"NexusAuthenticatingRealm", ""}},
		{"a name of spaces", []string{"NexusAuthenticatingRealm", "  "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := provisionedBench(t)
			first := len(b.nexus.Requests())
			before := b.nexus.Snapshot()
			err := SetActiveRealms(context.Background(), b.nexus.URL(), b.store, b.p, tt.realms)
			_ = requireAccessRefusal(t, b, err)
			if got := len(b.nexus.Requests()) - first; got != 0 {
				t.Errorf("%d request(s) were sent before the refusal, want none", got)
			}
			if b.nexus.Snapshot() != before {
				t.Error("the bench changed")
			}
		})
	}
}

func TestSetAnonymousAccessSendsTheWholeSettingTheProvisioningSends(t *testing.T) {
	tests := []struct {
		name    string
		realm   string // the realm of the setting on the instance
		enabled bool
	}{
		{"cut", defaultAnonymousRealm, false},
		{"back on", defaultAnonymousRealm, true},
		{"cut, a custom realm is kept", "CustomRealm", false},
		{"on, a custom realm is kept", "CustomRealm", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := provisionedBench(t)
			b.nexus.MutateAnonymous(func(m map[string]any) { m["enabled"] = !tt.enabled; m["realmName"] = tt.realm })
			first := len(b.nexus.Requests())
			if err := SetAnonymousAccess(context.Background(), b.nexus.URL(), b.store, b.p, tt.enabled); err != nil {
				t.Fatalf("SetAnonymousAccess: %v", err)
			}
			want := map[string]any{"enabled": tt.enabled, "userId": "anonymous", "realmName": tt.realm}
			if got := b.nexus.Anonymous(); !reflect.DeepEqual(got, want) {
				t.Errorf("the setting is %v, want %v", got, want)
			}
			if w := writesFrom(b, first); !slices.Equal(w, []string{"PUT " + pathAnonymous}) {
				t.Errorf("writes = %v, want one PUT %s and nothing else", w, pathAnonymous)
			}
			for _, r := range b.requestsFrom(first) {
				if r.User != adminUserID || r.EmptyPassword {
					t.Errorf("%s %s sent as %q, want the admin account", r.Method, r.Path, r.User)
				}
				if r.Method == http.MethodPut {
					sent, ok := decodeObject([]byte(r.Body))
					if !ok || !reflect.DeepEqual(sent, want) {
						t.Errorf("PUT body = %q, want exactly %v", r.Body, want)
					}
				}
			}
		})
	}
}

// Nothing but the one setting changes: the rest of the instance is as it was.
func TestAccessSettersChangeOnlyTheirSetting(t *testing.T) {
	b := provisionedBench(t)
	before := b.nexus.Snapshot()
	ctx := context.Background()
	if err := SetActiveRealms(ctx, b.nexus.URL(), b.store, b.p, []string{"NexusAuthenticatingRealm"}); err != nil {
		t.Fatal(err)
	}
	afterRealms := b.nexus.Snapshot()
	if afterRealms == before {
		t.Fatal("SetActiveRealms changed nothing")
	}
	if err := SetAnonymousAccess(ctx, b.nexus.URL(), b.store, b.p, false); err != nil {
		t.Fatal(err)
	}
	// Putting the two settings back by hand gives the first snapshot again: no other
	// field moved.
	b.nexus.SetRealms("NexusAuthenticatingRealm", "DockerToken")
	b.nexus.SetAnonymous(true)
	if got := b.nexus.Snapshot(); got != before {
		t.Errorf("something else changed:\n want: %s\n got:  %s", before, got)
	}
}

// ---- the guards ---------------------------------------------------------------

func TestAccessBricksRefuseAnOriginThatIsNotTheLoopback(t *testing.T) {
	origins := []string{
		"http://10.0.0.5:30481",
		"http://datomir.home.arpa:30481",
		"http://localhost:30481",
		"http://127.0.0.2:30481",
		"http://[::ffff:127.0.0.1]:30481",
		"http://127.0.0.1@evil.example:30481",
		"http://127.0.0.1.evil.example:30481",
		"http://user:pw@127.0.0.1:30481",
		"",
	}
	for _, call := range accessCalls() {
		for _, origin := range origins {
			t.Run(call.name+" "+origin, func(t *testing.T) {
				b := provisionedBench(t)
				store := &loadCounter{SecretStore: b.store}
				err := call.run(context.Background(), origin, store, b.p)
				_ = requireAccessRefusal(t, b, err)
				if store.loads != 0 {
					t.Errorf("the stored credentials were read %d time(s) for an origin that is refused", store.loads)
				}
			})
		}
	}
}

func TestAccessBricksNeedTheStoredAdminPassword(t *testing.T) {
	stores := map[string]func(b *bench) SecretStore{
		"nothing stored": func(*bench) SecretStore { return &memStore{log: &orderLog{}} },
		"a pull password only": func(*bench) SecretStore {
			s := &memStore{log: &orderLog{}}
			s.preset(Secrets{PullPassword: secretMarker + "-pull-only"})
			return s
		},
	}
	for _, call := range accessCalls() {
		for name, mk := range stores {
			t.Run(call.name+" "+name, func(t *testing.T) {
				b := provisionedBench(t)
				first := len(b.nexus.Requests())
				err := call.run(context.Background(), b.nexus.URL(), mk(b), b.p)
				be := requireAccessRefusal(t, b, err)
				if !strings.Contains(be.Check, "mise run nexus-bench:up") {
					t.Errorf("Check = %q, want it to name `mise run nexus-bench:up`", be.Check)
				}
				if got := len(b.nexus.Requests()) - first; got != 0 {
					t.Errorf("%d request(s) were sent with no credential to send", got)
				}
			})
		}
	}
}

func TestAccessBricksReturnACancelledContextAsIs(t *testing.T) {
	for _, call := range accessCalls() {
		t.Run(call.name, func(t *testing.T) {
			b := provisionedBench(t)
			first := len(b.nexus.Requests())
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := call.run(ctx, b.nexus.URL(), b.store, b.p)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("error = %v, want context.Canceled", err)
			}
			if got := len(b.nexus.Requests()) - first; got != 0 {
				t.Errorf("%d request(s) were sent on a cancelled context", got)
			}
		})
	}
}

// Whatever the instance does, the error is a *BenchError that carries no password,
// neither the stored one nor its Basic encoding, and the status stays reachable.
func TestAccessBricksNeverLeakAPasswordInAnError(t *testing.T) {
	faults := []struct {
		name    string
		onWrite bool // the fault is on the PUT: a read does not meet it
		arrange func(b *bench)
		status  int // reachable as an *APIError, 0 = none
	}{
		{"the PUT is refused with 403", true, func(b *bench) { b.nexus.Forbid(http.MethodPut, "/service/rest/v1/security", http.StatusForbidden) }, http.StatusForbidden},
		{"the PUT fails with 500", true, func(b *bench) {
			b.nexus.Forbid(http.MethodPut, "/service/rest/v1/security", http.StatusInternalServerError)
		}, http.StatusInternalServerError},
		{"the PUT answers 400", true, func(b *bench) { b.nexus.Forbid(http.MethodPut, "/service/rest/v1/security", http.StatusBadRequest) }, http.StatusBadRequest},
		{"the PUT is redirected, not followed", true, func(b *bench) {
			b.nexus.RedirectTo(http.MethodPut, "/service/rest/v1/security", http.StatusTemporaryRedirect, "http://127.0.0.1:1/elsewhere")
		}, http.StatusTemporaryRedirect},
		{"the stored password is wrong", false, func(b *bench) {
			b.store.preset(Secrets{AdminPassword: secretMarker + "-wrong-admin", PullPassword: b.store.current().PullPassword})
		}, http.StatusUnauthorized},
		{"the connection drops", false, func(b *bench) { b.nexus.DropConnections() }, 0},
	}
	for _, call := range accessCalls() {
		for _, ft := range faults {
			if ft.onWrite && call.name == "ActiveRealms" {
				continue
			}
			t.Run(call.name+" "+ft.name, func(t *testing.T) {
				b := provisionedBench(t)
				ft.arrange(b)
				err := call.run(context.Background(), b.nexus.URL(), b.store, b.p)
				if err == nil {
					t.Fatal("no error")
				}
				var be *BenchError
				if !errors.As(err, &be) {
					t.Fatalf("error %T is not a *BenchError: %v", err, err)
				}
				secrets := append(b.knownSecrets(), secretMarker+"-wrong-admin")
				for _, s := range slices.Clone(secrets) {
					secrets = append(secrets, recBasic(adminUserID, s))
				}
				for _, text := range []string{err.Error(), be.Cause, be.Check} {
					for _, s := range secrets {
						if strings.Contains(text, s) {
							t.Errorf("the error carries a password (%d characters): %q", len(s), text)
						}
					}
				}
				var api *APIError
				switch {
				case ft.status == 0:
				case !errors.As(err, &api):
					t.Errorf("no *APIError behind %v, want status %d", err, ft.status)
				case api.Status != ft.status:
					t.Errorf("status = %d, want %d", api.Status, ft.status)
				}
			})
		}
	}
}

// ---- the way back: Provision ----------------------------------------------------

// The scenario switches a setting, records, then runs Provision. Provision adds the
// realms it requires back (it never removes one) and sets the anonymous access to
// what Params says (on by default), so both bricks are undone by it: the bench is
// the one it was, bit for bit.
func TestProvisionPutsBackWhatTheAccessBricksChanged(t *testing.T) {
	b := provisionedBench(t)
	baseline := b.nexus.Snapshot()
	ctx := context.Background()
	if err := SetActiveRealms(ctx, b.nexus.URL(), b.store, b.p, []string{"NexusAuthenticatingRealm"}); err != nil {
		t.Fatalf("SetActiveRealms: %v", err)
	}
	if err := SetAnonymousAccess(ctx, b.nexus.URL(), b.store, b.p, false); err != nil {
		t.Fatalf("SetAnonymousAccess: %v", err)
	}
	if b.nexus.Snapshot() == baseline {
		t.Fatal("the bricks changed nothing, so there is nothing for Provision to put back")
	}
	if got := b.nexus.Anonymous()["enabled"]; got != false {
		t.Fatalf("anonymous access is %v after the cut, want false", got)
	}
	b.mustRun()
	if got, want := b.nexus.Realms(), []string{"NexusAuthenticatingRealm", "DockerToken"}; !slices.Equal(got, want) {
		t.Errorf("realms after Provision = %v, want %v", got, want)
	}
	if got := b.nexus.Anonymous()["enabled"]; got != true {
		t.Errorf("anonymous access after Provision = %v, want true", got)
	}
	if after := b.nexus.Snapshot(); after != baseline {
		t.Errorf("the bench is not as it was:\n want: %s\n got:  %s", baseline, after)
	}
}

// ---- helpers --------------------------------------------------------------------

// requireAccessRefusal checks an error of a brick that refused to act: a
// *BenchError with a cause and what to do, and no password.
func requireAccessRefusal(t *testing.T, b *bench, err error) *BenchError {
	t.Helper()
	if err == nil {
		t.Fatal("no error, want a refusal")
	}
	var be *BenchError
	if !errors.As(err, &be) {
		t.Fatalf("error %T is not a *BenchError: %v", err, err)
	}
	if be.Cause == "" || be.Check == "" {
		t.Errorf("the BenchError is incomplete: cause %q, check %q", be.Cause, be.Check)
	}
	for _, s := range b.knownSecrets() {
		if strings.Contains(err.Error(), s) {
			t.Errorf("the error carries a password (%d characters): %q", len(s), err)
		}
	}
	return be
}
