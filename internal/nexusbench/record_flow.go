package nexusbench

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// The replay of the jobs of a run and the flows of a fixture (DEVDESK-580, PR2).
//
// A fixture that says "flow: bearer" or "flow: redirects" holds several exchanges
// and the recorder follows them as DevDesk would, within limits:
//
//   - bearer: the first request goes out as DevDesk sends it. On a 401 with a Bearer
//     challenge whose realm is an origin of RecordOptions.Origins (matched on
//     host:port) and a token path, the token is asked for with the credentials of the
//     identity and the first request is sent again with it: three exchanges. Any
//     other realm ends the flow, nothing is sent to it. The token read is a term no
//     file and no output may hold;
//   - redirects: a 3xx whose Location is on the same origin as the request of the
//     hop (its host:port equals that request's Host) and a request the closed list
//     allows is followed, 5 hops at most, each hop with the credentials of the first
//     request; any other Location, another origin of Origins included, is recorded
//     and not followed.
//
// A reason to stop short is written in the file, in the parentheses of the "nexus"
// header, which the recorder rewrites on every run.

const (
	// maxRedirectHops is how many redirects a chain may hold before it is an error.
	maxRedirectHops = 5

	noteRealmOrigin  = "realm off-origin, not followed"
	noteRealm        = "realm not followed"
	noteNoChallenge  = "no challenge"
	noteNoBearer     = "no Bearer challenge"
	noteTokenRefused = "token refused, not retried"
	noteRedirect     = "off-origin redirect, not followed"
	noteRedirectList = "redirect to a request outside the closed list, not followed"

	// tokenAccept is the Accept header of a token request.
	tokenAccept = "application/json"
)

var (
	challengeParam = regexp.MustCompile(`([A-Za-z_]+)="([^"]*)"`)
	redirectCodes  = []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect}
)

// recorder is the state of one run of the jobs: its clients, one per origin and
// identity, and the terms no output may hold.
type recorder struct {
	o        RecordOptions
	restHost string
	timeout  time.Duration
	clients  map[string]*client
	// terms are the bench passwords and the deny list, then every token read by a flow.
	terms []string
	// cacheEffects are the fixtures sent with "effect: caches", in the order sent.
	cacheEffects []string
}

func newRecorder(o RecordOptions) *recorder {
	restHost, _ := restOriginKey(o.Descriptor)
	return &recorder{
		o: o, restHost: restHost, timeout: recordRequestTimeout(o),
		clients: map[string]*client{}, terms: recordTerms(o),
	}
}

func (r *recorder) close() {
	for _, c := range r.clients {
		c.closeIdle()
	}
}

// client returns the client of an origin for the identity of the job.
func (r *recorder) client(j recordJob, base string) *client {
	key := base + "\x00" + string(j.current.Identity)
	c, ok := r.clients[key]
	if !ok {
		c = newClient(base, j.user, j.password)
		c.setTimeout(r.timeout)
		r.clients[key] = c
	}
	return c
}

// replayRequest is a request to send: the host is a key of RecordOptions.Origins.
type replayRequest struct {
	method, target, host string
	header               http.Header
}

// replayStep is one answer of a job. issued is the token this answer carries on
// purpose, which the redaction hides.
type replayStep struct {
	raw    nexusfixture.RawExchange
	issued string
}

// gatherAnswers asks every job and returns the files to write, the fixtures that
// did not change and the fixtures sent with a cache effect. The first problem ends
// the run: nothing is written yet.
func gatherAnswers(ctx context.Context, o RecordOptions, jobs []recordJob) (writes []recordWrite, unchanged, sent []string, err error) {
	r := newRecorder(o)
	defer r.close()
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	date := now().Format(recordedDateLayout)
	for _, j := range jobs {
		data, same, err := r.record(ctx, j, date)
		switch {
		case err != nil:
			return nil, nil, r.cacheEffects, err
		case same:
			unchanged = append(unchanged, j.name)
		default:
			writes = append(writes, recordWrite{name: j.name, path: j.path, data: data})
		}
	}
	return writes, unchanged, r.cacheEffects, nil
}

// record asks the bench for the answers of the job and returns the bytes of the
// fixture it would write; same is true when the file already holds those answers.
func (r *recorder) record(ctx context.Context, j recordJob, date string) (data []byte, same bool, err error) {
	if j.current.Meta.Effect == nexusfixture.EffectCaches {
		r.cacheEffects = append(r.cacheEffects, j.name)
	}
	steps, note, err := r.replay(ctx, j)
	if err != nil {
		return nil, false, err
	}
	answers := make([]nexusfixture.Exchange, len(steps))
	for i, s := range steps {
		answers[i] = redactAnswer(s.raw)
	}
	f := j.current
	f.Meta.NexusVersion, f.Meta.Edition, f.Meta.NexusNote = r.o.Descriptor.NexusVersion, r.o.Descriptor.Edition, note
	f.Meta.Measured, f.Meta.RecordedBy = date, recordedBy
	if f.Meta.Record == nexusfixture.RecordPending {
		f.Meta.Record = ""
	}
	f.Exchanges = answers
	data = nexusfixture.Format(f)

	// What will be written is read back and checked, not what was built.
	back, err := nexusfixture.Parse(j.campaign, filepath.Base(j.path), data)
	if err != nil || !bytes.Equal(nexusfixture.Format(back), data) {
		return nil, false, recordError("the answer to "+j.name+" does not read back as a fixture", "do not record this answer", err)
	}
	if leaks := nexusfixture.CheckLeaks(j.name, data, r.terms); len(leaks) > 0 {
		return nil, false, recordError("the answer to "+j.name+" would leak: "+describeLeaks(leaks), "nothing was written; fix the bench or the redaction", nil)
	}
	for _, s := range steps {
		if holdsTerm(s.raw.Header, s.raw.Body, r.termsWithout(s.issued)) {
			return nil, false, recordError("the answer to "+j.name+" holds a bench password, a token or a denied term where the file would not show it", "nothing was written; do not record this answer", nil)
		}
	}
	return data, sameAnswer(j.current, f), nil
}

// termsWithout is the list of forbidden terms but the token an answer carries on purpose.
func (r *recorder) termsWithout(issued string) []string {
	if issued == "" {
		return r.terms
	}
	return slices.DeleteFunc(slices.Clone(r.terms), func(t string) bool { return t == issued })
}

// redactAnswer redacts a raw answer for a file.
func redactAnswer(raw nexusfixture.RawExchange) nexusfixture.Exchange {
	answer := nexusfixture.Redact(raw)
	if answer.Stub == "" && answer.Header.Get("Content-Length") != "" {
		// A redacted body is not as long as the one Nexus sent.
		answer.Header.Set("Content-Length", strconv.Itoa(len(answer.Body)))
	}
	return answer
}

// sameAnswer reports whether the file already holds the answers: the same
// exchanges, all of them, under the same version and edition. The date, the notes
// and who recorded it do not count.
func sameAnswer(current, recorded nexusfixture.Fixture) bool {
	if current.Meta.NexusVersion != recorded.Meta.NexusVersion || current.Meta.Edition != recorded.Meta.Edition ||
		len(current.Exchanges) != len(recorded.Exchanges) {
		return false
	}
	text := func(ex nexusfixture.Exchange) []byte {
		return nexusfixture.Format(nexusfixture.Fixture{Exchanges: []nexusfixture.Exchange{ex}})
	}
	for i := range current.Exchanges {
		if !bytes.Equal(text(current.Exchanges[i]), text(recorded.Exchanges[i])) {
			return false
		}
	}
	return true
}

// ---- sending -----------------------------------------------------------------------

// replay sends the first request of the job and follows its flow.
func (r *recorder) replay(ctx context.Context, j recordJob) (steps []replayStep, note string, err error) {
	first := j.current.Exchanges[0]
	rq := replayRequest{method: first.Method, target: first.Target, host: first.Host, header: first.RequestHeader}
	switch j.current.Meta.Flow {
	case nexusfixture.FlowBearer:
		return r.bearerFlow(ctx, j, rq)
	case nexusfixture.FlowRedirects:
		return r.redirectFlow(ctx, j, rq)
	}
	s, err := r.send(ctx, j, rq, "")
	if err != nil {
		return nil, "", err
	}
	return []replayStep{s}, "", nil
}

// send asks one request of a job, as the credentials of its identity, or with
// bearer as a token when it is not empty, and checks what the bench answers.
func (r *recorder) send(ctx context.Context, j recordJob, rq replayRequest, bearer string) (replayStep, error) {
	c := r.client(j, r.o.Origins[rq.host])
	status, header, body, err := c.recorded(ctx, rq.method, rq.target, rq.header, bearer)
	if err != nil {
		return replayStep{}, recordError("the answer to "+j.name+" could not be read", "check that the bench answers on its loopback ports", err)
	}
	if !benchVersionMatches(header.Get("Server"), r.o.Descriptor) {
		return replayStep{}, recordError("the answer to "+j.name+" does not come from the Nexus version and edition the descriptor names", upCheck, nil)
	}
	if declaresJSON(header.Get("Content-Type")) && len(body) > 0 && !json.Valid(body) {
		return replayStep{}, recordError("the answer to "+j.name+" is declared JSON and is not", "do not record this answer: a secret could hide from the redaction in it", nil)
	}
	return replayStep{raw: nexusfixture.RawExchange{
		Method: rq.method, Target: rq.target, Host: rq.host, RequestHeader: rq.header,
		Status: status, Header: header, Body: body,
	}}, nil
}

// ---- flow: bearer --------------------------------------------------------------------

// bearerChallenge is what a WWW-Authenticate Bearer challenge asks.
type bearerChallenge struct{ realm, service, scope string }

// bearerFlow sends the request, and on a Bearer challenge it can follow, asks for
// the token and sends the request again with it.
func (r *recorder) bearerFlow(ctx context.Context, j recordJob, rq replayRequest) ([]replayStep, string, error) {
	call, err := r.send(ctx, j, rq, "")
	if err != nil {
		return nil, "", err
	}
	steps := []replayStep{call}
	if call.raw.Status != http.StatusUnauthorized {
		return steps, noteNoChallenge, nil
	}
	challenge, ok := parseBearerChallenge(call.raw.Header)
	if !ok {
		return steps, noteNoBearer, nil
	}
	tokenRequest, note := r.tokenRequest(j, challenge)
	if note != "" {
		return steps, note, nil
	}
	issued, err := r.send(ctx, j, tokenRequest, "")
	if err != nil {
		return nil, "", err
	}
	steps = append(steps, issued)
	if issued.raw.Status != http.StatusOK {
		return steps, noteTokenRefused, nil
	}
	token := tokenOf(issued.raw.Body)
	if token == "" {
		return nil, "", recordError("the token answer to "+j.name+" holds no token", "do not record this answer", nil)
	}
	// Before any check of any answer: the token is a term no file may hold.
	r.terms = append(r.terms, token)
	steps[len(steps)-1].issued = token

	retry := rq
	retry.header = placeholderHeader(rq.header, "Bearer")
	again, err := r.send(ctx, j, retry, token)
	if err != nil {
		return nil, "", err
	}
	return append(steps, again), "", nil
}

// tokenRequest builds the request for the token of a challenge. It exists only when
// the realm is an origin of the run and a token path: anything else is not sent the
// credentials of the bench, and the note says why (noteRealmOrigin for an origin the
// run does not know, noteRealm for the other refusals).
func (r *recorder) tokenRequest(j recordJob, c bearerChallenge) (replayRequest, string) {
	u, err := url.Parse(c.realm)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return replayRequest{}, noteRealm
	}
	if _, known := r.o.Origins[u.Host]; !known {
		return replayRequest{}, noteRealmOrigin
	}
	if !nexusfixture.IsTokenPath(u.Path) || u.EscapedPath() != u.Path {
		return replayRequest{}, noteRealm
	}
	query := url.Values{}
	if c.service != "" {
		query.Set("service", c.service)
	}
	if c.scope != "" {
		query.Set("scope", c.scope)
	}
	target := u.Path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	scheme := ""
	if j.user != "" {
		scheme = "Basic"
	}
	return replayRequest{
		method: http.MethodGet, target: target, host: u.Host,
		header: placeholderHeader(http.Header{"Accept": {tokenAccept}}, scheme),
	}, ""
}

// parseBearerChallenge reads the first Bearer challenge of an answer.
func parseBearerChallenge(h http.Header) (bearerChallenge, bool) {
	for _, v := range h.Values("WWW-Authenticate") {
		if len(v) < len("Bearer ") || !strings.EqualFold(v[:len("Bearer")], "Bearer") || v[len("Bearer")] != ' ' {
			continue
		}
		var c bearerChallenge
		for _, m := range challengeParam.FindAllStringSubmatch(v[len("Bearer "):], -1) {
			switch strings.ToLower(m[1]) {
			case "realm":
				c.realm = m[2]
			case "service":
				c.service = m[2]
			case "scope":
				c.scope = m[2]
			}
		}
		if c.realm != "" {
			return c, true
		}
	}
	return bearerChallenge{}, false
}

// tokenOf reads the token of a token answer, "" when it holds none.
func tokenOf(body []byte) string {
	var answer struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return ""
	}
	return cmp.Or(answer.Token, answer.AccessToken)
}

// ---- flow: redirects -----------------------------------------------------------------

// redirectFlow sends the request and follows the redirects it can.
func (r *recorder) redirectFlow(ctx context.Context, j recordJob, rq replayRequest) ([]replayStep, string, error) {
	var steps []replayStep
	for hops := 0; ; hops++ {
		s, err := r.send(ctx, j, rq, "")
		if err != nil {
			return nil, "", err
		}
		steps = append(steps, s)
		location := s.raw.Header.Get("Location")
		if location == "" || !slices.Contains(redirectCodes, s.raw.Status) {
			return steps, "", nil
		}
		if hops == maxRedirectHops {
			return nil, "", recordError("the answer to "+j.name+" redirects more than "+strconv.Itoa(maxRedirectHops)+" times",
				"do not record a chain that does not end", nil)
		}
		next, note := r.redirectedRequest(j, rq, location)
		if note != "" {
			return steps, note, nil
		}
		rq = next
	}
}

// redirectedRequest is the request a Location asks for: a relative one is resolved
// against the request that was redirected. It is empty, with the note that says
// why, unless the target is on the origin of the redirected request and is a request
// the recorder sends: another origin of the run is recorded, not followed.
func (r *recorder) redirectedRequest(j recordJob, from replayRequest, location string) (replayRequest, string) {
	base, err := url.Parse("http://" + from.host + from.target)
	if err != nil {
		return replayRequest{}, noteRedirect
	}
	ref, err := url.Parse(location)
	if err != nil {
		return replayRequest{}, noteRedirect
	}
	u := base.ResolveReference(ref)
	if u.Host != from.host || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return replayRequest{}, noteRedirect
	}
	next := replayRequest{method: from.method, target: u.RequestURI(), host: u.Host, header: from.header}
	ex := nexusfixture.Exchange{Method: next.method, Target: next.target, Host: next.host}
	if reason, _ := refusedFixtureRequest(ex, j.current.Meta, r.restHost, r.o.AllowCacheEffects); reason != "" {
		return replayRequest{}, noteRedirectList
	}
	return next, ""
}
