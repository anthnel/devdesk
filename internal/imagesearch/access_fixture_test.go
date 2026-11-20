package imagesearch

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-580 (NX-08b, PR4, NX-05): the verdict DevDesk gives a catalog read by each
// kind of caller, replayed from the answers MEASURED on Nexus 3.98.1-01 (the
// `access` campaign): nominal, with the DockerToken realm removed (W5) and with the
// anonymous access cut (W6). The client-level half (the wire, the Bearer flow, the
// realm) is internal/oci/registry_fixture_test.go; Classify lives here.
//
// Criterion 3 of NX-08b wants the Bearer flow replayed and the realms off the origin
// refused. What is measured: a wrong password gets a BASIC challenge, not a Bearer
// one, so the client never enters the flow and Classify says "refused" (a 401). The
// realm off the origin is not refused (see the oci test and the measurement note).

type accessCase struct {
	name     string
	fixture  string
	host     string
	prefix   string // "" = the group connector, else a member read by path
	id       nexusfixture.Identity
	password string // "" = fakenexus.Password
	target   string // the one request recorded
	status   int
	want     Verdict
	rows     string
}

const (
	accessConnectorTarget = "/v2/_catalog?n=1000"
	accessHostedTarget    = "/v2/docker-hosted/_catalog?n=1000"
)

func accessTarget(c accessCase, base string) Target {
	tg := Target{APIURL: base, RepoPrefix: c.prefix, CatalogUnderPrefix: c.prefix != ""}
	if c.id != nexusfixture.Anonymous {
		tg.Username, tg.Password = fakenexus.User(c.id), fakenexus.Password
		if c.password != "" {
			tg.Password = c.password
		}
	}
	return tg
}

func loadAccess(t *testing.T, names ...string) []nexusfixture.Fixture {
	t.Helper()
	all, err := nexusfixture.Load("access")
	if err != nil {
		t.Fatalf("Load(access): %v", err)
	}
	var out []nexusfixture.Fixture
	for _, n := range names {
		i := slices.IndexFunc(all, func(f nexusfixture.Fixture) bool { return f.Name == n })
		if i < 0 {
			t.Fatalf("the access campaign has no fixture %q", n)
		}
		out = append(out, all[i])
	}
	return out
}

func TestCatalogVerdictsOnMeasuredNexusAccess(t *testing.T) {
	var cases []accessCase
	for _, state := range []struct{ prefix, label string }{{"bearer", "nominal"}, {"w5", "realm removed"}, {"w6", "anonymous cut"}} {
		for _, where := range []struct {
			slug, host, prefix, target string
		}{
			{"catalog-group-connector-30482", "127.0.0.1:30482", "", accessConnectorTarget},
			{"catalog-hosted-path", "127.0.0.1:30481", "docker-hosted", accessHostedTarget},
		} {
			rows := "api,team/api"
			if where.prefix != "" {
				rows = "docker-hosted/api,docker-hosted/team/api"
			}
			add := func(id nexusfixture.Identity, password string, status int, want Verdict, rows string) {
				cases = append(cases, accessCase{
					name:    fmt.Sprintf("%s, %s, %s", state.label, where.slug, id),
					fixture: state.prefix + "-" + where.slug + "--" + string(id), host: where.host, prefix: where.prefix,
					id: id, password: password, target: where.target, status: status, want: want, rows: rows,
				})
			}
			// A wrong password: 401 BASIC in every state, "refused", typed 401 (below).
			add(nexusfixture.BadCredentials, fakenexus.BadPassword, 401, VerdictRefused, "")
			switch state.prefix {
			case "bearer", "w5":
				// The anonymous access is open: 200, no challenge, nothing to follow.
				add(nexusfixture.Anonymous, "", 200, VerdictOK, rows)
			}
			if state.prefix != "bearer" {
				add(nexusfixture.Admin, "", 200, VerdictOK, rows)
			}
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixtures := loadAccess(t, tc.fixture)
			ex := fixtures[0].Exchanges
			if len(ex) != 1 || ex[0].Target != tc.target || ex[0].Status != tc.status {
				t.Fatalf("%s no longer records %s -> %d: the measurement changed", tc.fixture, tc.target, tc.status)
			}
			s := fakenexus.New(t, fixtures...)

			imgs, partial, err := CatalogSearcher{}.Search(context.Background(), accessTarget(tc, s.URL(tc.host)), Query{Text: "api"})
			out := Classify(err, len(imgs), partial)

			if out != (Outcome{Verdict: tc.want}) {
				t.Fatalf("outcome = %+v (err = %v), want verdict %v alone", out, err, tc.want)
			}
			if got := names(imgs); got != tc.rows {
				t.Errorf("rows = %q, want %q", got, tc.rows)
			}
			if tc.want == VerdictRefused {
				var se *oci.StatusError
				if !errors.As(err, &se) || se.Code != 401 || se.Source != oci.SourceRegistry {
					t.Errorf("err = %v (%T), want *oci.StatusError{401, SourceRegistry}", err, err)
				}
				if se.Challenge != `BASIC realm="Sonatype Nexus Repository Manager"` {
					t.Errorf("Challenge = %q: the Basic challenge Nexus sent is not what the error carries", se.Challenge)
				}
			} else if err != nil {
				t.Errorf("err = %v, want nil", err)
			}
			if h := s.Hits(tc.fixture); h != 1 {
				t.Errorf("Hits(%s) = %d, want 1", tc.fixture, h)
			}
		})
	}
}

// With the anonymous access cut (W6) an anonymous catalog read is answered 401 with
// a Bearer challenge. DevDesk follows it: a token request goes to the realm. What
// Nexus answers there was NOT measured (the w6 catalog fixtures have no Bearer flow),
// so no verdict is pinned; what is pinned is that the read does not end in "ok" and
// that the failure is the token endpoint's (a *StatusError with SourceToken), which
// is where the unmeasured part starts.
func TestAnonymousCatalogWithTheAnonymousAccessCutGoesToTheRealm(t *testing.T) {
	for _, tc := range []struct {
		fixture, host, prefix, target, tokenPath string
	}{
		{"w6-catalog-group-connector-30482--anon", "127.0.0.1:30482", "", accessConnectorTarget, "/v2/token"},
		{"w6-catalog-hosted-path--anon", "127.0.0.1:30481", "docker-hosted", accessHostedTarget, "/v2/docker-hosted/token"},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			fixtures := loadAccess(t, tc.fixture)
			if ex := fixtures[0].Exchanges; len(ex) != 1 || ex[0].Target != tc.target || ex[0].Status != 401 {
				t.Fatalf("%s no longer records %s -> 401: the measurement changed", tc.fixture, tc.target)
			}
			captured := &captureTB{TB: t}
			s := fakenexus.New(captured, fixtures...)
			c := accessCase{prefix: tc.prefix, id: nexusfixture.Anonymous}

			imgs, partial, err := CatalogSearcher{}.Search(context.Background(), accessTarget(c, s.URL(tc.host)), Query{Text: "api"})

			if out := Classify(err, len(imgs), partial); out.Verdict == VerdictOK || len(imgs) != 0 {
				t.Fatalf("outcome = %+v with %d rows: an anonymous read of a cut instance cannot be ok", out, len(imgs))
			}
			var se *oci.StatusError
			if !errors.As(err, &se) || se.Source != oci.SourceToken {
				t.Errorf("err = %v (%T), want the token endpoint's *oci.StatusError (SourceToken)", err, err)
			}
			if got := captured.all(); len(got) != 1 {
				t.Errorf("the fake reported %q, want exactly the one unmeasured token request (GET %s)", got, tc.tokenPath)
			}
			if h := s.Hits(tc.fixture); h != 1 {
				t.Errorf("Hits(%s) = %d, want 1", tc.fixture, h)
			}
		})
	}
}

// captureTB lets a test expect the complaints of a fake instead of failing on them.
type captureTB struct {
	testing.TB
	mu   sync.Mutex
	errs []string
}

func (c *captureTB) Helper() {}

func (c *captureTB) Errorf(format string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errs = append(c.errs, fmt.Sprintf(format, args...))
}

func (c *captureTB) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.errs)
}
