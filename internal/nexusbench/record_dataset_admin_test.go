package nexusbench

import (
	"net/http"
	"strings"
	"testing"
)

// DEVDESK-580, PR4 (W6): the recorder compares the data set of the bench with the
// descriptor before the first fixture request, and it lists the repositories
// anonymously. A bench whose anonymous access is cut (the point of the access
// scenarios) therefore cannot be recorded: the listing is a 401 and the run is
// refused. RecordOptions.DatasetAsAdmin (the command's --dataset-as-admin) reads
// the same two listings with the admin account instead. Without it nothing changes.
//
// What the tests fix:
//   - with the option, the two listings (docker-hosted and docker-proxy, never
//     docker-group) are sent with the admin Basic credentials, to the REST origin
//     only, and none is sent anonymously; the comparison is the same one, so a
//     drift is the same refusal with the same advice (reset);
//   - the admin listing, not the anonymous one, is what is compared: an anonymous
//     answer that drifted or is refused does not matter;
//   - the admin password is checked before the first request (a run with the
//     option and no stored admin password is refused with `up`), a listing the
//     admin account is refused is a refusal that names neither password, and its
//     advice does not blame anonymous access;
//   - with no job to run, no listing is read at all, option or not.

// cutAnonymousAccess makes the anonymous listings of the data set a 401, as a bench
// whose anonymous access is disabled answers them, and serves the admin ones.
func cutAnonymousAccess(e *recEnv) {
	for _, repo := range DatasetRepositories() {
		e.rest.answer("", componentsPath+"?repository="+repo, recAnswer{status: http.StatusUnauthorized})
		e.rest.serveListingFor("admin", repo, recListing(repo))
	}
}

// listingRequests are the requests of the data set check the REST origin received.
func listingRequests(e *recEnv) []recSeen {
	var out []recSeen
	for _, s := range e.rest.requests() {
		if strings.HasPrefix(s.uri, componentsPath) {
			out = append(out, s)
		}
	}
	return out
}

func TestRecordDatasetAsAdminListsWithTheAdminAccount(t *testing.T) {
	for name, arrange := range map[string]func(e *recEnv){
		"anonymous access cut": cutAnonymousAccess,
		"anonymous listing drifted, admin listing right": func(e *recEnv) {
			cutAnonymousAccess(e)
			drifted := recListing(dockerHostedRepository)
			drifted[0].Digest = "sha256:" + strings.Repeat("0", 64)
			e.rest.serveListing(dockerHostedRepository, drifted)
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newRecEnv(t)
			arrange(e)
			e.addGood()
			e.opts.DatasetAsAdmin = true

			rep, err := e.record()
			if err != nil {
				t.Fatalf("RecordFixtures with DatasetAsAdmin: %v", err)
			}
			if len(rep.Written) != 1 {
				t.Errorf("Written = %v, want the one fixture", rep.Written)
			}
			listed := map[string]bool{}
			for _, s := range listingRequests(e) {
				if s.method != "GET" || s.user != "admin" {
					t.Errorf("listing %s %s sent as %q, want a GET as admin", s.method, s.uri, s.user)
				}
				listed[s.uri] = true
			}
			for _, repo := range DatasetRepositories() {
				if !listed[componentsPath+"?repository="+repo] {
					t.Errorf("%s was not listed: %v", repo, listed)
				}
			}
			if len(listed) != len(DatasetRepositories()) {
				t.Errorf("listed %v, want docker-hosted and docker-proxy only (a read through docker-group can cache an image)", listed)
			}
			// The admin credentials go to the REST origin, and the Docker origin
			// only gets the fixtures.
			for _, s := range e.docker.requests() {
				if strings.HasPrefix(s.uri, componentsPath) {
					t.Errorf("the Docker origin received a listing: %s", s.uri)
				}
			}
		})
	}
}

// Without the option nothing changes: the listing is anonymous, and a bench that
// refuses it is refused, with the advice that says anonymous access is to be on.
func TestRecordDatasetStaysAnonymousWithoutTheOption(t *testing.T) {
	e := newRecEnv(t)
	cutAnonymousAccess(e)
	e.addGood()

	be := e.refuse("")
	if !strings.Contains(be.Check, "anonymous access is on") {
		t.Errorf("Check = %q, want the advice about anonymous access", be.Check)
	}
	e.requireNoFixtureRequest()
	for _, s := range listingRequests(e) {
		if s.user != "" {
			t.Errorf("a listing was sent as %q without the option: %s", s.user, s.uri)
		}
	}
}

func TestRecordDatasetAnonymousListingIsNotSentWithTheAdminOptionOnABenchThatStillAllowsIt(t *testing.T) {
	e := newRecEnv(t) // the anonymous listings are right, as on a nominal bench
	for _, repo := range DatasetRepositories() {
		e.rest.serveListingFor("admin", repo, recListing(repo))
	}
	e.addGood()
	e.opts.DatasetAsAdmin = true
	if _, err := e.record(); err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	for _, s := range listingRequests(e) {
		if s.user != "admin" {
			t.Errorf("listing %s sent as %q, want admin: the option is not a fallback", s.uri, s.user)
		}
	}
}

// The comparison itself is unchanged: a drifted admin listing is the same refusal,
// with the same advice, before any fixture request.
func TestRecordDatasetAsAdminRefusesADataSetThatDiffers(t *testing.T) {
	zero := "sha256:" + strings.Repeat("0", 64)
	tests := []struct {
		name    string
		arrange func(e *recEnv)
		reset   bool
	}{
		{"a hosted digest changed", func(e *recEnv) {
			l := recListing(dockerHostedRepository)
			l[0].Digest = zero
			e.rest.serveListingFor("admin", dockerHostedRepository, l)
		}, true},
		{"the proxy caches an image the data set does not", func(e *recEnv) {
			e.rest.serveListingFor("admin", dockerProxyRepository, append(recListing(dockerProxyRepository), ListedImage{Name: "shared/base", Tag: "1.0", Digest: zero}))
		}, true},
		{"a hosted image is missing", func(e *recEnv) {
			e.rest.serveListingFor("admin", dockerHostedRepository, recListing(dockerHostedRepository)[1:])
		}, true},
		{"the listing carries no digest", func(e *recEnv) {
			l := recListing(dockerHostedRepository)
			l[0].Digest = ""
			e.rest.serveListingFor("admin", dockerHostedRepository, l)
		}, false},
		{"the admin listing is refused", func(e *recEnv) {
			e.rest.answer("admin", componentsPath+"?repository="+dockerHostedRepository, recAnswer{status: http.StatusForbidden})
		}, false},
		{"the descriptor lists no data set", func(e *recEnv) { e.opts.Descriptor.Dataset = nil }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newRecEnv(t)
			cutAnonymousAccess(e)
			e.addGood()
			e.opts.DatasetAsAdmin = true
			tt.arrange(e)

			be := e.refuse("")
			if tt.reset && !strings.Contains(be.Check, "mise run nexus-bench:reset") {
				t.Errorf("Check = %q, want it to name `mise run nexus-bench:reset`", be.Check)
			}
			if strings.Contains(be.Check, "anonymous access is on") {
				t.Errorf("Check = %q blames anonymous access, which this run does not use", be.Check)
			}
			e.requireNoFixtureRequest()
		})
	}
}

// A wrong admin password is the bench refusing the listing: the run stops before
// any fixture request, and neither the stored password nor its Basic encoding is
// in the error.
func TestRecordDatasetAsAdminWithAWrongPasswordLeaksNothing(t *testing.T) {
	const wrong = "rec-wrong-Hq3bV9nD"
	e := newRecEnv(t)
	cutAnonymousAccess(e)
	e.addGood()
	e.opts.DatasetAsAdmin = true
	e.opts.Secrets.AdminPassword = wrong

	_, err := e.record()
	_ = requireRecordRefusal(t, err)
	for _, secret := range []string{wrong, recBasic("admin", wrong)} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the error carries the password: %q", err)
		}
	}
	// The refusal is the bench's answer to the admin listing, not the anonymous 401.
	if got := listingRequests(e); len(got) == 0 || got[0].user != "admin" {
		t.Errorf("the first listing was %+v, want one sent as admin", got)
	}
	e.requireNoFixtureRequest()
}

// The admin password is needed for the listing even when every fixture is
// anonymous (planFixture asks for a password per identity, so this is the option's
// own check), and it is checked before anything is sent.
func TestRecordDatasetAsAdminNeedsTheAdminPasswordBeforeAnyRequest(t *testing.T) {
	e := newRecEnv(t)
	cutAnonymousAccess(e)
	e.rest.answer("", recGoodTarget, recAnswer{contentType: "application/json", body: `[]`})
	e.pending("a-anon--anon", recRESTHost, recGoodTarget)
	e.opts.DatasetAsAdmin = true
	e.opts.Secrets.AdminPassword = ""

	be := e.refuse("")
	if !strings.Contains(be.Check, "mise run nexus-bench:up") {
		t.Errorf("Check = %q, want it to name `mise run nexus-bench:up`", be.Check)
	}
	e.requireNoRequest()
}

// A run that has nothing to record compares nothing, option or not.
func TestRecordDatasetAsAdminWithNoJobReadsNoListing(t *testing.T) {
	e := newRecEnv(t)
	e.opts.DatasetAsAdmin = true
	rep, err := e.record()
	if err != nil {
		t.Fatalf("RecordFixtures: %v", err)
	}
	if len(rep.Written) != 0 {
		t.Errorf("Written = %v, want none", rep.Written)
	}
	e.requireNoRequest()
}
