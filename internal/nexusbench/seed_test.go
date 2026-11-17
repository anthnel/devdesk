package nexusbench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
)

// The tests of Seed (DEVDESK-505, NX-02a, step 7; Plane DEVDESK-542):
//
//	Seed(ctx, SeedDeps{Runner, Forwarder, Copier, Clock, Store}, baseURL, p, out) (SeedResult, error)
//
// against a provisioned fake Nexus (fakenexus_test.go), the scripted kubectl of
// upstream_test.go, a recording port-forward, a recording image copier that models
// the three registries (upstream, docker-hosted, docker-proxy) and the fake clock.
//
// What the tests assume, to settle with the code (the choices are the tester's):
//   - Order: credentials from the store (admin, then bench-pull; nothing else is
//     touched when one is missing) -> the state of the upstream (ReadUpstream, read
//     only) -> OCI layout in a temporary folder -> port-forward to
//     svc/bench-upstream:5000 -> the upstream images, in the order of Dataset() ->
//     the port-forward is stopped -> the docker-hosted images, in the order of
//     Dataset() -> the PullThrough images through docker-proxy -> the two listings.
//   - An image is copied only when the manifest digest read at its destination
//     (ImageCopier.ManifestDigest) differs from the one of the data set. A
//     ManifestDigest error of kind SeedFailed ("manifest unknown") means "absent";
//     any other error stops the seed. Every Copy carries PreserveDigests.
//   - Credentials: no credentials for the upstream (no authentication), the stored
//     admin for docker-hosted, the stored bench-pull for docker-proxy. The proxy is
//     only ever read for a PullThrough image (F9): a read of mirror/uncached through
//     it would cache it. The group is never touched.
//   - A PullThrough image already listed by docker-proxy with the right digest is
//     not pulled again (the second run copies nothing). How Seed knows (a REST
//     listing, or a read through the proxy) is left to the code.
//   - NX-02a: a cut upstream is an error (kind SeedFailed, "the upstream is cut")
//     without any write: restoring it is NX-02b. An upstream that is missing or still
//     starting is a SeedFailed too, and an upstream URL that is not the bench's is
//     refused (it names NEXUS_BENCH_UPSTREAM_URL): the bench would push to a registry
//     the proxy does not read. Seed never scales, never applies, never writes to
//     Nexus.
//   - The final listings are read by REST with the admin account, after every copy.
//     docker-proxy must list exactly the PullThrough images, docker-hosted exactly
//     the hosted ones (name and tag; a digest the instance lists must be the one of
//     the data set). A missing component is waited for, by Clock, 30 s at most (the
//     same bound for both repositories, H-COMPONENTS), then it is a SeedFailed. A
//     component outside the data set is a SeedFailed at once, without waiting, that
//     names the repository and says to run `mise run nexus-bench:reset`.
//   - Every failure is a *BenchError; the error of the copier, of the forwarder or
//     of the REST client stays reachable with errors.Is/As and is never printed.
//     A cancelled context is returned so that errors.Is(err, context.Canceled) holds.
//   - The port-forward is opened once per Seed, stopped on every path, and its
//     address must be a loopback literal or Seed refuses it before any copy.
//   - The temporary folders (layout, pull-through target) are under TMPDIR and gone
//     when Seed returns. Output: lines that start with "seed: ", no password. A nil
//     writer is accepted.
//   - SeedResult.Images is Dataset(): every digest filled, PullThrough true exactly
//     for the images cached in docker-proxy (DatasetImage has no CachedInProxy field).

const (
	// seedForwardLocal is the loopback address the fake port-forward hands out.
	seedForwardLocal = "127.0.0.1:41234"
	// seedWaitBound is the longest wait for a listing (H-COMPONENTS).
	seedWaitBound = 30 * time.Second

	whereUpstream = "upstream"
	whereHosted   = "hosted"
	whereProxy    = "proxy"
)

// ---- the references -----------------------------------------------------------

func TestSeedNexusImageRef(t *testing.T) {
	custom := Defaults()
	custom.HTTPNodePort = 31999
	tests := []struct {
		name       string
		p          Params
		repository string
		img        DatasetImage
		want       string
	}{
		{"hosted", Defaults(), dockerHostedRepository, DatasetImage{Name: "api", Tag: "1.0.0"}, "127.0.0.1:30481/docker-hosted/api:1.0.0"},
		{"hosted, a name of two components", Defaults(), dockerHostedRepository, DatasetImage{Name: "team/api", Tag: "1.0.0"}, "127.0.0.1:30481/docker-hosted/team/api:1.0.0"},
		{"proxy", Defaults(), dockerProxyRepository, DatasetImage{Name: "mirror/cached", Tag: "1.0"}, "127.0.0.1:30481/docker-proxy/mirror/cached:1.0"},
		{"the HTTP NodePort of the parameters", custom, dockerHostedRepository, DatasetImage{Name: "api", Tag: "1.1.0"}, "127.0.0.1:31999/docker-hosted/api:1.1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nexusImageRef(tt.p, tt.repository, tt.img)
			if got.Transport != "docker" || got.Location != tt.want {
				t.Errorf("nexusImageRef = %+v, want docker %q", got, tt.want)
			}
			if err := checkSide(got, nil); err != nil {
				t.Errorf("the copier would refuse the reference: %v", err)
			}
		})
	}
}

func TestSeedUpstreamImageRef(t *testing.T) {
	got := upstreamImageRef(seedForwardLocal, DatasetImage{Name: "shared/base", Tag: "1.0"})
	if got.Transport != "docker" || got.Location != "127.0.0.1:41234/shared/base:1.0" {
		t.Errorf("upstreamImageRef = %+v, want docker 127.0.0.1:41234/shared/base:1.0", got)
	}
	if err := checkSide(got, nil); err != nil {
		t.Errorf("the copier would refuse the reference: %v", err)
	}
}

// ---- doubles ------------------------------------------------------------------

type forwardCall struct {
	namespace, service string
	port               int
}

// fakeForwarder is the PortForwarder: it records the calls and says whether the
// forward is open.
type fakeForwarder struct {
	t     *testing.T
	log   *orderLog
	local string
	err   error

	calls  []forwardCall
	active bool
	stops  int
}

var _ PortForwarder = (*fakeForwarder)(nil)

func (f *fakeForwarder) Forward(ctx context.Context, namespace, service string, port int) (string, func() error, error) {
	f.calls = append(f.calls, forwardCall{namespace, service, port})
	f.log.add("forward")
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if f.err != nil {
		return "", nil, f.err
	}
	f.active = true
	return f.local, f.stop, nil
}

func (f *fakeForwarder) stop() error {
	f.stops++
	f.active = false
	f.log.add("stop")
	return nil
}

// loggedRunner adds the calls of kubectl to the event log.
type loggedRunner struct {
	Runner
	log *orderLog
}

func (r loggedRunner) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, []byte, error) {
	r.log.add("kubectl")
	return r.Runner.Run(ctx, stdin, args...)
}

type refInfo struct {
	where string // whereUpstream, whereHosted or whereProxy
	img   DatasetImage
}

type copyCall struct {
	src, dst ImageRef
	opts     CopyOptions
	where    string // the registry the call touches
}

type digestCall struct {
	ref   ImageRef
	creds *Credentials
	where string
}

// seedCopier is the ImageCopier of the tests. It models the three registries the
// way the real ones behave: the upstream (reached only while the port-forward is
// open, no authentication), docker-hosted (admin) and docker-proxy (bench-pull, a
// read pulls the image through and caches it). Hosted and proxy contents are
// mirrored into the fake Nexus listing. It reads the layout Seed wrote, so a copy
// puts the digest of the image the layout says, and a wrong reference to the
// layout shows.
type seedCopier struct {
	t     *testing.T
	log   *orderLog
	fwd   *fakeForwarder
	nexus *fakeNexus
	p     *Params
	tmp   string

	admin, pull Credentials

	index map[string]refInfo // imageArg -> what it is

	upstream, hosted, proxyCache map[string]string // "name:tag" -> manifest digest
	listing                      map[string][]fakeComponent
	hidden                       map[string]bool // repositories whose listing is not published yet

	copies    []copyCall
	digests   []digestCall
	ociCopies int

	// absentText is the output of skopeo the fake reports for an absent image.
	absentText string
	// blind lists the repositories whose listing carries no digest (H-COMPONENTS:
	// the instance may not list the manifest asset).
	blind map[string]bool

	// copyErr, when set, runs at the start of the n-th Copy (1-based); a non-nil
	// answer is the result of the Copy.
	copyErr func(n int, c copyCall) error
	// digestErr, when set, runs at the start of every ManifestDigest.
	digestErr func(c digestCall) error
}

var _ ImageCopier = (*seedCopier)(nil)

// absentManifestText is what skopeo prints for an image that is not there.
const absentManifestText = textManifestUnknown

// absent is the error ManifestDigest returns for an image that is not in the
// registry: built from the output of skopeo by the code of the real copier, not
// from a kind. absentText (default: manifest unknown) says which output.
func (c *seedCopier) absent() error {
	text := c.absentText
	if text == "" {
		text = absentManifestText
	}
	return inspectFailure(text, errFakeExit1)
}

func refusedImage() error {
	return inspectFailure("FATA[0000] reading manifest 1.0 in 127.0.0.1:41234/x: unauthorized: authentication required", errFakeExit1)
}

func nameTag(img DatasetImage) string { return img.Name + ":" + img.Tag }

func sameCreds(got, want *Credentials) bool {
	if got == nil || want == nil {
		return got == want
	}
	return got.User == want.User && got.Password == want.Password
}

func (c *seedCopier) lookup(ref ImageRef) (refInfo, bool) {
	if c.index == nil {
		c.index = map[string]refInfo{}
		for _, img := range Dataset() {
			switch img.Location {
			case LocationHosted:
				c.index[imageArg(nexusImageRef(*c.p, dockerHostedRepository, img))] = refInfo{whereHosted, img}
			case LocationUpstream:
				c.index[imageArg(upstreamImageRef(c.fwd.local, img))] = refInfo{whereUpstream, img}
				c.index[imageArg(nexusImageRef(*c.p, dockerProxyRepository, img))] = refInfo{whereProxy, img}
			}
		}
	}
	info, ok := c.index[imageArg(ref)]
	return info, ok
}

// whereOf is the registry a copy touches: the docker side of it.
func (c *seedCopier) whereOf(refs ...ImageRef) (string, string) {
	for _, r := range refs {
		if r.Transport != "docker" {
			continue
		}
		if info, ok := c.lookup(r); ok {
			return info.where, nameTag(info.img)
		}
		return "unknown", r.Location
	}
	return "none", ""
}

// authorize checks the credentials offered to a registry.
func (c *seedCopier) authorize(where string, creds *Credentials) error {
	var want *Credentials
	switch where {
	case whereHosted:
		want = &c.admin
	case whereProxy:
		want = &c.pull
	}
	if !sameCreds(creds, want) {
		c.t.Errorf("the %s registry was offered the wrong credentials", where)
		return refusedImage()
	}
	return nil
}

// reach fails the way a closed port-forward does.
func (c *seedCopier) reach(where string) error {
	if where == whereUpstream && !c.fwd.active {
		c.t.Errorf("the upstream was reached while the port-forward is not open")
		return &BenchError{Kind: RegistryUnreachable, Cause: "connection refused", Check: "start the port-forward"}
	}
	return nil
}

// read returns the manifest digest of ref the way a registry (or a layout) would.
func (c *seedCopier) read(ref ImageRef, creds *Credentials) (string, error) {
	if ref.Transport == "oci" {
		return c.readLayout(ref)
	}
	info, ok := c.lookup(ref)
	if !ok || ref.Transport != "docker" {
		c.t.Errorf("read of %s: not a reference of the data set", imageArg(ref))
		return "", c.absent()
	}
	if err := c.reach(info.where); err != nil {
		return "", err
	}
	if err := c.authorize(info.where, creds); err != nil {
		return "", err
	}
	nt := nameTag(info.img)
	switch info.where {
	case whereUpstream:
		if d, ok := c.upstream[nt]; ok {
			return d, nil
		}
	case whereHosted:
		if d, ok := c.hosted[nt]; ok {
			return d, nil
		}
	case whereProxy:
		if !info.img.PullThrough {
			c.t.Errorf("F9: %s was read through docker-proxy, which caches it: only PullThrough images may be", nt)
		}
		if d, ok := c.proxyCache[nt]; ok {
			return d, nil
		}
		if d, ok := c.upstream[nt]; ok {
			c.preload(whereProxy, info.img, d)
			return d, nil
		}
	}
	return "", c.absent()
}

// readLayout reads the digest of an image of the OCI layout "<dir>:<ref>".
func (c *seedCopier) readLayout(ref ImageRef) (string, error) {
	i := strings.LastIndex(ref.Location, ":")
	if i < 0 {
		c.t.Errorf("oci reference %q has no image name", ref.Location)
		return "", c.absent()
	}
	dir, name := ref.Location[:i], ref.Location[i+1:]
	if !strings.HasPrefix(dir, c.tmp+string(os.PathSeparator)) {
		c.t.Errorf("the layout %s is not in a temporary folder under TMPDIR (%s)", dir, c.tmp)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		c.t.Errorf("the layout is not readable when the copy starts: %v", err)
		return "", c.absent()
	}
	var idx struct {
		Manifests []struct {
			Digest      string            `json:"digest"`
			Annotations map[string]string `json:"annotations"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(raw, &idx); err != nil {
		c.t.Errorf("index.json of the layout is not JSON: %v", err)
		return "", c.absent()
	}
	for _, m := range idx.Manifests {
		if m.Annotations[annotationRefName] == name {
			c.ociCopies++
			return m.Digest, nil
		}
	}
	c.t.Errorf("the layout holds no image called %q", name)
	return "", c.absent()
}

// Copy implements ImageCopier.
func (c *seedCopier) Copy(ctx context.Context, src, dst ImageRef, o CopyOptions) error {
	c.t.Helper()
	where, nt := c.whereOf(dst, src)
	call := copyCall{src: src, dst: dst, opts: o, where: where}
	c.copies = append(c.copies, call)
	c.log.add("copy " + where + " " + nt)
	if c.copyErr != nil {
		if err := c.copyErr(len(c.copies), call); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return &BenchError{Kind: Unclassified, Cause: "skopeo copy was interrupted", Check: "run the command again", Err: err}
	}
	if !o.PreserveDigests {
		c.t.Errorf("copy of %s without PreserveDigests", nt)
	}
	for _, side := range []struct {
		ref   ImageRef
		creds *Credentials
	}{{src, o.SrcCreds}, {dst, o.DestCreds}} {
		if err := checkSide(side.ref, side.creds); err != nil {
			c.t.Errorf("the real copier would refuse %s before any process: %v", imageArg(side.ref), err)
			return err
		}
	}
	digest, err := c.read(src, o.SrcCreds)
	if err != nil {
		return err
	}
	if !o.PreserveDigests {
		sum := sha256.Sum256([]byte(digest))
		digest = "sha256:" + hex.EncodeToString(sum[:])
	}
	return c.write(dst, src, digest, o.DestCreds)
}

func (c *seedCopier) write(dst, src ImageRef, digest string, creds *Credentials) error {
	switch dst.Transport {
	case "dir":
		if !strings.HasPrefix(dst.Location, c.tmp+string(os.PathSeparator)) {
			c.t.Errorf("the pull-through target %s is not under TMPDIR (%s)", dst.Location, c.tmp)
		}
		return os.MkdirAll(dst.Location, 0o700)
	case "docker":
	default:
		c.t.Errorf("destination transport %q", dst.Transport)
		return c.absent()
	}
	info, ok := c.lookup(dst)
	if !ok {
		c.t.Errorf("write to %s: not a reference of the data set", imageArg(dst))
		return c.absent()
	}
	if err := c.reach(info.where); err != nil {
		return err
	}
	if info.where == whereProxy {
		c.t.Errorf("a write through docker-proxy: it is a proxy, nothing is pushed to it")
		return refusedImage()
	}
	if err := c.authorize(info.where, creds); err != nil {
		return err
	}
	if src.Transport == "oci" && digest != info.img.Digest {
		c.t.Errorf("the copy to %s %s would store %s, the data set says %s: the layout reference of another image was used",
			info.where, nameTag(info.img), digest, info.img.Digest)
	}
	if info.where == whereUpstream {
		c.upstream[nameTag(info.img)] = digest
		return nil
	}
	c.preload(whereHosted, info.img, digest)
	return nil
}

// ManifestDigest implements ImageCopier.
func (c *seedCopier) ManifestDigest(_ context.Context, ref ImageRef, creds *Credentials) (string, error) {
	where, nt := c.whereOf(ref)
	call := digestCall{ref: ref, creds: creds, where: where}
	c.digests = append(c.digests, call)
	c.log.add("digest " + where + " " + nt)
	if c.digestErr != nil {
		if err := c.digestErr(call); err != nil {
			return "", err
		}
	}
	if err := checkSide(ref, creds); err != nil {
		c.t.Errorf("the real copier would refuse %s before any process: %v", imageArg(ref), err)
		return "", err
	}
	return c.read(ref, creds)
}

// ---- the state of the registries

// preload puts an image, with a digest, in a registry as an earlier run left it.
func (c *seedCopier) preload(where string, img DatasetImage, digest string) {
	nt := nameTag(img)
	switch where {
	case whereUpstream:
		c.upstream[nt] = digest
	case whereHosted:
		c.hosted[nt] = digest
		c.setComponent(dockerHostedRepository, img, digest)
	case whereProxy:
		c.proxyCache[nt] = digest
		c.setComponent(dockerProxyRepository, img, digest)
	}
}

// preloadAll is the registries as a complete seed leaves them.
func (c *seedCopier) preloadAll() {
	for _, img := range Dataset() {
		switch img.Location {
		case LocationHosted:
			c.preload(whereHosted, img, img.Digest)
		case LocationUpstream:
			c.preload(whereUpstream, img, img.Digest)
			if img.PullThrough {
				c.preload(whereProxy, img, img.Digest)
			}
		}
	}
}

// removeUpstream takes an image out of the upstream.
func (c *seedCopier) removeUpstream(img DatasetImage) { delete(c.upstream, nameTag(img)) }

func (c *seedCopier) setComponent(repo string, img DatasetImage, digest string) {
	comp := fakeComponent{Name: img.Name, Version: img.Tag, Digest: digest}
	list := c.listing[repo]
	if i := slices.IndexFunc(list, func(x fakeComponent) bool { return x.Name == comp.Name && x.Version == comp.Version }); i >= 0 {
		list[i] = comp
	} else {
		list = append(list, comp)
	}
	c.listing[repo] = list
	c.publish(repo)
}

// addExtra lists a component outside the data set.
func (c *seedCopier) addExtra(repo string, comp fakeComponent) {
	c.listing[repo] = append(c.listing[repo], comp)
	c.publish(repo)
}

func (c *seedCopier) publish(repo string) {
	if c.hidden[repo] {
		return
	}
	list := slices.Clone(c.listing[repo])
	if c.blind[repo] {
		for i := range list {
			list[i].Digest = ""
		}
	}
	c.nexus.SetComponents(repo, list...)
}

// blindListing makes a repository list its components without a digest.
func (c *seedCopier) blindListing(repo string) {
	c.blind[repo] = true
	c.publish(repo)
}

// setListedDigest changes the digest a repository lists for img, and nothing else.
func (c *seedCopier) setListedDigest(repo string, img DatasetImage, digest string) {
	for i, comp := range c.listing[repo] {
		if comp.Name == img.Name && comp.Version == img.Tag {
			c.listing[repo][i].Digest = digest
		}
	}
	c.publish(repo)
}

// hide stops the instance from listing what a repository holds, until reveal.
func (c *seedCopier) hide(repo string) { c.hidden[repo] = true }

func (c *seedCopier) reveal(repo string) {
	c.hidden[repo] = false
	c.publish(repo)
}

// SetComponents replaces the listing of an existing repository.
func (f *fakeNexus) SetComponents(repo string, comps ...fakeComponent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.repoExists(repo) {
		f.t.Fatalf("fake Nexus: no repository %q to set the components of", repo)
	}
	f.components[repo] = slices.Clone(comps)
}

// ---- one scenario

type seedEnv struct {
	t        *testing.T
	b        *bench
	p        Params
	clk      *fakeClock
	kube     *upstreamKubectl
	fwd      *fakeForwarder
	cp       *seedCopier
	log      *orderLog
	tmp      string
	baseline int // requests the fake Nexus had received before the seed
	runs     int
	out      *bytes.Buffer
	nilOut   bool
	deps     SeedDeps
}

// readyUpstream is a Deployment at one replica with a ready pod.
func readyUpstream() upstreamRound { return roundOf(1, readyPod()) }

// newSeedEnv is a provisioned bench with an empty upstream behind the round the
// cluster plays. The temporary folder is private to the test: TMPDIR points at it.
func newSeedEnv(t *testing.T, round upstreamRound, mutate ...func(*Params)) *seedEnv {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	b := provisioned(t)
	p := upstreamParams()
	for _, m := range mutate {
		m(&p)
	}
	log := &orderLog{}
	clk := newFakeClock(t)
	e := &seedEnv{t: t, b: b, p: p, clk: clk, log: log, tmp: tmp, baseline: len(b.nexus.Requests()), out: &bytes.Buffer{}}
	e.kube = newUpstreamKubectl(t, e.p, clk, []upstreamRound{round}, nil)
	e.fwd = &fakeForwarder{t: t, log: log, local: seedForwardLocal}
	sec := b.store.current()
	e.cp = &seedCopier{
		t: t, log: log, fwd: e.fwd, nexus: b.nexus, p: &e.p, tmp: tmp,
		admin:      Credentials{User: adminUserID, Password: sec.AdminPassword},
		pull:       Credentials{User: pullUserID, Password: sec.PullPassword},
		upstream:   map[string]string{},
		hosted:     map[string]string{},
		proxyCache: map[string]string{},
		listing:    map[string][]fakeComponent{},
		hidden:     map[string]bool{},
		blind:      map[string]bool{},
	}
	e.deps = SeedDeps{Runner: loggedRunner{e.kube, log}, Forwarder: e.fwd, Copier: e.cp, Clock: clk, Store: b.store}
	if sec.PullPassword == "" {
		t.Fatal("the provisioned bench stored no bench-pull password")
	}
	return e
}

// run calls Seed (with a context that ends the test if the code hangs) and checks
// what must hold whatever the outcome.
func (e *seedEnv) run() (SeedResult, error) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
	defer cancel()
	return e.runWith(ctx)
}

func (e *seedEnv) runWith(ctx context.Context) (SeedResult, error) {
	e.t.Helper()
	var out io.Writer = e.out
	if e.nilOut {
		out = nil
	}
	e.runs++
	res, err := Seed(ctx, e.deps, e.b.nexus.URL(), e.p, out)
	e.checkInvariants(err)
	return res, err
}

func (e *seedEnv) secrets() []string {
	return append(e.b.knownSecrets(), fakeSecret)
}

// checkInvariants are the rules that hold after every call, success or failure.
func (e *seedEnv) checkInvariants(err error) {
	e.t.Helper()
	// Seed reads Nexus; it never writes to it, and always as the admin account.
	for _, r := range e.b.nexus.Requests()[e.baseline:] {
		if r.Method != http.MethodGet {
			e.t.Errorf("Seed sent %s %s to Nexus: it only reads", r.Method, r.Path)
		}
		if r.User != adminUserID {
			e.t.Errorf("Seed sent %s %s as %q, want the admin account", r.Method, r.Path, r.User)
		}
	}
	// Seed reads the cluster and writes nothing (NX-02a: it never restores the upstream).
	if len(e.kube.writes) != 0 {
		e.t.Errorf("Seed wrote to the cluster: %q", e.kube.writes)
	}
	e.kube.requireNoPreflight()
	// One port-forward per run, always stopped.
	if len(e.fwd.calls) > e.runs {
		e.t.Errorf("%d port-forwards for %d runs", len(e.fwd.calls), e.runs)
	}
	for _, call := range e.fwd.calls {
		if want := (forwardCall{e.p.Namespace, "bench-upstream", 5000}); call != want {
			e.t.Errorf("port-forward to %+v, want %+v", call, want)
		}
	}
	if e.fwd.active {
		e.t.Error("the port-forward is still open when Seed returns")
	}
	if opened := e.openedForwards(); e.fwd.stops < opened {
		e.t.Errorf("%d forwards opened, stop called %d times", opened, e.fwd.stops)
	}
	// Temporary folders.
	if left, rerr := os.ReadDir(e.tmp); rerr != nil || len(left) != 0 {
		names := make([]string, 0, len(left))
		for _, f := range left {
			names = append(names, f.Name())
		}
		e.t.Errorf("temporary files left under TMPDIR: %v (%v)", names, rerr)
	}
	e.requireQuiet(err)
}

// openedForwards counts the forwards that succeeded.
func (e *seedEnv) openedForwards() int {
	n := 0
	for _, ev := range e.log.snapshot() {
		if ev == "forward" {
			n++
		}
	}
	if e.fwd.err != nil || n > len(e.fwd.calls) {
		return 0
	}
	return n
}

// requireQuiet fails when a password is in what Seed said, in its error, or in
// anything a process would be given on its command line.
func (e *seedEnv) requireQuiet(err error) {
	e.t.Helper()
	texts := map[string]string{"output": e.out.String(), "kubectl": strings.Join(e.kube.lines, "\n")}
	if err != nil {
		texts["error"] = err.Error()
	}
	var argv []string
	for _, c := range e.cp.copies {
		argv = append(argv, imageArg(c.src), imageArg(c.dst))
	}
	for _, c := range e.cp.digests {
		argv = append(argv, imageArg(c.ref))
	}
	for _, f := range e.fwd.calls {
		argv = append(argv, f.namespace, f.service, fmt.Sprint(f.port))
	}
	texts["argv"] = strings.Join(argv, "\n")
	for label, text := range texts {
		for _, secret := range e.secrets() {
			if secret != "" && strings.Contains(text, secret) {
				e.t.Errorf("the %s contains a password (%d characters): %q", label, len(secret), text)
			}
		}
	}
}

func (e *seedEnv) mustRun() SeedResult {
	e.t.Helper()
	res, err := e.run()
	if err != nil {
		e.t.Fatalf("Seed: %v", err)
	}
	return res
}

// requireNoProgress checks Seed stopped before it touched anything.
func (e *seedEnv) requireNoProgress() {
	e.t.Helper()
	if len(e.fwd.calls) != 0 {
		e.t.Errorf("port-forward opened: %+v", e.fwd.calls)
	}
	if len(e.cp.copies) != 0 || len(e.cp.digests) != 0 {
		e.t.Errorf("the copier was used: %d copies, %d reads", len(e.cp.copies), len(e.cp.digests))
	}
}

func requireKind(t *testing.T, err error, kind ErrKind) *BenchError {
	t.Helper()
	var be *BenchError
	if !errors.As(err, &be) {
		t.Fatalf("error = %#v, want a *BenchError", err)
	}
	if be.Kind != kind {
		t.Fatalf("Kind = %d, want %d; error: %v", be.Kind, kind, err)
	}
	return be
}

func datasetImage(t *testing.T, name, tag string, loc Location) DatasetImage {
	t.Helper()
	for _, img := range Dataset() {
		if img.Name == name && img.Tag == tag && img.Location == loc {
			return img
		}
	}
	t.Fatalf("the data set has no %s:%s in %s", name, tag, loc)
	return DatasetImage{}
}

func imagesIn(loc Location) []DatasetImage {
	var out []DatasetImage
	for _, img := range Dataset() {
		if img.Location == loc {
			out = append(out, img)
		}
	}
	return out
}

func pullThrough() []DatasetImage {
	var out []DatasetImage
	for _, img := range Dataset() {
		if img.PullThrough {
			out = append(out, img)
		}
	}
	return out
}

// wantCopy is one expected Copy.
type wantCopy struct {
	srcTransport string    // "oci" or "docker"
	src          *ImageRef // for docker sources
	dst          ImageRef
	dirDst       bool
	srcCreds     *Credentials
	destCreds    *Credentials
}

// fullSequence is the Copy calls of a first run, in order.
func (e *seedEnv) fullSequence() []wantCopy {
	admin, pull := e.cp.admin, e.cp.pull
	var out []wantCopy
	for _, img := range imagesIn(LocationUpstream) {
		out = append(out, wantCopy{srcTransport: "oci", dst: upstreamImageRef(e.fwd.local, img)})
	}
	for _, img := range imagesIn(LocationHosted) {
		out = append(out, wantCopy{srcTransport: "oci", dst: nexusImageRef(e.p, dockerHostedRepository, img), destCreds: &admin})
	}
	for _, img := range pullThrough() {
		src := nexusImageRef(e.p, dockerProxyRepository, img)
		out = append(out, wantCopy{srcTransport: "docker", src: &src, dirDst: true, srcCreds: &pull})
	}
	return out
}

func (e *seedEnv) requireCopies(want []wantCopy) {
	e.t.Helper()
	got := e.cp.copies
	if len(got) != len(want) {
		e.t.Fatalf("%d copies, want %d:\n%s", len(got), len(want), describeCopies(got))
	}
	for i, w := range want {
		g := got[i]
		switch {
		case g.src.Transport != w.srcTransport:
			e.t.Errorf("copy #%d: source transport %q, want %q", i+1, g.src.Transport, w.srcTransport)
		case w.src != nil && g.src != *w.src:
			e.t.Errorf("copy #%d: source %v, want %v", i+1, g.src, *w.src)
		}
		if w.dirDst {
			if g.dst.Transport != "dir" || !strings.HasPrefix(g.dst.Location, e.tmp) {
				e.t.Errorf("copy #%d: destination %v, want a dir under %s", i+1, g.dst, e.tmp)
			}
		} else if g.dst != w.dst {
			e.t.Errorf("copy #%d: destination %v, want %v", i+1, g.dst, w.dst)
		}
		if !sameCreds(g.opts.SrcCreds, w.srcCreds) || !sameCreds(g.opts.DestCreds, w.destCreds) {
			e.t.Errorf("copy #%d: source credentials set=%t, destination credentials set=%t, want %t and %t",
				i+1, g.opts.SrcCreds != nil, g.opts.DestCreds != nil, w.srcCreds != nil, w.destCreds != nil)
		}
		if !g.opts.PreserveDigests {
			e.t.Errorf("copy #%d: no --preserve-digests", i+1)
		}
	}
}

func describeCopies(calls []copyCall) string {
	var b strings.Builder
	for i, c := range calls {
		fmt.Fprintf(&b, "  %d. %s -> %s\n", i+1, imageArg(c.src), imageArg(c.dst))
	}
	return b.String()
}

// requirePhases checks the order of the events: every read of the upstream is
// inside the port-forward, and the upstream, then docker-hosted, then docker-proxy
// are done one after the other.
func requirePhases(t *testing.T, events []string) {
	t.Helper()
	rank := map[string]int{whereUpstream: 0, whereHosted: 1, whereProxy: 2}
	forward, stop := slices.Index(events, "forward"), slices.Index(events, "stop")
	last := -1
	for i, ev := range events {
		fields := strings.Fields(ev)
		if fields[0] != "copy" && fields[0] != "digest" {
			continue
		}
		r, ok := rank[fields[1]]
		if !ok {
			t.Errorf("event %q touches nothing of the data set", ev)
			continue
		}
		if r < last {
			t.Errorf("event %q comes after an event of a later registry", ev)
		}
		last = max(last, r)
		if fields[1] == whereUpstream && (forward < 0 || i < forward || (stop >= 0 && i > stop)) {
			t.Errorf("event %q is not inside the port-forward (forward at %d, stop at %d, event at %d)", ev, forward, stop, i)
		}
		if fields[1] != whereUpstream && (stop < 0 || i < stop) {
			t.Errorf("event %q comes before the port-forward is stopped (stop at %d, event at %d)", ev, stop, i)
		}
	}
}

// requireListing checks what a repository lists, through the REST client.
func (e *seedEnv) requireListing(repo string, want []DatasetImage) {
	e.t.Helper()
	c := e.b.adminClient()
	got, err := e.b.list(c, repo)
	if err != nil {
		e.t.Fatalf("listComponents(%s): %v", repo, err)
	}
	have := map[string]string{}
	for _, comp := range got {
		have[comp.Name+":"+comp.Version] = comp.Digest
	}
	if len(have) != len(want) {
		e.t.Errorf("%s lists %v, want %d images", repo, have, len(want))
	}
	for _, img := range want {
		if d, ok := have[nameTag(img)]; !ok || d != img.Digest {
			e.t.Errorf("%s lists %s with digest %q, want %q", repo, nameTag(img), d, img.Digest)
		}
	}
}

// ---- the first run -------------------------------------------------------------

func TestSeedFirstRun(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*Params)
	}{
		{"default ports", func(*Params) {}},
		{"another HTTP NodePort", func(p *Params) { p.HTTPNodePort = 31999 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream(), tt.mutate)
			res := e.mustRun()

			// The exact sequence of copies: the upstream images, then docker-hosted,
			// then the pull-through, each in the order of the data set.
			e.requireCopies(e.fullSequence())
			requirePhases(t, e.log.snapshot())
			if e.cp.ociCopies != len(imagesIn(LocationUpstream))+len(imagesIn(LocationHosted)) {
				t.Errorf("%d copies read the layout, want one per pushed image", e.cp.ociCopies)
			}

			// The state of the cluster is read before the port-forward opens.
			events := e.log.snapshot()
			forward := slices.Index(events, "forward")
			for i, ev := range events {
				if ev == "kubectl" && i > forward {
					t.Errorf("kubectl is called after the port-forward opened (event %d)", i)
				}
			}
			if forward < 0 || !slices.Contains(events[:forward], "kubectl") {
				t.Errorf("the upstream was not read before the port-forward: %v", events)
			}

			// Every docker reference is on the loopback, on the forward or the NodePort.
			hosts := []string{e.fwd.local, fmt.Sprintf("127.0.0.1:%d", e.p.HTTPNodePort)}
			for _, c := range e.cp.copies {
				for _, ref := range []ImageRef{c.src, c.dst} {
					if ref.Transport == "docker" && !slices.Contains(hosts, registryHost(ref.Location)) {
						t.Errorf("reference %s is not on %v", imageArg(ref), hosts)
					}
				}
			}

			if !reflect.DeepEqual(res.Images, Dataset()) {
				t.Errorf("Images = %+v\nwant the data set %+v", res.Images, Dataset())
			}
			e.requireListing(dockerHostedRepository, imagesIn(LocationHosted))
			e.requireListing(dockerProxyRepository, pullThrough())

			lines := strings.Split(strings.TrimRight(e.out.String(), "\n"), "\n")
			if e.out.Len() == 0 {
				t.Fatal("Seed said nothing")
			}
			for _, line := range lines {
				if !strings.HasPrefix(line, "seed: ") {
					t.Errorf("output line %q does not start with \"seed: \"", line)
				}
			}
		})
	}
}

func TestSeedAcceptsANilWriter(t *testing.T) {
	e := newSeedEnv(t, readyUpstream())
	e.nilOut = true
	e.mustRun()
}

func TestSeedNeverReadsTheUncachedImageThroughTheProxy(t *testing.T) {
	e := newSeedEnv(t, readyUpstream())
	e.mustRun()
	uncached := datasetImage(t, "mirror/uncached", "1.0", LocationUpstream)
	if uncached.PullThrough {
		t.Fatal("mirror/uncached must not be PullThrough")
	}
	for _, ev := range e.log.snapshot() {
		if ev == "copy proxy "+nameTag(uncached) || ev == "digest proxy "+nameTag(uncached) {
			t.Errorf("event %q: reading it through docker-proxy would cache it (F9)", ev)
		}
	}
	e.requireListing(dockerProxyRepository, pullThrough())
}

// ---- idempotence and repair ----------------------------------------------------

func TestSeedSecondRunCopiesNothing(t *testing.T) {
	e := newSeedEnv(t, readyUpstream())
	first := e.mustRun()
	copiesBefore, digestsBefore := len(e.cp.copies), len(e.cp.digests)
	e.out.Reset()

	second := e.mustRun()
	if got := e.cp.copies[copiesBefore:]; len(got) != 0 {
		t.Fatalf("the second run copied:\n%s", describeCopies(got))
	}
	if !reflect.DeepEqual(second, first) {
		t.Errorf("second result %+v differs from the first %+v", second, first)
	}
	// The digests really were compared: one read per pushed image.
	for _, loc := range []Location{LocationUpstream, LocationHosted} {
		where := whereUpstream
		if loc == LocationHosted {
			where = whereHosted
		}
		for _, img := range imagesIn(loc) {
			want := imageArg(e.refOf(where, img))
			found := slices.ContainsFunc(e.cp.digests[digestsBefore:], func(c digestCall) bool { return imageArg(c.ref) == want })
			if !found {
				t.Errorf("the second run did not read the digest of %s in %s", nameTag(img), where)
			}
		}
	}
	if len(e.fwd.calls) != 2 {
		t.Errorf("%d port-forwards for two runs, want 2", len(e.fwd.calls))
	}
}

// refOf is the reference of img in where.
func (e *seedEnv) refOf(where string, img DatasetImage) ImageRef {
	switch where {
	case whereUpstream:
		return upstreamImageRef(e.fwd.local, img)
	case whereHosted:
		return nexusImageRef(e.p, dockerHostedRepository, img)
	}
	return nexusImageRef(e.p, dockerProxyRepository, img)
}

func TestSeedCopiesOnlyWhatDiffers(t *testing.T) {
	apiHosted := func(t *testing.T) DatasetImage { return datasetImage(t, "api", "1.0.0", LocationHosted) }
	tests := []struct {
		name string
		// arrange changes the registries away from a complete seed; want lists the
		// destinations of the copies that must follow, in order.
		arrange func(t *testing.T, e *seedEnv)
		want    func(t *testing.T, e *seedEnv) []ImageRef
	}{
		{
			"a hosted tag with another digest",
			func(t *testing.T, e *seedEnv) { e.cp.preload(whereHosted, apiHosted(t), digestAlpine) },
			func(t *testing.T, e *seedEnv) []ImageRef {
				return []ImageRef{nexusImageRef(e.p, dockerHostedRepository, apiHosted(t))}
			},
		},
		{
			"an image missing from the upstream",
			func(t *testing.T, e *seedEnv) {
				e.cp.removeUpstream(datasetImage(t, "mirror/uncached", "1.0", LocationUpstream))
			},
			func(t *testing.T, e *seedEnv) []ImageRef {
				return []ImageRef{upstreamImageRef(e.fwd.local, datasetImage(t, "mirror/uncached", "1.0", LocationUpstream))}
			},
		},
		{
			"the upstream and the hosted both differ, upstream first",
			func(t *testing.T, e *seedEnv) {
				e.cp.preload(whereUpstream, datasetImage(t, "shared/base", "1.0", LocationUpstream), digestBusy)
				e.cp.removeUpstream(datasetImage(t, "mirror/uncached", "1.0", LocationUpstream))
				delete(e.cp.hosted, "tools/cli:0.1.0")
				e.cp.listing[dockerHostedRepository] = slices.DeleteFunc(e.cp.listing[dockerHostedRepository],
					func(c fakeComponent) bool { return c.Name == "tools/cli" })
				e.cp.publish(dockerHostedRepository)
			},
			func(t *testing.T, e *seedEnv) []ImageRef {
				return []ImageRef{
					upstreamImageRef(e.fwd.local, datasetImage(t, "shared/base", "1.0", LocationUpstream)),
					upstreamImageRef(e.fwd.local, datasetImage(t, "mirror/uncached", "1.0", LocationUpstream)),
					nexusImageRef(e.p, dockerHostedRepository, datasetImage(t, "tools/cli", "0.1.0", LocationHosted)),
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			e.cp.preloadAll()
			tt.arrange(t, e)
			e.mustRun()

			want := tt.want(t, e)
			if len(e.cp.copies) != len(want) {
				t.Fatalf("%d copies, want %d:\n%s", len(e.cp.copies), len(want), describeCopies(e.cp.copies))
			}
			for i, dst := range want {
				if e.cp.copies[i].dst != dst {
					t.Errorf("copy #%d to %v, want %v", i+1, e.cp.copies[i].dst, dst)
				}
			}
			requirePhases(t, e.log.snapshot())
			e.requireListing(dockerHostedRepository, imagesIn(LocationHosted))
			e.requireListing(dockerProxyRepository, pullThrough())
		})
	}
}

func TestSeedFillsAnEmptyProxyCache(t *testing.T) {
	e := newSeedEnv(t, readyUpstream())
	e.cp.preloadAll()
	cached := datasetImage(t, "mirror/cached", "1.0", LocationUpstream)
	delete(e.cp.proxyCache, nameTag(cached))
	e.cp.listing[dockerProxyRepository] = nil
	e.cp.publish(dockerProxyRepository)

	e.mustRun()

	// Pulled through the proxy with bench-pull, or read through it: either fills
	// the cache; nothing else may be copied.
	for _, c := range e.cp.copies {
		if c.where != whereProxy || c.dst.Transport != "dir" {
			t.Errorf("copy %s -> %s: only the pull-through is expected", imageArg(c.src), imageArg(c.dst))
		}
	}
	e.requireListing(dockerProxyRepository, pullThrough())
}

// ---- the preconditions ---------------------------------------------------------

func TestSeedNeedsTheStoredCredentials(t *testing.T) {
	tests := []struct {
		name  string
		store func(e *seedEnv) SecretStore
	}{
		{"no secrets file", func(e *seedEnv) SecretStore {
			if err := e.b.store.Remove(); err != nil {
				t.Fatal(err)
			}
			return e.b.store
		}},
		{"no admin password", func(e *seedEnv) SecretStore {
			e.b.store.preset(Secrets{PullPassword: secretMarker + "-pull"})
			return e.b.store
		}},
		{"no bench-pull password", func(e *seedEnv) SecretStore {
			e.b.store.preset(Secrets{AdminPassword: storedAdmin})
			return e.b.store
		}},
		{"an unreadable secrets file", func(*seedEnv) SecretStore { return &failingStore{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			e.deps.Store = tt.store(e)

			_, err := e.run()
			if err == nil {
				t.Fatal("Seed succeeded without the credentials")
			}
			be := requireKind(t, err, ProvisionFailed)
			if !strings.Contains(be.Error(), "nexus-bench:up") {
				t.Errorf("error %q does not say to run mise run nexus-bench:up", be)
			}
			e.requireNoProgress()
			if len(e.kube.lines) != 0 {
				t.Errorf("the cluster was called before the credentials were known: %q", e.kube.lines)
			}
			if got := len(e.b.nexus.Requests()) - e.baseline; got != 0 {
				t.Errorf("%d requests to Nexus before the credentials were known", got)
			}
		})
	}
}

func TestSeedNeedsAReadyUpstream(t *testing.T) {
	tests := []struct {
		name   string
		round  upstreamRound
		mutate func(*Params)
		want   string // a fragment of the message, lower case
	}{
		{"a cut upstream", cutRound(), nil, "the upstream is cut"},
		{"no upstream Deployment", absentRound(), nil, "nexus-bench:up"},
		{"an upstream still starting", roundOf(1, startingPod()), nil, ""},
		{"an upstream URL that is not the bench's", readyUpstream(), func(p *Params) { p.UpstreamURL = "http://registry.example.com:5000" }, strings.ToLower(envUpstreamURL)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mutate []func(*Params)
			if tt.mutate != nil {
				mutate = append(mutate, tt.mutate)
			}
			e := newSeedEnv(t, tt.round, mutate...)

			_, err := e.run()
			if err == nil {
				t.Fatal("Seed succeeded")
			}
			_ = requireKind(t, err, SeedFailed)
			if !strings.Contains(strings.ToLower(err.Error()), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
			// No write at all (the cut upstream is not restored: that is NX-02b),
			// no copy, no port-forward: checkInvariants has the writes.
			e.requireNoProgress()
			if len(e.kube.scales) != 0 {
				t.Errorf("Seed scaled the upstream: %+v", e.kube.scales)
			}
		})
	}
}

func TestSeedReportsAFailedReadOfTheCluster(t *testing.T) {
	e := newSeedEnv(t, readyUpstream())
	e.kube.podsReply = &fakeReply{stderr: "Error from server (Forbidden): pods is forbidden token=" + fakeSecret, err: errFakeExit1}

	_, err := e.run()
	if err == nil {
		t.Fatal("Seed succeeded")
	}
	_ = requireKind(t, err, Forbidden)
	e.requireNoProgress()
}

// ---- the port-forward ----------------------------------------------------------

func TestSeedRefusesAForwardOutsideTheLoopback(t *testing.T) {
	for _, local := range []string{"192.168.1.31:41234", "localhost:41234", "127.0.0.1.evil.example:41234", "127.0.0.1@evil.example:41234"} {
		t.Run(local, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			e.fwd.local = local

			_, err := e.run()
			if err == nil {
				t.Fatal("Seed accepted a port-forward that is not on the loopback")
			}
			var be *BenchError
			if !errors.As(err, &be) {
				t.Fatalf("error = %#v, want a *BenchError", err)
			}
			if len(e.fwd.calls) != 1 {
				t.Errorf("%d port-forwards, want the one that was refused", len(e.fwd.calls))
			}
			if len(e.cp.copies) != 0 || len(e.cp.digests) != 0 {
				t.Errorf("the copier was given a reference outside the loopback: %d copies, %d reads", len(e.cp.copies), len(e.cp.digests))
			}
		})
	}
}

// ---- failures ------------------------------------------------------------------

// copyIndexes are the positions (1-based) of the first Copy of each phase of a
// first run.
func copyIndexes() (upstream, hosted, proxy int) {
	return 1, len(imagesIn(LocationUpstream)) + 1, len(imagesIn(LocationUpstream)) + len(imagesIn(LocationHosted)) + 1
}

func TestSeedStopsAtTheFirstFailedCopy(t *testing.T) {
	up, hosted, proxy := copyIndexes()
	for _, tt := range []struct {
		name string
		at   int
	}{{"upstream", up}, {"docker-hosted", hosted}, {"docker-proxy", proxy}} {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			injected := &BenchError{Kind: RegistryRefused, Cause: "a registry refused the skopeo copy", Check: "check the credentials"}
			e.cp.copyErr = func(n int, _ copyCall) error {
				if n == tt.at {
					return injected
				}
				return nil
			}

			_, err := e.run()
			if err == nil {
				t.Fatal("Seed succeeded")
			}
			if !errors.Is(err, injected) {
				t.Errorf("error %v does not lead to the failure of the copier", err)
			}
			_ = requireKind(t, err, injected.Kind)
			if len(e.cp.copies) != tt.at {
				t.Errorf("%d copies, want %d: nothing is copied after a failure", len(e.cp.copies), tt.at)
			}
		})
	}
}

func TestSeedStopsAtTheFirstFailedRead(t *testing.T) {
	for _, tt := range []struct {
		name      string
		where     string
		maxCopies int
	}{{"upstream", whereUpstream, 0}, {"docker-hosted", whereHosted, len(imagesIn(LocationUpstream))}} {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			injected := &BenchError{Kind: RegistryUnreachable, Cause: "skopeo inspect could not reach a registry", Check: "check the port-forward"}
			e.cp.digestErr = func(c digestCall) error {
				if c.where == tt.where {
					return injected
				}
				return nil
			}

			_, err := e.run()
			if err == nil {
				t.Fatal("Seed succeeded: a registry that cannot be reached is not an absent image")
			}
			if !errors.Is(err, injected) {
				t.Errorf("error %v does not lead to the failure of the copier", err)
			}
			if len(e.cp.copies) != tt.maxCopies {
				t.Errorf("%d copies, want %d: the failed read stops the seed before its own copy", len(e.cp.copies), tt.maxCopies)
			}
		})
	}
}

func TestSeedHonorsACancelledContext(t *testing.T) {
	up, hosted, proxy := copyIndexes()
	for _, tt := range []struct {
		name string
		at   int
	}{{"during the upstream copies", up}, {"during the docker-hosted copies", hosted}, {"during the pull-through", proxy}} {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			ctx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
			defer cancel()
			e.cp.copyErr = func(n int, _ copyCall) error {
				if n == tt.at {
					cancel()
					return &BenchError{Kind: Unclassified, Cause: "skopeo copy was interrupted", Check: "run the command again", Err: context.Canceled}
				}
				return nil
			}

			_, err := e.runWith(ctx)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want one that is context.Canceled", err)
			}
			if len(e.cp.copies) != tt.at {
				t.Errorf("%d copies, want %d: nothing is copied after the cancellation", len(e.cp.copies), tt.at)
			}
		})
	}
}

func TestSeedHonorsAContextCancelledBeforeItStarts(t *testing.T) {
	e := newSeedEnv(t, readyUpstream())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := e.runWith(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want one that is context.Canceled", err)
	}
	e.requireNoProgress()
}

func TestSeedFailsWhenThePortForwardFails(t *testing.T) {
	e := newSeedEnv(t, readyUpstream())
	injected := &BenchError{Kind: ClusterUnreachable, Cause: "the port-forward did not start", Check: "check the cluster"}
	e.fwd.err = injected

	_, err := e.run()
	if !errors.Is(err, injected) {
		t.Fatalf("error = %v, want the failure of the forwarder", err)
	}
	if len(e.cp.copies) != 0 || len(e.cp.digests) != 0 {
		t.Errorf("the copier was used without a port-forward")
	}
}

// TestSeedNeverPrintsWhatAFailureSays feeds each door a plain error whose text
// holds the passwords, as the output of a failed sub-process could: Seed answers a
// *BenchError that keeps the cause for errors.Is and prints none of it.
func TestSeedNeverPrintsWhatAFailureSays(t *testing.T) {
	_, hosted, proxy := copyIndexes()
	tests := []struct {
		name   string
		inject func(e *seedEnv, plain error)
	}{
		{"the port-forward", func(e *seedEnv, plain error) { e.fwd.err = plain }},
		{"a read of the upstream", func(e *seedEnv, plain error) {
			e.cp.digestErr = func(c digestCall) error {
				if c.where == whereUpstream {
					return plain
				}
				return nil
			}
		}},
		{"a copy to docker-hosted", func(e *seedEnv, plain error) {
			e.cp.copyErr = func(n int, _ copyCall) error {
				if n == hosted {
					return plain
				}
				return nil
			}
		}},
		{"the pull-through", func(e *seedEnv, plain error) {
			e.cp.copyErr = func(n int, _ copyCall) error {
				if n == proxy {
					return plain
				}
				return nil
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			plain := fmt.Errorf("FATA[0000] auth=%s:%s token=%s", adminUserID, storedAdmin, fakeSecret)
			tt.inject(e, plain)

			_, err := e.run()
			if err == nil {
				t.Fatal("Seed succeeded")
			}
			var be *BenchError
			if !errors.As(err, &be) {
				t.Fatalf("error = %#v, want a *BenchError", err)
			}
			if !errors.Is(err, plain) {
				t.Errorf("the cause is not reachable with errors.Is")
			}
			if strings.Contains(err.Error(), "FATA") {
				t.Errorf("error %q repeats what the sub-process said", err)
			}
		})
	}
}

// ---- the final listings ---------------------------------------------------------

func TestSeedRefusesWhatTheListingsHoldOutsideTheDataSet(t *testing.T) {
	tests := []struct {
		name  string
		repo  string
		extra fakeComponent
	}{
		{"mirror/uncached read through the proxy", dockerProxyRepository, fakeComponent{Name: "mirror/uncached", Version: "1.0", Digest: digestAlpine}},
		{"another tag of a cached image", dockerProxyRepository, fakeComponent{Name: "mirror/cached", Version: "2.0", Digest: digestBusy}},
		{"an unknown image in the proxy", dockerProxyRepository, fakeComponent{Name: "library/nginx", Version: "1.27", Digest: digestNginx}},
		{"an unknown tag in docker-hosted", dockerHostedRepository, fakeComponent{Name: "api", Version: "9.9.9", Digest: digestNginx}},
		{"an unknown image in docker-hosted", dockerHostedRepository, fakeComponent{Name: "old/stuff", Version: "1", Digest: digestAlpine}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			e.cp.preloadAll()
			e.cp.addExtra(tt.repo, tt.extra)

			_, err := e.run()
			if err == nil {
				t.Fatal("Seed accepted a repository that holds more than the data set")
			}
			_ = requireKind(t, err, SeedFailed)
			for _, want := range []string{tt.repo + " holds images outside the dataset", "nexus-bench:reset"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if e.clk.elapsed() >= seedWaitBound {
				t.Errorf("Seed waited %v for a component that waiting cannot remove", e.clk.elapsed())
			}
		})
	}
}

func TestSeedWaitsForALateListing(t *testing.T) {
	for _, repo := range []string{dockerProxyRepository, dockerHostedRepository} {
		t.Run(repo, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			e.cp.hide(repo)
			e.clk.onSleep = func(n int) {
				if n == 3 {
					e.cp.reveal(repo)
				}
			}

			e.mustRun()
			if len(e.clk.sleeps) < 3 {
				t.Errorf("%d sleeps: the listing was not waited for", len(e.clk.sleeps))
			}
			if e.clk.elapsed() >= seedWaitBound {
				t.Errorf("waited %v, want less than %v for a listing that shows up after three polls", e.clk.elapsed(), seedWaitBound)
			}
			for i, d := range e.clk.sleeps {
				if d > maxPollInterval {
					t.Errorf("sleep #%d lasts %v, want at most %v", i+1, d, maxPollInterval)
				}
			}
		})
	}
}

func TestSeedGivesUpOnAListingThatNeverShowsUp(t *testing.T) {
	for _, repo := range []string{dockerProxyRepository, dockerHostedRepository} {
		t.Run(repo, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			e.cp.hide(repo)

			_, err := e.run()
			if err == nil {
				t.Fatal("Seed succeeded although the instance never lists the images")
			}
			_ = requireKind(t, err, SeedFailed)
			if !strings.Contains(err.Error(), repo) {
				t.Errorf("error %q does not name %s", err, repo)
			}
			if got := e.clk.elapsed(); got < seedWaitBound || got > seedWaitBound+maxPollInterval {
				t.Errorf("waited %v, want %v (within one poll interval of %v)", got, seedWaitBound, maxPollInterval)
			}
		})
	}
}

func TestSeedReportsAListingThatFails(t *testing.T) {
	e := newSeedEnv(t, readyUpstream())
	e.b.nexus.Forbid(http.MethodGet, pathComponents, http.StatusForbidden)

	_, err := e.run()
	if err == nil {
		t.Fatal("Seed succeeded although the components cannot be listed")
	}
	var be *BenchError
	if !errors.As(err, &be) {
		t.Fatalf("error = %#v, want a *BenchError", err)
	}
	var api *APIError
	if !errors.As(err, &api) || api.Status != http.StatusForbidden {
		t.Errorf("error %v does not lead to the *APIError 403", err)
	}
}

// ---- a listing without digests (H-COMPONENTS), a listed digest that differs ----------

// The instance may list a component without the digest of its manifest
// (components_test.go: a component without a manifest asset has an empty Digest).
// The name and the tag listed are then enough: an unlisted digest is accepted, a
// listed digest that is not the one of the data set is not.

func TestSeedAcceptsAListingWithoutDigests(t *testing.T) {
	for _, tt := range []struct {
		name  string
		blind []string
	}{
		{"docker-proxy", []string{dockerProxyRepository}},
		{"docker-hosted", []string{dockerHostedRepository}},
		{"both", []string{dockerProxyRepository, dockerHostedRepository}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			for _, repo := range tt.blind {
				e.cp.blindListing(repo)
			}

			e.mustRun() // verify passes: the names and tags are there
			e.requireCopies(e.fullSequence())

			// The second run copies nothing, although the proxy cannot say the digest
			// of what it caches.
			before := len(e.cp.copies)
			e.out.Reset()
			e.mustRun()
			if got := e.cp.copies[before:]; len(got) != 0 {
				t.Errorf("the second run copied:\n%s", describeCopies(got))
			}
		})
	}
}

func TestSeedAcceptsAnAlreadySeededBenchWhoseListingHasNoDigest(t *testing.T) {
	e := newSeedEnv(t, readyUpstream())
	e.cp.preloadAll()
	e.cp.blindListing(dockerProxyRepository)
	e.cp.blindListing(dockerHostedRepository)

	e.mustRun()
	if len(e.cp.copies) != 0 {
		t.Errorf("copies:\n%s", describeCopies(e.cp.copies))
	}
}

func TestSeedFailsOnAListedDigestThatDiffers(t *testing.T) {
	for _, tt := range []struct {
		name string
		repo string
		img  func(t *testing.T) DatasetImage
	}{
		{"docker-proxy", dockerProxyRepository, func(t *testing.T) DatasetImage {
			return datasetImage(t, "mirror/cached", "1.0", LocationUpstream)
		}},
		{"docker-hosted", dockerHostedRepository, func(t *testing.T) DatasetImage {
			return datasetImage(t, "api", "1.0.0", LocationHosted)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			e.cp.preloadAll()
			img := tt.img(t)
			// The instance lists another digest than the data set's: waiting does
			// not change it, the seed fails (a reset brings the bench back).
			e.cp.setListedDigest(tt.repo, img, digestBusy)

			_, err := e.run()
			if err == nil {
				t.Fatal("Seed accepted a repository that lists another digest than the data set's")
			}
			_ = requireKind(t, err, SeedFailed)
			for _, want := range []string{tt.repo, nameTag(img)} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
			if e.clk.elapsed() < seedWaitBound {
				t.Errorf("failed after %v, want the bounded wait of %v first (the listing may still be catching up)", e.clk.elapsed(), seedWaitBound)
			}
		})
	}
}

// ---- what proves an absence, and what does not ----------------------------------------

// Every output of skopeo that proves the image is not there is a copy.
func TestSeedCopiesWhenTheOutputProvesTheAbsence(t *testing.T) {
	for name, text := range absentTexts {
		t.Run(name, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			e.cp.absentText = text

			e.mustRun()
			e.requireCopies(e.fullSequence())
			if len(e.cp.digests) == 0 {
				t.Error("no digest was read: the absence was not tested")
			}
		})
	}
}

// Whatever does not prove an absence stops the seed before the copy of that image.
func TestSeedStopsWhenTheReadProvesNoAbsence(t *testing.T) {
	// What the real copier returns for an output it cannot read.
	noManifest := &BenchError{Kind: SeedFailed, Cause: "skopeo printed no manifest", Check: "run `skopeo inspect --raw` on the same reference by hand"}
	tooLarge := &BenchError{Kind: SeedFailed, Cause: "the manifest is larger than the 4 MiB registries accept", Check: "run `skopeo inspect --raw` on the same reference by hand"}
	tests := []struct {
		name string
		err  error
	}{
		{"no manifest printed", noManifest},
		{"a manifest above 4 MiB", tooLarge},
		{"an output nobody classified", inspectFailure("FATA[0000] something nobody classified", errFakeExit1)},
		{"a registry that refuses", inspectFailure("FATA[0000] reading manifest 1.0: unauthorized: authentication required", errFakeExit1)},
		{"a registry that cannot be reached", inspectFailure("FATA[0000] dial tcp 127.0.0.1:41234: connect: connection refused", errFakeExit1)},
		{"a refusal that mentions a 404", inspectFailure("FATA[0000] unauthorized: authentication required (probe answered 404 not found)", errFakeExit1)},
	}
	for _, tt := range tests {
		for _, where := range []string{whereUpstream, whereHosted} {
			t.Run(tt.name+" in the "+where, func(t *testing.T) {
				e := newSeedEnv(t, readyUpstream())
				first := true
				e.cp.digestErr = func(c digestCall) error {
					if c.where == where && first {
						first = false
						return tt.err
					}
					return nil
				}

				_, err := e.run()
				if err == nil {
					t.Fatal("Seed went on after a read that proves no absence")
				}
				if !errors.Is(err, tt.err) {
					t.Errorf("error %v does not lead to the failure of the copier", err)
				}
				for _, c := range e.cp.copies {
					if where == whereUpstream || c.where != whereUpstream {
						t.Errorf("copy of %s after the failed read in the %s", imageArg(c.dst), where)
					}
				}
			})
		}
	}
}

// F3 of the audit: the names a listing of Nexus returns are not trusted. Printed
// in a message they are quoted, so that an escape sequence of a terminal shows
// as text, and at most maxSeedNamesShown of them are printed, the rest being
// summed up.
func TestSeedMessageNeutralizesAndBoundsTheNamesOfForeignComponents(t *testing.T) {
	for _, tt := range []struct {
		name     string
		extras   int
		wantMore string // "" when every name is shown
	}{
		{"more names than the bound", maxSeedNamesShown + 3, "and 3 more"},
		{"exactly the bound", maxSeedNamesShown, ""},
		{"fewer names than the bound", 2, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newSeedEnv(t, readyUpstream())
			e.cp.preloadAll()
			for i := 0; i < tt.extras; i++ {
				e.cp.addExtra(dockerProxyRepository, fakeComponent{
					Name: fmt.Sprintf("evil-%d\x1b]0;pwned\x07\x1b[31m", i), Version: "1", Digest: digestNginx,
				})
			}
			_, err := e.run()
			if err == nil {
				t.Fatal("Seed accepted a repository that holds more than the data set")
			}
			_ = requireKind(t, err, SeedFailed)
			msg := err.Error()
			for _, r := range msg {
				if (unicode.IsControl(r) && r != '\n') || r == 0x7f {
					t.Fatalf("the message carries the control character %U:\n%q", r, msg)
				}
			}
			if got, want := strings.Count(msg, "evil-"), min(tt.extras, maxSeedNamesShown); got != want {
				t.Errorf("the message prints %d names, want %d:\n%s", got, want, msg)
			}
			if tt.wantMore != "" && !strings.Contains(msg, tt.wantMore) {
				t.Errorf("the message does not sum up the rest with %q:\n%s", tt.wantMore, msg)
			}
			if tt.wantMore == "" && strings.Contains(msg, " more") {
				t.Errorf("the message sums up names that were all shown:\n%s", msg)
			}
			for _, want := range []string{dockerProxyRepository + " holds images outside the dataset", "nexus-bench:reset"} {
				if !strings.Contains(msg, want) {
					t.Errorf("the message does not contain %q:\n%s", want, msg)
				}
			}
		})
	}
}
