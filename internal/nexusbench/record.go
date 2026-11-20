package nexusbench

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// The fixture recorder (DEVDESK-511, step 7). RecordFixtures replays the request
// of each recorded fixture against the bench and writes the redacted answer back
// to the same file. It sends the bench credentials, so everything that could go
// wrong is refused before the first fixture request, and nothing is written
// unless every answer passed: GET and the HEAD of a manifest only, a closed list
// of paths, loopback origins only, a redirection or a Bearer challenge followed
// only inside a declared flow (record_flow.go), a leak check of what will be written.

const (
	// recordedBy is the value of the "recorded-by" header the recorder sets.
	recordedBy = "nexus-bench record"
	// recordedFileMode and recordedDirMode are the modes of a written fixture.
	recordedFileMode os.FileMode = 0o644
	recordedDirMode  os.FileMode = 0o755
	// recordedDateLayout is the date of the "measured" header.
	recordedDateLayout = "2006-01-02"
)

// BenchState is the state of the bench a run records against: the fixtures whose
// "state" header names another state are skipped (DEVDESK-580, PR2).
type BenchState string

// The states a run can declare.
const (
	// StateNominal is the bench as seeded: the upstream is up, the proxy is in
	// service. A run in this state takes the fixtures with no "state" header, or one
	// that starts with "nominal".
	StateNominal BenchState = ""
	// StateUpstreamCut is the bench after cut-upstream: the upstream has no replica.
	// A run in this state takes the fixtures whose "state" header starts with it.
	StateUpstreamCut BenchState = "upstream-cut"
	// StateAny is a run that declares no state: every fixture is recorded whatever
	// its "state" header says, as before the states existed. It is what the command
	// passes when --state is not given, so that nothing changes for a run that never
	// asked for a state; it is not a state of the bench.
	StateAny BenchState = "any"
)

// defaultRecordTimeout bounds one request of the recorder when RecordOptions.RequestTimeout is 0.
const defaultRecordTimeout = 15 * time.Second

// recordRequestTimeout is the timeout of one request of a run: the option, or the default.
func recordRequestTimeout(o RecordOptions) time.Duration {
	if o.RequestTimeout > 0 {
		return o.RequestTimeout
	}
	return defaultRecordTimeout
}

// RecordOptions says what to record and against which bench.
type RecordOptions struct {
	// Origins maps the Host of a fixture ("127.0.0.1:30481") to the base URL that
	// reaches it. Each key must be a loopback host:port of the descriptor and each
	// URL a loopback origin (IsLoopbackRESTHost), no user information. The REST
	// origin (first host of the descriptor, REST node port) is required: the data
	// set is compared through it.
	Origins    map[string]string
	Descriptor Descriptor
	Secrets    Secrets
	// Dir is the folder of the campaigns; empty means nexusfixture.SourceDir().
	Dir string
	// Campaigns limits the run to these campaigns; empty means every folder of Dir.
	Campaigns []string
	// Only is an optional glob (path.Match) on fixture names, without ".http".
	Only string
	// Now gives the date of the "measured" header; nil means time.Now.
	Now func() time.Time
	// DenyTerms are extra terms no written fixture may hold (the external deny list).
	DenyTerms []string
	// State is the state the bench is in: only the fixtures whose "state" header
	// starts with it are recorded, the others are reported skipped. The zero value is
	// StateNominal, which takes the fixtures with no "state" header; StateAny turns
	// the filter off. The library only filters: it is the command that reads the
	// cluster to check that the bench is in the state the run declares.
	State BenchState
	// AllowCacheEffects lets a fixture that says "effect: caches" be sent: such a
	// request can leave an image in the proxy, and the bench has then to be reset.
	AllowCacheEffects bool
	// RequestTimeout bounds one request of the run, connection included; 0 means
	// defaultRecordTimeout (15 s).
	RequestTimeout time.Duration
}

// RecordReport lists the fixtures of a run as "<campaign>/<name>". A Skipped
// entry is followed by ": <reason>".
type RecordReport struct {
	Written, Unchanged, Skipped []string
	// CacheEffects are the fixtures of the run that were sent with "effect: caches":
	// the bench can hold a cached image because of them, and needs a reset. It is
	// also filled when the run fails after sending one, the only part of the report
	// an error leaves.
	CacheEffects []string
}

// recordJob is one fixture to replay.
type recordJob struct {
	campaign string
	name     string // "<campaign>/<file name without .http>"
	path     string
	current  nexusfixture.Fixture
	user     string // "" for an anonymous fixture
	password string
}

// recordBadPassword is the public wrong password of the badcred identity. It is the
// recorder's own constant, equal to fakenexus.BadPassword, which the replay server
// recognizes: fakenexus imports testing, so the recorder cannot import it. It is no
// secret of the bench.
const recordBadPassword = "fixture-wrong-password"

// recordWrite is a fixture file to replace, held back until every answer passed.
type recordWrite struct {
	name string
	path string
	data []byte
}

// RecordFixtures regenerates the recorded fixtures from the bench. A fixture is
// skipped (reported, never requested) when it says "record: skip", when it is of
// another state than the run (RecordOptions.State) or when it holds more than one
// exchange and names no flow. A fixture whose answers are the ones it already
// holds, apart from its date, is left alone, bytes and modification time. Every
// error is a *BenchError, free of secrets; on an error nothing is written and the
// report is empty, apart from RecordReport.CacheEffects.
func RecordFixtures(ctx context.Context, o RecordOptions) (RecordReport, error) {
	if err := ctx.Err(); err != nil {
		return RecordReport{}, recordError("the recording was cancelled before it started", "run it again", err)
	}
	jobs, report, err := planRecording(o)
	if err != nil {
		return RecordReport{}, err
	}
	if len(jobs) > 0 {
		if err := checkBenchDataset(ctx, o); err != nil {
			return RecordReport{}, err
		}
	}
	writes, unchanged, sent, err := gatherAnswers(ctx, o, jobs)
	if err != nil {
		return RecordReport{CacheEffects: sent}, err
	}
	report.Unchanged, report.CacheEffects = unchanged, sent
	for i, w := range writes {
		if err := writeFileAtomic(w.path, w.data, recordedFileMode, recordedDirMode); err != nil {
			return RecordReport{CacheEffects: sent}, recordError(
				fmt.Sprintf("%s could not be written (%d fixture(s) of this run were already written)", w.name, i),
				"check the permissions of the fixtures folder, then record again", err)
		}
		report.Written = append(report.Written, w.name)
	}
	return report, nil
}

// recordError returns a BenchError; err is kept for errors.Is and errors.As and
// is never printed. A refusal with no underlying error keeps its own cause.
func recordError(cause, check string, err error) *BenchError {
	if err == nil {
		err = errors.New(cause)
	}
	return &BenchError{Kind: Unclassified, Cause: cause, Check: check, Err: err}
}

// ---- planning: everything is checked before the first request ------------------

// planRecording reads the fixtures and refuses anything that must not be sent.
func planRecording(o RecordOptions) ([]recordJob, RecordReport, error) {
	var report RecordReport
	if err := checkRunOptions(o); err != nil {
		return nil, report, err
	}
	if err := checkOrigins(o); err != nil {
		return nil, report, err
	}
	if o.Only != "" {
		if _, err := path.Match(o.Only, ""); err != nil {
			return nil, report, recordError("the fixture filter is not a valid glob", "write it as a path.Match pattern, e.g. catalog-*", err)
		}
	}
	dir := o.Dir
	if dir == "" {
		var err error
		if dir, err = nexusfixture.SourceDir(); err != nil {
			return nil, report, recordError("the fixtures folder of the source tree cannot be located", "run the recorder from a build that knows its source tree (not -trimpath)", err)
		}
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, report, recordError("the fixtures folder "+dir+" is not there", "check the folder", err)
	}
	fsys := os.DirFS(dir)
	campaigns, err := recordCampaigns(dir, fsys, o.Campaigns)
	if err != nil {
		return nil, report, err
	}
	var jobs []recordJob
	for _, campaign := range campaigns {
		entries, err := fs.ReadDir(fsys, campaign)
		if err != nil {
			return nil, report, recordError("the campaign "+campaign+" cannot be read", "check the fixtures folder", err)
		}
		for _, e := range entries {
			name, ok := strings.CutSuffix(e.Name(), ".http")
			if e.IsDir() || !ok {
				continue
			}
			if err := refuseLink(filepath.Join(dir, campaign, e.Name()), campaign+"/"+name); err != nil {
				return nil, report, err
			}
			if o.Only != "" {
				if match, _ := path.Match(o.Only, name); !match {
					continue
				}
			}
			job, skipped, err := planFixture(o, fsys, filepath.Join(dir, campaign, e.Name()), campaign, e.Name())
			switch {
			case err != nil:
				return nil, report, err
			case skipped != "":
				report.Skipped = append(report.Skipped, campaign+"/"+name+": "+skipped)
			default:
				jobs = append(jobs, job)
			}
		}
	}
	return jobs, report, nil
}

// refuseLink refuses a campaign folder or a fixture file that is a symbolic
// link: a link could carry a run, which sends the bench credentials and rewrites
// files, to something that is not in the fixtures folder. A path that cannot be
// inspected is left to the read that follows.
func refuseLink(filePath, label string) error {
	if info, err := os.Lstat(filePath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return recordError(label+" is a symbolic link", "replace the link with the real file or folder", nil)
	}
	return nil
}

// recordCampaigns returns the campaigns to read: the ones asked for, each a plain
// folder name of fsys (dir on disk), or every folder of fsys. A campaign that is
// a symbolic link is refused, asked for or found.
func recordCampaigns(dir string, fsys fs.FS, wanted []string) ([]string, error) {
	if len(wanted) == 0 {
		entries, err := fs.ReadDir(fsys, ".")
		if err != nil {
			return nil, recordError("the fixtures folder cannot be read", "check the folder", err)
		}
		var all []string
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if e.Type()&fs.ModeSymlink != 0 {
				return nil, recordError("the campaign "+e.Name()+" is a symbolic link", "replace the link with the real folder", nil)
			}
			if e.IsDir() {
				all = append(all, e.Name())
			}
		}
		return all, nil
	}
	for _, c := range wanted {
		if c == "." || !fs.ValidPath(c) || strings.Contains(c, "/") {
			return nil, recordError("the campaign "+strconv.Quote(c)+" is not a campaign name", "name a folder of the fixtures folder, with no path", nil)
		}
		if err := refuseLink(filepath.Join(dir, c), c); err != nil {
			return nil, err
		}
		if info, err := fs.Stat(fsys, c); err != nil || !info.IsDir() {
			return nil, recordError("the campaign "+c+" does not exist", "check the campaign name", err)
		}
	}
	return wanted, nil
}

// planFixture reads one fixture file. It returns the reason when the fixture is
// skipped, an error when it must not be sent.
func planFixture(o RecordOptions, fsys fs.FS, filePath, campaign, fileName string) (recordJob, string, error) {
	label := campaign + "/" + strings.TrimSuffix(fileName, ".http")
	data, err := fs.ReadFile(fsys, path.Join(campaign, fileName))
	if err != nil {
		return recordJob{}, "", recordError(label+" cannot be read", "check the file", err)
	}
	f, err := nexusfixture.Parse(campaign, fileName, data)
	if err != nil {
		return recordJob{}, "", recordError(label+" is not a valid fixture", "fix the file", err)
	}
	if reason, ok := strings.CutPrefix(f.Meta.Record, "skip"); ok {
		return recordJob{}, strings.TrimSpace(strings.TrimPrefix(reason, ":")), nil
	}
	if reason := stateSkip(o.State, f.Meta.State); reason != "" {
		return recordJob{}, reason, nil
	}
	if len(f.Exchanges) != 1 && f.Meta.Flow == "" {
		return recordJob{}, fmt.Sprintf("holds %d exchanges and names no flow, so only a fixture of one exchange is recorded", len(f.Exchanges)), nil
	}
	// The file is scanned as it stands: a request header outside Host, the
	// credentials and Accept, or a query other than n and last (and, on a token
	// path, service and scope), refuses it.
	if leaks := nexusfixture.CheckLeaks(label, data, recordTerms(o)); len(leaks) > 0 {
		return recordJob{}, "", recordError(label+" is refused before it is sent: "+describeLeaks(leaks), "fix the file", nil)
	}
	// Only the first exchange is sent: the others are what a flow adds to it.
	ex := f.Exchanges[0]
	restHost, _ := restOriginKey(o.Descriptor)
	if reason, check := refusedFixtureRequest(ex, f.Meta, restHost, o.AllowCacheEffects); reason != "" {
		return recordJob{}, "", recordError(label+" asks for something the recorder does not send ("+reason+")", check, nil)
	}
	if _, ok := o.Origins[ex.Host]; !ok {
		return recordJob{}, "", recordError(label+" is recorded on "+ex.Host+", which has no origin", "give the origin of this host:port", nil)
	}
	job := recordJob{campaign: campaign, name: label, path: filePath, current: f}
	switch f.Identity {
	case nexusfixture.Admin:
		job.user, job.password = adminUserID, o.Secrets.AdminPassword
	case nexusfixture.Pull:
		job.user, job.password = pullUserID, o.Secrets.PullPassword
	case nexusfixture.BadCredentials:
		job.user, job.password = adminUserID, recordBadPassword
	}
	if job.user != "" && job.password == "" {
		return recordJob{}, "", recordError("the bench has no stored password for "+job.user+", the account of "+label, upCheck, nil)
	}
	return job, "", nil
}

// checkRunOptions refuses a state the recorder does not know and a negative
// timeout, before anything is read or sent.
func checkRunOptions(o RecordOptions) error {
	switch o.State {
	case StateNominal, StateUpstreamCut, StateAny:
	default:
		return recordError(fmt.Sprintf("the state %q is not one the recorder knows", string(o.State)), "declare nominal or upstream-cut, or no state", nil)
	}
	if o.RequestTimeout < 0 {
		return recordError("the request timeout is negative", "give a duration of at least one second, or none for 15s", nil)
	}
	return nil
}

// stateSkip returns why a fixture is not recorded in the state of the run, or "".
// declared is the "state" header of the fixture: empty means nominal, and the
// state it names must start the header (free text may follow, "upstream-cut (the
// proxy is blocked)"). A run of StateAny takes everything.
func stateSkip(run BenchState, declared string) string {
	if run == StateAny {
		return ""
	}
	have := fixtureState(declared)
	want := string(run)
	if run == StateNominal {
		want = nominalState
	}
	if have == want {
		return ""
	}
	if declared == "" {
		return fmt.Sprintf("the fixture declares no state (nominal), the run is in state %q", want)
	}
	return fmt.Sprintf("the fixture declares state %q, the run is in state %q", declared, want)
}

// nominalState names StateNominal in a "state" header and in a message.
const nominalState = "nominal"

// fixtureState is the state a "state" header names: "nominal" for none, the
// known state it starts with, or the header itself when it names none.
func fixtureState(declared string) string {
	if declared == "" {
		return nominalState
	}
	for _, state := range []string{nominalState, string(StateUpstreamCut)} {
		if rest, ok := strings.CutPrefix(declared, state); ok && (rest == "" || !isStateChar(rest[0])) {
			return state
		}
	}
	return declared
}

// isStateChar reports whether c can continue the name of a state.
func isStateChar(c byte) bool {
	return c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// checkOrigins refuses an origin that is not a loopback URL, a key that is not a
// loopback host:port of the descriptor, and a run with no REST origin.
func checkOrigins(o RecordOptions) error {
	known := descriptorOrigins(o.Descriptor)
	for key, base := range o.Origins {
		if !known[key] {
			return recordError("the origin of "+key+" is not a loopback host:port of the bench descriptor", "give origins for the hosts and ports the descriptor lists", nil)
		}
		if err := requireLoopbackBase(base); err != nil {
			return err
		}
	}
	rest, ok := restOriginKey(o.Descriptor)
	if _, have := o.Origins[rest]; !ok || !have {
		return recordError("no origin is given for the REST port of the bench, which the data set check needs", "give the REST origin of the descriptor", nil)
	}
	return nil
}

// descriptorOrigins is the set of loopback host:port pairs the descriptor
// publishes: every host of the descriptor on the REST and the Docker ports.
func descriptorOrigins(d Descriptor) map[string]bool {
	ports := []int{d.REST.NodePort}
	for _, c := range d.DockerConnectors {
		ports = append(ports, c.NodePort)
	}
	known := map[string]bool{}
	for _, h := range d.Hosts {
		host := strings.Trim(h, "[]")
		if !IsLoopbackRESTHost(host) {
			continue
		}
		for _, p := range ports {
			known[net.JoinHostPort(host, strconv.Itoa(p))] = true
		}
	}
	return known
}

// RecordOrigins builds RecordOptions.Origins from the descriptor: one entry per
// loopback host and port it publishes (the REST and the Docker ports), keyed
// host:port, valued with the http URL on restHost, which is the host of the key
// when empty.
func RecordOrigins(d Descriptor, restHost string) map[string]string {
	origins := map[string]string{}
	for key := range descriptorOrigins(d) {
		host, portText, err := net.SplitHostPort(key)
		port, convErr := strconv.Atoi(portText)
		if err != nil || convErr != nil {
			continue
		}
		if restHost != "" {
			host = restHost
		}
		origins[key] = hostURL(host, port)
	}
	return origins
}

// restOriginKey is the host:port of the REST API: the first host of the
// descriptor on the REST node port.
func restOriginKey(d Descriptor) (string, bool) {
	if len(d.Hosts) == 0 || d.REST.NodePort == 0 {
		return "", false
	}
	return net.JoinHostPort(strings.Trim(d.Hosts[0], "[]"), strconv.Itoa(d.REST.NodePort)), true
}

// The closed list of what the recorder may ask for with a GET. A manifest or a blob
// read through a proxy repository would cache an image and change the bench, so
// none is allowed, whatever the path says; the exceptions (the HEAD of a manifest,
// the blob of docker-hosted in a redirects flow) are record_request.go's, and the
// token paths are a Bearer flow's alone (record_flow.go).
var (
	recordPaths = []*regexp.Regexp{
		regexp.MustCompile(`^/v2/(_catalog|[a-z0-9._/-]+/tags/list)?$`),
		regexp.MustCompile(`^/v2/[a-z0-9._-]+(/[a-z0-9._-]+)*/_catalog$`),
		regexp.MustCompile(`^/repository/[A-Za-z0-9._-]+/v2/_catalog$`),
		regexp.MustCompile(`^/service/rest/v1/repositories(/[A-Za-z0-9._-]+)*$`),
	}
	// recordQueryParam admits one percent escape only: %2F in the value of last,
	// the form a Nexus group page Link gives the last name (DEVDESK-580).
	recordQueryParam = regexp.MustCompile(`^(n=[0-9]{1,6}|last=([A-Za-z0-9._/-]|%2[Ff])*)$`)
)

// refusedRequest returns why a request may not be sent, or "".
func refusedRequest(method, target string) string {
	p, query, hasQuery := strings.Cut(target, "?")
	switch {
	case method != http.MethodGet:
		return "the method is not GET"
	case strings.Contains(target, "..") || strings.Contains(p, "%"):
		// An escape in the query is left to recordQueryParam, which lets only %2F
		// through in the value of last.
		return "the target holds a dot-dot or an escape"
	case strings.Contains(p+"/", "/manifests/") || strings.Contains(p+"/", "/blobs/"):
		return "a manifest or a blob read can change the bench"
	}
	listed := false
	for _, re := range recordPaths {
		listed = listed || re.MatchString(p)
	}
	if !listed {
		return "the path is not in the closed list"
	}
	if hasQuery {
		for _, param := range strings.Split(query, "&") {
			if !recordQueryParam.MatchString(param) {
				return "the query holds more than n and last"
			}
		}
	}
	return ""
}

// ---- the data set of the bench ---------------------------------------------------

// checkBenchDataset compares, anonymously, what docker-hosted and docker-proxy
// list with the data set of the descriptor: a bench that does not hold it would
// record answers that are not the ones the fixtures describe. What cannot be read
// is a refusal too.
func checkBenchDataset(ctx context.Context, o RecordOptions) error {
	if len(o.Descriptor.Dataset) == 0 {
		return recordError("the descriptor lists no data set to compare the bench with", "run `mise run nexus-bench:seed`", nil)
	}
	key, _ := restOriginKey(o.Descriptor)
	base := o.Origins[key]
	for _, repo := range DatasetRepositories() {
		listed, err := ListImagesAnonymously(ctx, base, repo)
		if err != nil {
			return recordError("the listing of "+repo+" could not be read, so the data set of the bench is unknown",
				"check that the bench answers and that anonymous access is on", err)
		}
		diffs, ok := CompareDataset(DatasetIn(o.Descriptor.Dataset, repo), listed)
		if !ok {
			return recordError("the listing of "+repo+" gives no digest, so the data set of the bench cannot be compared", resetCheck, nil)
		}
		if diffs > 0 {
			return recordError(fmt.Sprintf("%s differs from the data set of the descriptor (%d image(s))", repo, diffs), resetCheck, nil)
		}
	}
	return nil
}

// DatasetRepositories are the two repositories that hold the data set. The group
// is never read: a read through it can cache an image.
func DatasetRepositories() []string {
	return []string{dockerHostedRepository, dockerProxyRepository}
}

// DatasetIn returns the images of the data set that repo holds: the hosted ones
// in docker-hosted, the ones cached in the proxy in docker-proxy.
func DatasetIn(images []DescriptorImage, repo string) []DescriptorImage {
	var out []DescriptorImage
	for _, img := range images {
		hosted := repo == dockerHostedRepository && img.Location == string(LocationHosted)
		cached := repo == dockerProxyRepository && img.CachedInProxy
		if hosted || cached {
			out = append(out, img)
		}
	}
	return out
}

// CompareDataset counts the images of a repository that are missing, have
// another digest, or are not in the data set. ok is false when the listing gives
// no digest for an image that is there, which makes the comparison impossible.
func CompareDataset(want []DescriptorImage, listed []ListedImage) (diffs int, ok bool) {
	have := make(map[string]string, len(listed))
	for _, img := range listed {
		have[img.Name+":"+img.Tag] = img.Digest
	}
	for _, img := range want {
		key := img.Name + ":" + img.Tag
		digest, found := have[key]
		delete(have, key)
		switch {
		case !found:
			diffs++
		case digest == "":
			return 0, false
		case digest != img.Digest:
			diffs++
		}
	}
	return diffs + len(have), true
}

var serverHeader = regexp.MustCompile(`^Nexus/(\S+) \(([A-Za-z]+)\)$`)

// benchVersionMatches reports whether a Server header names the version and the
// edition of the descriptor.
func benchVersionMatches(server string, d Descriptor) bool {
	m := serverHeader.FindStringSubmatch(server)
	return m != nil && m[1] == d.NexusVersion && m[2] == d.Edition
}

// declaresJSON reports whether a Content-Type is JSON.
func declaresJSON(contentType string) bool {
	media, _, err := mime.ParseMediaType(contentType)
	return err == nil && (media == "application/json" || strings.HasSuffix(media, "+json"))
}

// ---- leaks ------------------------------------------------------------------------

// recordTerms are the terms no written fixture may hold: the bench passwords,
// their Basic encodings, and the external deny list.
func recordTerms(o RecordOptions) []string {
	terms := append([]string(nil), o.DenyTerms...)
	for _, account := range []struct{ user, password string }{
		{adminUserID, o.Secrets.AdminPassword},
		{adminUserID, o.Secrets.PendingAdminPassword},
		{pullUserID, o.Secrets.PullPassword},
	} {
		if account.password == "" {
			continue
		}
		terms = append(terms, account.password,
			base64.StdEncoding.EncodeToString([]byte(account.user+":"+account.password)))
	}
	return terms
}

// describeLeaks names the rules and the lines of a leak report, never what matched.
func describeLeaks(leaks []nexusfixture.Leak) string {
	parts := make([]string, 0, len(leaks))
	for _, l := range leaks {
		parts = append(parts, fmt.Sprintf("%s at line %d", l.Rule, l.Line))
	}
	return strings.Join(parts, ", ")
}

// holdsTerm searches the raw answer, whatever the redaction would drop or hide
// (a header outside the allowlist, an HTML body), for a term in the forms it can
// take: as is, in another case, URL-escaped, JSON-escaped.
func holdsTerm(header http.Header, body []byte, terms []string) bool {
	var raw strings.Builder
	for name, values := range header {
		raw.WriteString(name + ": " + strings.Join(values, "\n") + "\n")
	}
	raw.Write(body)
	text := strings.ToLower(raw.String())
	jsonEscaper := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `/`, `\/`)
	for _, term := range terms {
		if term == "" {
			continue
		}
		for _, v := range []string{term, url.QueryEscape(term), url.PathEscape(term), jsonEscaper.Replace(term)} {
			if strings.Contains(text, strings.ToLower(v)) {
				return true
			}
		}
	}
	return false
}
