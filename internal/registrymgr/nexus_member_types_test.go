package registrymgr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// DEVDESK-629 (F2a): DetectGroup reads the type of each member with one GET on
// /service/rest/v1/repositories/<member>. A failure leaves the type unknown and
// never fails the discovery.

const typeEndpoint = "/service/rest/v1/repositories/"

// typeRecorder notes every request the member-type endpoint received.
type typeRecorder struct {
	mu    sync.Mutex
	paths []string // escaped path, as it went on the wire
	auths []string // "user:password" of each request, "" when none
}

func (r *typeRecorder) note(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, pass, ok := req.BasicAuth()
	auth := ""
	if ok {
		auth = user + ":" + pass
	}
	r.paths = append(r.paths, req.URL.EscapedPath())
	r.auths = append(r.auths, auth)
}

func (r *typeRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.paths)
}

// typedGroupServer serves a group that lists members on the generic endpoint
// (viaFormat false) or on the format endpoint (viaFormat true), and answers a
// member's own endpoint with memberAnswer. The group's own endpoints are not
// recorded: only requests for a member are.
func typedGroupServer(t *testing.T, members []string, viaFormat bool, memberAnswer func(w http.ResponseWriter, r *http.Request, name string)) (RegistryInfo, *typeRecorder) {
	t.Helper()
	rec := &typeRecorder{}
	names, _ := json.Marshal(members)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.EscapedPath()
		switch {
		case path == typeEndpoint+"docker-group" && viaFormat:
			_, _ = io.WriteString(w, nexusGroupNoMembers)
		case path == typeEndpoint+"docker-group":
			_, _ = fmt.Fprintf(w, `{"type":"group","format":"docker","group":{"memberNames":%s}}`, names)
		case path == typeEndpoint+"docker/group/docker-group":
			_, _ = fmt.Fprintf(w, `{"group":{"memberNames":%s}}`, names)
		case strings.HasPrefix(path, typeEndpoint):
			rec.note(r)
			name := strings.TrimPrefix(path, typeEndpoint)
			memberAnswer(w, r, name)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return RegistryInfo{Provider: ProviderNexus, URL: srv.URL + "/repository/docker-group", Username: nexusTestUser, Password: nexusTestPass}, rec
}

func answerType(typ string) func(http.ResponseWriter, *http.Request, string) {
	return func(w http.ResponseWriter, _ *http.Request, _ string) {
		_, _ = fmt.Fprintf(w, `{"type":%q,"format":"docker"}`, typ)
	}
}

func typesOf(members []GroupMember) []string {
	out := make([]string, len(members))
	for i, m := range members {
		out[i] = m.Type
	}
	return out
}

// Both paths of DetectGroup type their members: the generic endpoint that already
// lists them, and the format endpoint that lists them after it.
func TestMemberTypeLookupTypesTheMembersOnBothPaths(t *testing.T) {
	for _, viaFormat := range []bool{false, true} {
		t.Run(fmt.Sprintf("viaFormat=%v", viaFormat), func(t *testing.T) {
			info, rec := typedGroupServer(t, []string{"a-hosted", "b-proxy", "sub-group"}, viaFormat,
				func(w http.ResponseWriter, r *http.Request, name string) {
					answerType(map[string]string{"a-hosted": "hosted", "b-proxy": "proxy", "sub-group": "group"}[name])(w, r, name)
				})

			members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if got := strings.Join(typesOf(members), ","); got != "hosted,proxy,group" {
				t.Errorf("types = %s, want hosted,proxy,group (in the order of the group's member names)", got)
			}
			if rec.count() != 3 {
				t.Errorf("%d type requests, want one per member (3)", rec.count())
			}
		})
	}
}

func TestMemberTypeLookupFailureLeavesItUnknown(t *testing.T) {
	old := nexusHTTPClient
	nexusHTTPClient = &http.Client{Timeout: 400 * time.Millisecond, CheckRedirect: old.CheckRedirect}
	t.Cleanup(func() { nexusHTTPClient = old })
	logs := nexusLog(t)

	members := []string{"m-500", "m-slow", "m-empty", "m-weird", "m-text", "m-null", "m-hosted", "m-proxy"}
	info, rec := typedGroupServer(t, members, false, func(w http.ResponseWriter, r *http.Request, name string) {
		switch name {
		case "m-500":
			w.WriteHeader(http.StatusInternalServerError)
		case "m-slow":
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		case "m-empty":
			_, _ = io.WriteString(w, `{}`)
		case "m-weird":
			_, _ = io.WriteString(w, `{"type":"weird\nline"}`)
		case "m-text":
			_, _ = io.WriteString(w, `not json`)
		case "m-null":
			_, _ = io.WriteString(w, `null`)
		case "m-hosted":
			answerType("hosted")(w, r, name)
		case "m-proxy":
			answerType("proxy")(w, r, name)
		}
	})

	got, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	if err != nil {
		t.Fatalf("err = %v: a type that cannot be read must not fail the discovery", err)
	}
	if len(got) != len(members) {
		t.Fatalf("%d members, want the complete list of %d", len(got), len(members))
	}
	if want := ",,,,,,hosted,proxy"; strings.Join(typesOf(got), ",") != want {
		t.Errorf("types = %q, want %q: unknown on every failure, the others typed", strings.Join(typesOf(got), ","), want)
	}
	for i, m := range got {
		if m.RepoPrefix != members[i] {
			t.Errorf("member %d prefix = %q, want %q: the list must not change", i, m.RepoPrefix, members[i])
		}
	}
	if rec.count() != len(members) {
		t.Errorf("%d type requests, want %d: one failure must not stop the others", rec.count(), len(members))
	}
	// Server-supplied values go in the log quoted, never raw (a newline in a type
	// must not forge a log line).
	if strings.Contains(logs.String(), "weird\nline") {
		t.Errorf("the log carries the server's type raw: %q", logs.String())
	}
	if !strings.Contains(logs.String(), `"weird\nline"`) {
		t.Errorf("the log does not quote the unknown type with %%q: %q", logs.String())
	}
}

func TestMemberTypeLookupEscapesTheName(t *testing.T) {
	info, rec := typedGroupServer(t, []string{"a b"}, false, answerType("hosted"))

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if want := typeEndpoint + "a%20b"; len(rec.paths) != 1 || rec.paths[0] != want {
		t.Errorf("type request paths = %q, want [%q]: the name stays one escaped path segment", rec.paths, want)
	}
	if len(members) != 1 || members[0].Type != "hosted" {
		t.Errorf("members = %+v, want the one typed hosted", members)
	}
}

func TestMemberTypeLookupIsCapped(t *testing.T) {
	var names []string
	for i := range 40 {
		names = append(names, fmt.Sprintf("m%02d", i))
	}
	info, rec := typedGroupServer(t, names, false, answerType("hosted"))

	members, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if rec.count() != 32 {
		t.Errorf("%d type requests, want 32 at most", rec.count())
	}
	if len(members) != 40 {
		t.Fatalf("%d members, want all 40: the cap limits the requests, not the list", len(members))
	}
	for i, m := range members {
		want := "hosted"
		if i >= 32 {
			want = ""
		}
		if m.Type != want {
			t.Errorf("member %d Type = %q, want %q (the first 32 are typed, the rest unknown)", i, m.Type, want)
		}
	}
}

func TestMemberTypeLookupSendsTheGroupCredentials(t *testing.T) {
	info, rec := typedGroupServer(t, []string{"a-hosted", "b-proxy"}, false, answerType("hosted"))

	if _, err := (&NexusDetector{}).DetectGroup(context.Background(), info); err != nil {
		t.Fatalf("err = %v", err)
	}

	if rec.count() != 2 {
		t.Fatalf("%d type requests, want 2", rec.count())
	}
	for i, got := range rec.auths {
		if want := nexusTestUser + ":" + nexusTestPass; got != want {
			t.Errorf("type request %d carried %q, want the group's account", i, got)
		}
	}
}

// Without credentials no Authorization header goes out either.
func TestMemberTypeLookupWithoutCredentialsIsAnonymous(t *testing.T) {
	info, rec := typedGroupServer(t, []string{"a-hosted"}, false, answerType("hosted"))
	info.Username, info.Password = "", ""

	if _, err := (&NexusDetector{}).DetectGroup(context.Background(), info); err != nil {
		t.Fatalf("err = %v", err)
	}

	if rec.count() != 1 || rec.auths[0] != "" {
		t.Errorf("type requests = %d with auths %q, want one without credentials", rec.count(), rec.auths)
	}
}

// A `type` longer than 64 runes is cut before it is logged, so a hostile manager
// cannot fill the log.
func TestMemberTypeLogTruncatesALongType(t *testing.T) {
	logs := nexusLog(t)
	long := strings.Repeat("x", 5000)
	info, _ := typedGroupServer(t, []string{"m-long"}, false, answerType(long))

	got, err := (&NexusDetector{}).DetectGroup(context.Background(), info)

	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got[0].Type != "" {
		t.Errorf("Type = %q, want unknown", got[0].Type)
	}
	out := logs.String()
	if !strings.Contains(out, strings.Repeat("x", 64)) || strings.Contains(out, strings.Repeat("x", 65)) {
		t.Errorf("the log must hold exactly 64 characters of the type, got %d bytes of log", len(out))
	}
}

// At most typeLookupWorkers lookups are in flight at once.
func TestMemberTypeLookupConcurrencyStaysWithinTheWorkerCount(t *testing.T) {
	var names []string
	for i := range 24 {
		names = append(names, fmt.Sprintf("m%02d", i))
	}
	var inFlight, peak atomic.Int32
	info, rec := typedGroupServer(t, names, false, func(w http.ResponseWriter, r *http.Request, name string) {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		answerType("hosted")(w, r, name)
	})

	if _, err := (&NexusDetector{}).DetectGroup(context.Background(), info); err != nil {
		t.Fatalf("err = %v", err)
	}

	if rec.count() != len(names) {
		t.Errorf("%d type requests, want %d", rec.count(), len(names))
	}
	if got := peak.Load(); got > typeLookupWorkers || got < 2 {
		t.Errorf("peak concurrency = %d, want between 2 and %d", got, typeLookupWorkers)
	}
}

// An expired context starts no further lookup and leaves the types unknown, with
// no member-type line in the log.
func TestMemberTypeLookupStopsWhenTheContextIsDone(t *testing.T) {
	logs := nexusLog(t)
	info, rec := typedGroupServer(t, []string{"a", "b", "c"}, false, answerType("hosted"))
	// DetectGroup itself would fail on a dead context; ask the lookup directly.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	types := (&NexusDetector{}).memberTypes(ctx, strings.TrimSuffix(info.URL, "/repository/docker-group"), []string{"a", "b", "c"}, info)

	if len(types) != 0 {
		t.Errorf("types = %v, want none after cancellation", types)
	}
	if rec.count() != 0 {
		t.Errorf("%d type requests after cancellation, want none", rec.count())
	}
	if strings.Contains(logs.String(), "type unknown") {
		t.Errorf("lookups were logged after cancellation: %q", logs.String())
	}
}

// A deadline that expires while every worker is busy stops the loop: the lookups
// that were not started are not launched afterwards.
func TestMemberTypeLookupStopsLaunchingAfterTheDeadline(t *testing.T) {
	var names []string
	for i := range 32 {
		names = append(names, fmt.Sprintf("m%02d", i))
	}
	info, rec := typedGroupServer(t, names, false, func(w http.ResponseWriter, r *http.Request, _ string) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	types := (&NexusDetector{}).memberTypes(ctx, strings.TrimSuffix(info.URL, "/repository/docker-group"), names, info)

	if time.Since(start) > 2*time.Second {
		t.Errorf("memberTypes took %v after a 100 ms deadline", time.Since(start))
	}
	if len(types) != 0 {
		t.Errorf("types = %v, want none", types)
	}
	if got := rec.count(); got > typeLookupWorkers {
		t.Errorf("%d lookups started, want at most the %d that were running at the deadline", got, typeLookupWorkers)
	}
}
