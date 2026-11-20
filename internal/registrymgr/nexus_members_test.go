package registrymgr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/oci"
)

// DEVDESK-510 (NX-07): a group whose members endpoint is refused is not an empty
// group. The format endpoint answers one of three ways: a settled listing
// (possibly empty), a refusal (401/403, typed as ErrMembersRefused), or a failed
// discovery (everything else), and only the first may be read as "N members".

const nexusFormatSegment = "/group/"

// membersServer starts a Nexus whose generic endpoint says "group, no members"
// and whose format endpoint is the given handler. formatHits counts the requests
// the format endpoint received and formatAuths records their Authorization.
func membersServer(t *testing.T, format http.HandlerFunc) (info RegistryInfo, formatHits *atomic.Int32) {
	t.Helper()
	formatHits = &atomic.Int32{}
	info, _ = nexusServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, nexusFormatSegment) {
			_, _ = io.WriteString(w, nexusGroupNoMembers)
			return
		}
		formatHits.Add(1)
		format(w, r)
	})
	return info, formatHits
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
}

// wantRefused fails unless err is a members refusal with the given fields, whose
// cause is the management API's status error, and which leaks nothing.
func wantRefused(t *testing.T, members []GroupMember, err error, code int, anonymous bool) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil (members = %v), want a refusal: a refused listing is not an empty group", members)
	}
	if members != nil {
		t.Errorf("members = %v, want nil on a refusal", members)
	}
	if !errors.Is(err, ErrMembersRefused) {
		t.Errorf("err = %v, want errors.Is ErrMembersRefused", err)
	}
	var refused *MembersRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v (%T), want errors.As *MembersRefusedError", err, err)
	}
	if refused.Group != "docker-group" || refused.Status != code || refused.Anonymous != anonymous {
		t.Errorf("refusal = {Group %q, Status %d, Anonymous %v}, want {docker-group, %d, %v}",
			refused.Group, refused.Status, refused.Anonymous, code, anonymous)
	}
	var se *oci.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want errors.As *oci.StatusError (the cause)", err)
	}
	if se.Code != code || se.Source != oci.SourceManagementAPI {
		t.Errorf("cause = {Code %d, Source %v}, want {%d, management API}", se.Code, se.Source, code)
	}
	if refused.Cause == nil || !errors.As(refused.Cause, &se) {
		t.Errorf("Cause = %v, want the *oci.StatusError", refused.Cause)
	}
	wantNoSecret(t, "error", err.Error())
	if strings.Contains(err.Error(), "http://") {
		t.Errorf("error carries a URL: %v", err)
	}
}

// wantFailedDiscovery fails unless err is a non-nil error that is not a refusal.
func wantFailedDiscovery(t *testing.T, members []GroupMember, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil (members = %v), want a failed discovery", members)
	}
	if members != nil {
		t.Errorf("members = %v, want nil on a failed discovery", members)
	}
	if errors.Is(err, ErrMembersRefused) {
		t.Errorf("err = %v, is ErrMembersRefused, want a plain failure (not a refusal)", err)
	}
	var refused *MembersRefusedError
	if errors.As(err, &refused) {
		t.Errorf("err = %v, is a *MembersRefusedError, want a plain failure", err)
	}
	wantNoSecret(t, "error", err.Error())
}

// CA1. The generic endpoint says "group" and the format endpoint refuses the
// account: a refusal, not "0 members". 401 and 403 alike.
func TestNexusRefusedMembersAreARefusalNotAnEmptyGroup(t *testing.T) {
	for _, code := range []int{http.StatusForbidden, http.StatusUnauthorized} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			info, formatHits := membersServer(t, status(code))

			members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

			wantRefused(t, members, err, code, false)
			if got := formatHits.Load(); got != 2 {
				t.Errorf("the format endpoint received %d requests, want 2 (account, then the anonymous retry of D156)", got)
			}
		})
	}
}

// CA1, anonymous. A registry with no credential is refused on the format
// endpoint: a refusal flagged Anonymous, and no retry (there is nothing to drop).
func TestNexusRefusedMembersWithoutCredentialIsAnAnonymousRefusal(t *testing.T) {
	for _, code := range []int{http.StatusForbidden, http.StatusUnauthorized} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			info, formatHits := membersServer(t, status(code))
			info.Username, info.Password = "", ""

			members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

			wantRefused(t, members, err, code, true)
			if got := formatHits.Load(); got != 1 {
				t.Errorf("the format endpoint received %d requests, want exactly 1", got)
			}
		})
	}
}

// D156 followed by a 403 for the anonymous request too: the account was refused
// first and that is the verdict (Anonymous false), whatever the retry got.
func TestNexusRefusedAccountThenRefusedAnonymousRetryIsAnAccountRefusal(t *testing.T) {
	info, formatHits := membersServer(t, status(http.StatusForbidden))

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	wantRefused(t, members, err, http.StatusForbidden, false)
	if got := formatHits.Load(); got != 2 {
		t.Errorf("the format endpoint received %d requests, want 2", got)
	}
}

// The account is refused (403), the anonymous retry is answered by something
// else (500): the refusal is still the verdict, the retry's error is not returned.
func TestNexusRefusedAccountWhoseAnonymousRetryFailsIsStillARefusal(t *testing.T) {
	info, _ := membersServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	wantRefused(t, members, err, http.StatusForbidden, false)
}

// A 403 on the generic endpoint does not say the repository is a group: it stays a
// failed discovery, not a members refusal (pinned so it does not drift).
func TestNexusRefusedGenericEndpointIsNotAMembersRefusal(t *testing.T) {
	info, _ := nexusServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	wantFailedDiscovery(t, members, err)
	var se *oci.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusForbidden {
		t.Errorf("err = %v, want errors.As *oci.StatusError with code 403", err)
	}
}

// CA2. A group that Nexus lists with no member is a settled answer: no error and
// a non-nil empty list (the caller caches it).
func TestNexusGroupWithNoMemberIsAnEmptyListingNotAnError(t *testing.T) {
	info, _ := membersServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"group":{"memberNames":[]}}`)
	})

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	if err != nil {
		t.Fatalf("err = %v, want nil (0 members is a settled answer)", err)
	}
	if members == nil || len(members) != 0 {
		t.Errorf("members = %#v, want a non-nil empty list", members)
	}
}

// Non-regression: a listing with members is still read.
func TestNexusFormatEndpointListingStillGivesItsMembers(t *testing.T) {
	info, _ := membersServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"group":{"memberNames":["a-proxy","b-hosted"]}}`)
	})

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(members) != 2 || members[0].RepoPrefix != "a-proxy" || members[1].RepoPrefix != "b-hosted" {
		t.Errorf("members = %+v, want a-proxy and b-hosted", members)
	}
}

// CA3. A deadline on the format endpoint is a failed discovery, never a refusal.
func TestNexusMembersDeadlineIsNotARefusal(t *testing.T) {
	info, _ := membersServer(t, func(_ http.ResponseWriter, r *http.Request) { blockUntilGone(t, r) })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	members, err := (&NexusDetector{}).DetectGroup(ctx, info)

	wantContextError(t, err, context.DeadlineExceeded)
	wantFailedDiscovery(t, members, err)
}

// CA3. A connection cut on every attempt is a network error: not a refusal and
// not a status.
func TestNexusMembersConnectionCutIsNotARefusal(t *testing.T) {
	info, _ := membersServer(t, func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
	})

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	wantFailedDiscovery(t, members, err)
	var se *oci.StatusError
	if errors.As(err, &se) {
		t.Errorf("err = %v, is a *oci.StatusError, want a network error", err)
	}
}

// CA3. Any status other than 401/403 is a failed discovery that keeps its code.
//
// The 404 here has an empty body: that is not what a Nexus says to a pull account
// (that one carries {"message":"\"Repository not found\""} and is a refusal since
// DEVDESK-578, see TestNexusMembers404*). A bare 404 says nothing about the
// account, so it stays a failure.
func TestNexusMembersOtherStatusIsNotARefusal(t *testing.T) {
	for _, code := range []int{http.StatusNotFound, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			info, _ := membersServer(t, status(code))

			members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

			wantFailedDiscovery(t, members, err)
			var se *oci.StatusError
			if !errors.As(err, &se) || se.Code != code {
				t.Errorf("err = %v, want errors.As *oci.StatusError with code %d", err, code)
			}
		})
	}
}

// CA3. A redirect to another origin on the format endpoint is a failed discovery
// that keeps oci.ErrOffOriginRedirect, not a refusal.
func TestNexusMembersOffOriginRedirectIsNotARefusal(t *testing.T) {
	forEachOffOriginRedirect(t, func(t *testing.T, kind string, code int) {
		env := startNexus(t, kind, func(w http.ResponseWriter, r *http.Request, other string) {
			if !strings.Contains(r.URL.Path, nexusFormatSegment) {
				_, _ = io.WriteString(w, nexusGroupNoMembers)
				return
			}
			http.Redirect(w, r, other+nexusLeakPath+"?"+nexusSigned, code)
		})

		members, err := (&NexusDetector{}).DetectGroup(context.Background(), env.info())

		wantFailedDiscovery(t, members, err)
		if !errors.Is(err, oci.ErrOffOriginRedirect) {
			t.Errorf("err = %v, want errors.Is oci.ErrOffOriginRedirect", err)
		}
	})
}

// CA3. A 200 reply that is not a group document is a failed discovery: reading it
// as "0 members" would be cached and erase what was last known. Only a `group`
// object carrying a `memberNames` key (even empty) is a document.
func TestNexusMembersUnreadableReplyIsNotARefusalNorAnEmptyGroup(t *testing.T) {
	for _, body := range []string{
		"", "  \n", "{", "<html>login</html>", "null", "{}",
		`{"group":{}}`, `{"group":null}`, `{"group":{"memberNames":null}}`, `{"group":{"memberNames":5}}`,
	} {
		t.Run(fmt.Sprintf("%q", body), func(t *testing.T) {
			info, formatHits := membersServer(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, body)
			})

			members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

			wantFailedDiscovery(t, members, err)
			var se *oci.StatusError
			if errors.As(err, &se) {
				t.Errorf("err = %v, is a *oci.StatusError, want a reading error (the status was 200)", err)
			}
			if got := formatHits.Load(); got != 1 {
				t.Errorf("the format endpoint received %d requests, want 1 (a 200 is not retried)", got)
			}
		})
	}
}

// The error type on its own: the sentinel through Is, the cause through Unwrap,
// and a message with the group and the code and nothing else.
func TestMembersRefusedErrorShape(t *testing.T) {
	cause := &oci.StatusError{Code: http.StatusForbidden, Source: oci.SourceManagementAPI}
	err := error(&MembersRefusedError{Group: "docker-group", Status: http.StatusForbidden, Cause: cause})

	if !errors.Is(err, ErrMembersRefused) {
		t.Errorf("errors.Is(err, ErrMembersRefused) = false, want true")
	}
	if errors.Is(err, errors.New("group members refused")) {
		t.Errorf("Is matches an unrelated error with the same text")
	}
	var se *oci.StatusError
	if !errors.As(err, &se) || se != cause {
		t.Errorf("errors.As *oci.StatusError = %v, want the cause itself", se)
	}
	want := `reading the members of group "docker-group": management API returned 403`
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// DEVDESK-578 (NX-07b): on the detailed endpoint of a group the generic endpoint
// already confirmed, a 404 whose body is Nexus's own {"message":"\"Repository not
// found\""} is what an account without nx-repository-admin gets (measured for the
// pull account of Nexus 3.98.1-01). It is a refusal. Any other 404 is not.

// nexusNotFoundBody is the body measured on the pull account's 404 (compact).
const nexusNotFoundBody = `{"id":"*","message":"\"Repository not found\""}`

// nexusNotFound answers the way Nexus answers a pull account on the detailed endpoint.
func nexusNotFound(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, nexusNotFoundBody)
}

// wantRefused404 adds to wantRefused what DEVDESK-578 promises about the text:
// the group and the status, never the body Nexus sent.
func wantRefused404(t *testing.T, members []GroupMember, err error, anonymous bool) {
	t.Helper()
	wantRefused(t, members, err, http.StatusNotFound, anonymous)
	if err == nil {
		return
	}
	const want = `reading the members of group "docker-group": management API returned 404`
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if strings.Contains(strings.ToLower(err.Error()), "repository not found") {
		t.Errorf("error carries the body of the reply: %v", err)
	}
	// The cause is the bare status error: neither repositoryNotFoundError nor the
	// body is reachable through it.
	var refused *MembersRefusedError
	if !errors.As(err, &refused) {
		return
	}
	if _, ok := refused.Cause.(*oci.StatusError); !ok {
		t.Errorf("Cause is %T, want exactly *oci.StatusError", refused.Cause)
	}
}

// The account gets Nexus's 404 and so does the anonymous retry (D156): a refusal
// of the account, two requests on the detailed endpoint.
func TestNexus404MembersRefusedAccountThenNotFoundAnonymousRetry(t *testing.T) {
	info, formatHits := membersServer(t, nexusNotFound)

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	wantRefused404(t, members, err, false)
	if got := formatHits.Load(); got != 2 {
		t.Errorf("the format endpoint received %d requests, want 2 (account, then the anonymous retry of D156)", got)
	}
}

// No credential: the same 404 is an anonymous refusal and there is nothing to drop.
func TestNexus404MembersWithoutCredentialIsAnAnonymousRefusal(t *testing.T) {
	info, formatHits := membersServer(t, nexusNotFound)
	info.Username, info.Password = "", ""

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	wantRefused404(t, members, err, true)
	if got := formatHits.Load(); got != 1 {
		t.Errorf("the format endpoint received %d requests, want exactly 1", got)
	}
}

// The account gets Nexus's 404 but the anonymous retry is let in: D156 still
// rescues the listing, and nothing is reported as refused.
func TestNexus404MembersAnonymousRetrySucceeds(t *testing.T) {
	info, formatHits := membersServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			nexusNotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"group":{"memberNames":["a-proxy"]}}`)
	})

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	if err != nil {
		t.Fatalf("err = %v, want nil (the anonymous retry succeeded)", err)
	}
	if len(members) != 1 || members[0].RepoPrefix != "a-proxy" {
		t.Errorf("members = %+v, want exactly a-proxy", members)
	}
	if got := formatHits.Load(); got != 2 {
		t.Errorf("the format endpoint received %d requests, want 2", got)
	}
}

// The account gets Nexus's 404, the anonymous retry a 500: the first attempt
// decides, the refusal is still the verdict.
func TestNexus404MembersWhoseAnonymousRetryFailsIsStillARefusal(t *testing.T) {
	info, _ := membersServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			nexusNotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	wantRefused404(t, members, err, false)
}

// Only Nexus's own "Repository not found" JSON is requalified. Everything else
// with a 404 stays a failed discovery that keeps its code.
func TestNexus404MembersOtherNotFoundIsNotARefusal(t *testing.T) {
	// A valid Nexus body followed by padding past the 4 KiB the reader keeps: the
	// message cannot be trusted to be the whole reply.
	padded := nexusNotFoundBody + strings.Repeat(" ", 5<<10)
	for _, tc := range []struct{ name, body string }{
		{"empty body", ""},
		{"html page naming the text", "<html><body>Repository not found</body></html>"},
		{"json not found", `{"message":"Not Found"}`},
		{"json other message", `{"id":"*","message":"Repository not found in this context"}`},
		{"invalid json", `{"id":"*","message":"\"Repository not found\"`},
		{"valid body then padding over 4 KiB", padded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info, _ := membersServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, tc.body)
			})

			members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

			wantFailedDiscovery(t, members, err)
			var se *oci.StatusError
			if !errors.As(err, &se) || se.Code != http.StatusNotFound {
				t.Errorf("err = %v, want errors.As *oci.StatusError with code 404", err)
			}
		})
	}
}

// A 404 "Repository not found" on the GENERIC endpoint says the repository is not
// there: a failed discovery, not a members refusal.
func TestNexus404GenericEndpointIsNotAMembersRefusal(t *testing.T) {
	info, _ := nexusServer(t, nexusNotFound)

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	wantFailedDiscovery(t, members, err)
	var se *oci.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusNotFound {
		t.Errorf("err = %v, want errors.As *oci.StatusError with code 404", err)
	}
}

// The deadline expires while the body of the 404 is being read (headers sent,
// then silence): a context error, never a refusal.
func TestNexus404MembersDeadlineWhileReadingTheBodyIsNotARefusal(t *testing.T) {
	info, _ := membersServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"id":"*",`)
		w.(http.Flusher).Flush()
		blockUntilGone(t, r)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	members, err := (&NexusDetector{}).DetectGroup(ctx, info)

	wantContextError(t, err, context.DeadlineExceeded)
	wantFailedDiscovery(t, members, err)
}

// The log of a 404 refusal says the status and keeps the admin advice, but never
// the body Nexus sent nor the password.
func TestNexus404MembersLogHoldsNeitherTheBodyNorThePassword(t *testing.T) {
	logs := nexusLog(t)
	info, _ := membersServer(t, nexusNotFound)

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	wantRefused404(t, members, err, false)
	out := logs.String()
	if !strings.Contains(out, "members refused") || !strings.Contains(out, "404") {
		t.Fatalf("the log does not record the refusal with its status (the test checks nothing): %s", out)
	}
	for _, leak := range []string{"Repository not found", `"id":"*"`, nexusTestPass} {
		if strings.Contains(out, leak) {
			t.Errorf("the log leaks %q: %s", leak, out)
		}
	}
	if !strings.Contains(out, nexusAdminAdvice) {
		t.Errorf("the log lost the admin advice for a 404 refusal: %s", out)
	}
}

// isRepositoryNotFound reads only Nexus's message: quotes around it removed, case
// ignored, anything else (including a non-JSON reply) is false.
func TestIsRepositoryNotFound(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want bool
	}{
		{"measured body", nexusNotFoundBody, true},
		{"pretty printed as recorded", "{\n  \"id\" : \"*\",\n  \"message\" : \"\\\"Repository not found\\\"\"\n}", true},
		{"without inner quotes", `{"message":"Repository not found"}`, true},
		{"message only, quoted", `{"message":"\"Repository not found\""}`, true},
		{"lower case", `{"message":"\"repository not found\""}`, true},
		{"upper case", `{"message":"REPOSITORY NOT FOUND"}`, true},
		{"surrounding whitespace", " \n" + nexusNotFoundBody + "\n ", true},
		{"nil body", "", false},
		{"null", `null`, false},
		{"empty object", `{}`, false},
		{"empty array", `[]`, false},
		{"message null", `{"message":null}`, false},
		{"message number", `{"message":5}`, false},
		{"message list", `{"message":["Repository not found"]}`, false},
		{"message object", `{"message":{"text":"Repository not found"}}`, false},
		{"other message", `{"message":"Not Found"}`, false},
		{"message with a suffix", `{"message":"Repository not found: docker-group"}`, false},
		{"other key", `{"error":"Repository not found"}`, false},
		{"html", `<html>Repository not found</html>`, false},
		{"truncated json", `{"message":"Repository not found"`, false},
		{"plain text", `Repository not found`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRepositoryNotFound([]byte(tc.body)); got != tc.want {
				t.Errorf("isRepositoryNotFound(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

// repositoryNotFoundError carries the status of the 404 and nothing of its body:
// its text and its cause are the StatusError's, and refusedStatus reads it as a
// 404 refusal, which a bare 404 StatusError is not.
func TestRepositoryNotFoundErrorShape(t *testing.T) {
	status := &oci.StatusError{Code: http.StatusNotFound, Source: oci.SourceManagementAPI}
	err := error(&repositoryNotFoundError{status: status})

	if got, want := err.Error(), status.Error(); got != want {
		t.Errorf("Error() = %q, want the status error's %q", got, want)
	}
	var se *oci.StatusError
	if !errors.As(err, &se) || se != status {
		t.Errorf("errors.As *oci.StatusError = %v, want the status itself", se)
	}
	if code, ok := refusedStatus(err); !ok || code != http.StatusNotFound {
		t.Errorf("refusedStatus(repositoryNotFoundError) = (%d, %v), want (404, true)", code, ok)
	}
	if code, ok := refusedStatus(fmt.Errorf("wrapped: %w", err)); !ok || code != http.StatusNotFound {
		t.Errorf("refusedStatus(wrapped) = (%d, %v), want (404, true)", code, ok)
	}
	if code, ok := refusedStatus(status); ok {
		t.Errorf("refusedStatus(bare 404 StatusError) = (%d, true), want false", code)
	}
}
