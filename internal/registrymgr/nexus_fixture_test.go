package registrymgr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/nexusfixture"
	"github.com/anthnel/devdesk/internal/nexusfixture/fakenexus"
)

// DEVDESK-511 (NX-08, step 5): DetectGroup walks the REST answers MEASURED on
// Nexus 3.98.1-01 (NX-03, DEVDESK-506, criterion 3 of the 2026-11-17 note),
// replayed by fakenexus, for the three bench accounts. The hand-written servers
// of nexus_members_test.go model what a Nexus could say; the verdicts below come
// from what this one did say, read in the fixtures (TestMeasuredRESTAnswersHoldThePremises
// pins the premises so that a re-recorded fixture that changes them fails here,
// not silently in a verdict).
//
// Out of scope, because no measured answer exists: a 5xx or an unreadable reply
// from a real Nexus, and the detailed endpoints of hosted and proxy (measured
// 404 and 403 but never asked by DevDesk: DetectGroup stops at the generic one).

const (
	restHost = "127.0.0.1:30481" // the REST API; hosted and proxy live here too

	restGroup  = "docker-group"
	restHosted = "docker-hosted"
	restProxy  = "docker-proxy"
)

var restIdentities = []nexusfixture.Identity{nexusfixture.Admin, nexusfixture.Pull, nexusfixture.Anonymous}

// restFixtures loads the catalog campaign, which holds the rest-* answers.
func restFixtures(t *testing.T) map[string]nexusfixture.Fixture {
	t.Helper()
	list, err := nexusfixture.Load("catalog")
	if err != nil {
		t.Fatalf("Load(catalog): %v", err)
	}
	byName := make(map[string]nexusfixture.Fixture, len(list))
	for _, f := range list {
		byName[f.Name] = f
	}
	return byName
}

// genericFixture and detailedFixture name the recordings of
// GET /v1/repositories/{name} and GET /v1/repositories/docker/group/{name}.
func genericFixture(repo string, id nexusfixture.Identity) string {
	return "rest-repositories-" + repo + "--" + string(id)
}

func detailedFixture(repo string, id nexusfixture.Identity) string {
	return "rest-repositories-group-" + repo + "--" + string(id)
}

// restExchange returns the one exchange of a fixture, failing the test otherwise.
func restExchange(t *testing.T, fixtures map[string]nexusfixture.Fixture, name string) nexusfixture.Exchange {
	t.Helper()
	f, ok := fixtures[name]
	if !ok {
		t.Fatalf("fixture %s is missing from the catalog campaign", name)
	}
	if len(f.Exchanges) != 1 {
		t.Fatalf("fixture %s has %d exchanges, want 1", name, len(f.Exchanges))
	}
	return f.Exchanges[0]
}

// The premises every verdict below rests on, read from the fixtures.
func TestMeasuredRESTAnswersHoldThePremises(t *testing.T) {
	fixtures := restFixtures(t)

	// Criterion 3: the generic endpoint answers 200 to all three accounts, names
	// the type, and carries no memberNames anywhere (so step 2 of DetectGroup is
	// always reached for a group).
	for _, tc := range []struct{ repo, typ string }{{restGroup, "group"}, {restHosted, "hosted"}, {restProxy, "proxy"}} {
		for _, id := range restIdentities {
			ex := restExchange(t, fixtures, genericFixture(tc.repo, id))
			if ex.Status != http.StatusOK {
				t.Errorf("%s generic as %s: status %d, want 200", tc.repo, id, ex.Status)
			}
			var doc struct {
				Type  string `json:"type"`
				Group any    `json:"group"`
				Attrs struct {
					Group any `json:"group"`
				} `json:"attributes"`
			}
			if err := json.Unmarshal(ex.Body, &doc); err != nil {
				t.Fatalf("%s generic as %s: %v", tc.repo, id, err)
			}
			if doc.Type != tc.typ {
				t.Errorf("%s generic as %s: type %q, want %q", tc.repo, id, doc.Type, tc.typ)
			}
			if doc.Group != nil || doc.Attrs.Group != nil {
				t.Errorf("%s generic as %s carries a group document: DetectGroup would stop at step 1", tc.repo, id)
			}
		}
	}

	// The detailed endpoint of the group: 200 with the members for admin, 403
	// with no body for anonymous, and 404 "Repository not found" (not 403) for the
	// pull account (C1 of the DEVDESK-511 plan, decided by the user).
	admin := restExchange(t, fixtures, detailedFixture(restGroup, nexusfixture.Admin))
	if admin.Status != http.StatusOK {
		t.Errorf("detailed as admin: status %d, want 200", admin.Status)
	}
	if got := memberNamesOf(t, admin.Body); !reflect.DeepEqual(got, []string{restHosted, restProxy}) {
		t.Errorf("detailed as admin: memberNames %v, want [%s %s]", got, restHosted, restProxy)
	}
	anon := restExchange(t, fixtures, detailedFixture(restGroup, nexusfixture.Anonymous))
	if anon.Status != http.StatusForbidden || len(anon.Body) != 0 {
		t.Errorf("detailed as anonymous: %d %q, want 403 with no body", anon.Status, anon.Body)
	}
	pull := restExchange(t, fixtures, detailedFixture(restGroup, nexusfixture.Pull))
	if pull.Status != http.StatusNotFound || !strings.Contains(string(pull.Body), "Repository not found") {
		t.Errorf("detailed as pull: %d %q, want 404 \"Repository not found\"", pull.Status, pull.Body)
	}
	// DEVDESK-578: the body is what tells this 404 from any other one.
	if !isRepositoryNotFound(pull.Body) {
		t.Errorf("isRepositoryNotFound(%q) = false, want true for the measured pull body", pull.Body)
	}
}

func memberNamesOf(t *testing.T, body []byte) []string {
	t.Helper()
	var doc struct {
		Group struct {
			MemberNames []string `json:"memberNames"`
		} `json:"group"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("reading memberNames: %v", err)
	}
	return doc.Group.MemberNames
}

// restInfo is the registry an identity would have configured for a repository
// served by the fake, reached by the path form of a Nexus URL.
func restInfo(s *fakenexus.Server, repo string, id nexusfixture.Identity) RegistryInfo {
	info := RegistryInfo{Provider: ProviderNexus, URL: s.URL(restHost) + "/repository/" + repo}
	if id != nexusfixture.Anonymous {
		info.Username, info.Password = fakenexus.User(id), fakenexus.Password
	}
	return info
}

// wantHits fails unless each named fixture answered exactly the given number of
// requests and nothing else answered any.
func wantHits(t *testing.T, s *fakenexus.Server, fixtures map[string]nexusfixture.Fixture, want map[string]int) {
	t.Helper()
	total, wantTotal := 0, 0
	for name := range fixtures {
		total += s.Hits(name)
	}
	for name, n := range want {
		wantTotal += n
		if got := s.Hits(name); got != n {
			t.Errorf("Hits(%s) = %d, want %d", name, got, n)
		}
	}
	if total != wantTotal {
		t.Errorf("%d requests answered in total, want %d", total, wantTotal)
	}
}

// The group's two members, as the admin's detailed answer lists them (F2: both
// aliases come out as "docker", the -hosted and -proxy suffixes being stripped),
// typed by their own generic answers (DEVDESK-629, F2a).
func measuredMembers(s *fakenexus.Server) []GroupMember {
	return []GroupMember{
		{Alias: "docker", URL: s.URL(restHost), RepoPrefix: restHosted, Type: "hosted"},
		{Alias: "docker", URL: s.URL(restHost), RepoPrefix: restProxy, Type: "proxy"},
	}
}

func TestDetectGroupOnMeasuredNexusAnswers(t *testing.T) {
	fixtures := restFixtures(t)
	all := make([]nexusfixture.Fixture, 0, len(fixtures))
	for _, f := range fixtures {
		all = append(all, f)
	}

	t.Run("group as admin lists its members", func(t *testing.T) {
		s := fakenexus.New(t, all...)
		id := nexusfixture.Admin

		members, err := (&NexusDetector{}).DetectGroup(context.Background(), restInfo(s, restGroup, id))

		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if want := measuredMembers(s); !reflect.DeepEqual(members, want) {
			t.Errorf("members = %+v, want %+v", members, want)
		}
		// F2a: one more request per member, on its own generic endpoint.
		wantHits(t, s, fixtures, map[string]int{
			genericFixture(restGroup, id):  1,
			detailedFixture(restGroup, id): 1,
			genericFixture(restHosted, id): 1,
			genericFixture(restProxy, id):  1,
		})
	})

	t.Run("group as anonymous is refused, 403", func(t *testing.T) {
		s := fakenexus.New(t, all...)
		id := nexusfixture.Anonymous

		members, err := (&NexusDetector{}).DetectGroup(context.Background(), restInfo(s, restGroup, id))

		wantRefused(t, members, err, http.StatusForbidden, true)
		// No credential, so no anonymous retry: one request on the detailed endpoint.
		wantHits(t, s, fixtures, map[string]int{genericFixture(restGroup, id): 1, detailedFixture(restGroup, id): 1})
	})

	// C1 (DEVDESK-511 plan, decided by the user): the pull account does not get
	// the 403 that D161 (NX-07) assumed on the detailed endpoint, it gets 404
	// "Repository not found" (rest-repositories-group-docker-group--pull). That is
	// Nexus hiding the endpoint from an account without nx-repository-admin, not a
	// repository that is missing (the generic endpoint just said it is a group),
	// so since DEVDESK-578 (NX-07b) it is a refusal like the 403: the UI can say
	// "denied" instead of "unreachable". Only this 404 body qualifies; every other
	// 404 stays a failed discovery (see TestNexusMembers404*).
	t.Run("group as the pull account is refused, 404 Repository not found", func(t *testing.T) {
		s := fakenexus.New(t, all...)
		id := nexusfixture.Pull

		members, err := (&NexusDetector{}).DetectGroup(context.Background(), restInfo(s, restGroup, id))

		wantRefused(t, members, err, http.StatusNotFound, false)
		if strings.Contains(err.Error(), fakenexus.Password) {
			t.Errorf("error leaks the password: %v", err)
		}
		// The account's 404, then D156's anonymous retry, which the anonymous
		// recording answers with its 403: the first attempt decides, so the 403 is
		// not what the error reports.
		wantHits(t, s, fixtures, map[string]int{
			genericFixture(restGroup, id):                      1,
			detailedFixture(restGroup, id):                     1,
			detailedFixture(restGroup, nexusfixture.Anonymous): 1,
		})
	})

	// A hosted or a proxy repository says what it is on the generic endpoint, and
	// that is the end of it: not a group, no detailed request.
	for _, repo := range []string{restHosted, restProxy} {
		for _, id := range restIdentities {
			t.Run(repo+" as "+string(id)+" is not a group", func(t *testing.T) {
				s := fakenexus.New(t, all...)

				members, err := (&NexusDetector{}).DetectGroup(context.Background(), restInfo(s, repo, id))

				if err != nil || members != nil {
					t.Fatalf("DetectGroup = (%v, %v), want (nil, nil): not a group is a settled answer", members, err)
				}
				wantHits(t, s, fixtures, map[string]int{genericFixture(repo, id): 1})
			})
		}
	}
}

// DEVDESK-629 (F2a): the type of each member is what its own generic answer
// measured, one request each, and a refused listing asks for no type at all.
func TestDetectGroupTypesItsMembersOnMeasuredAnswers(t *testing.T) {
	fixtures := restFixtures(t)
	all := make([]nexusfixture.Fixture, 0, len(fixtures))
	for _, f := range fixtures {
		all = append(all, f)
	}

	t.Run("admin", func(t *testing.T) {
		s := fakenexus.New(t, all...)
		id := nexusfixture.Admin

		members, err := (&NexusDetector{}).DetectGroup(context.Background(), restInfo(s, restGroup, id))

		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		want := map[string]string{restHosted: "hosted", restProxy: "proxy"}
		if len(members) != len(want) {
			t.Fatalf("members = %+v, want %d", members, len(want))
		}
		for _, m := range members {
			if m.Type != want[m.RepoPrefix] {
				t.Errorf("member %q Type = %q, want %q", m.RepoPrefix, m.Type, want[m.RepoPrefix])
			}
			// The type is the one the recording says, not a guess from the name.
			var doc struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(restExchange(t, fixtures, genericFixture(m.RepoPrefix, id)).Body, &doc); err != nil || doc.Type != m.Type {
				t.Errorf("member %q: recorded type %q (err %v), DetectGroup says %q", m.RepoPrefix, doc.Type, err, m.Type)
			}
		}
		wantHits(t, s, fixtures, map[string]int{
			genericFixture(restGroup, id):  1,
			detailedFixture(restGroup, id): 1,
			genericFixture(restHosted, id): 1,
			genericFixture(restProxy, id):  1,
		})
	})

	for _, id := range []nexusfixture.Identity{nexusfixture.Pull, nexusfixture.Anonymous} {
		t.Run(string(id)+" is refused and asks for no type", func(t *testing.T) {
			s := fakenexus.New(t, all...)

			_, err := (&NexusDetector{}).DetectGroup(context.Background(), restInfo(s, restGroup, id))

			if !errors.Is(err, ErrMembersRefused) {
				t.Fatalf("err = %v, want errors.Is ErrMembersRefused", err)
			}
			for _, repo := range []string{restHosted, restProxy} {
				for _, as := range restIdentities {
					if got := s.Hits(genericFixture(repo, as)); got != 0 {
						t.Errorf("Hits(%s) = %d, want 0: a refused listing has no member to type", genericFixture(repo, as), got)
					}
				}
			}
		})
	}
}
