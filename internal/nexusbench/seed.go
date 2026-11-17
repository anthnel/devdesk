package nexusbench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// upstreamServiceName and upstreamServicePort are the Service of the bench
	// upstream registry and the port the port-forward reaches.
	upstreamServiceName = upstreamDeployment
	upstreamServicePort = 5000

	// seedListWait bounds the wait for a repository to list what the seed put in
	// it (H-COMPONENTS), and seedListInterval separates two readings.
	seedListWait     = 30 * time.Second
	seedListInterval = 2 * time.Second

	// seedLoopbackHost is the only host the port-forward may be reached on.
	seedLoopbackHost = "127.0.0.1"

	// maxSeedNamesShown bounds the names of components a message prints, the rest
	// being summed up as "and K more" (F3 of the audit).
	maxSeedNamesShown = 5

	// resetCheck is what a repository that holds more than the data set asks.
	resetCheck = "run `mise run nexus-bench:reset` to bring the bench back to its initial state"
)

// SeedDeps are the doors Seed goes through: the cluster (to read the upstream),
// the port-forward to the upstream, the image copier, the clock of the bounded
// wait and the stored secrets.
type SeedDeps struct {
	Runner    Runner
	Forwarder PortForwarder
	Copier    ImageCopier
	Clock     Clock
	Store     SecretStore
}

// SeedResult is what Seed leaves in place: the data set, every digest filled,
// PullThrough true exactly for the images now cached in docker-proxy.
type SeedResult struct{ Images []DatasetImage }

// Seed puts the data set (D-I) in place, in this order: the stored credentials,
// the state of the upstream (read only), the OCI layout in a temporary folder,
// the port-forward to the upstream and the upstream images, the end of the
// port-forward, the docker-hosted images, the pull-through of the PullThrough
// images by docker-proxy, and the two listings, which must hold the data set and
// nothing else. An image is copied only when the digest read at its destination
// differs from the one of the data set, so a second run copies nothing.
//
// Seed never scales, applies or writes to Nexus, and it never restores a cut
// upstream (NX-02b): a cut, missing or starting upstream is a SeedFailed before
// the first copy. Docker-proxy is read only for the PullThrough images (F9): a
// read of mirror/uncached through it would cache it, and only reset removes
// that. Every failure is a *BenchError whose cause stays reachable with
// errors.Is and is never printed, but a cancelled context, which is returned
// as is. A nil out discards the progress lines ("seed: ...", no password).
func Seed(ctx context.Context, d SeedDeps, baseURL string, p Params, out io.Writer) (SeedResult, error) {
	if out == nil {
		out = io.Discard
	}
	if err := ctx.Err(); err != nil {
		return SeedResult{}, err
	}
	admin, pull, c, err := seedCredentials(ctx, d.Store, baseURL)
	if err != nil {
		return SeedResult{}, err
	}
	defer c.closeIdle()
	if err := requireSeedableUpstream(ctx, d.Runner, p); err != nil {
		return SeedResult{}, err
	}

	images := Dataset()
	s := &seeder{ctx: ctx, d: d, p: p, out: out, c: c, baseURL: baseURL, admin: admin, pull: pull, images: images}
	root, err := os.MkdirTemp("", "nexus-bench-seed-")
	if err != nil {
		return SeedResult{}, &BenchError{
			Kind:  SeedFailed,
			Cause: "the temporary folder of the seed cannot be created",
			Check: "check that TMPDIR is a folder this user can write to",
			Err:   err,
		}
	}
	defer func() { _ = os.RemoveAll(root) }()
	if err := s.writeLayout(root); err != nil {
		return SeedResult{}, err
	}
	steps := []func() error{
		s.seedUpstream,
		s.seedHosted,
		func() error { return s.pullThrough(root) },
		func() error { return s.verify(dockerProxyRepository, s.wanted(isPullThrough)) },
		func() error { return s.verify(dockerHostedRepository, s.wanted(isHosted)) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return SeedResult{}, err
		}
	}
	s.say("the data set is in place: %d images", len(images))
	return SeedResult{Images: images}, nil
}

// nexusImageRef is the one place that builds the reference of an image of a
// repository of the bench Nexus ("docker-hosted" or "docker-proxy"). Today it is
// the path routing on the HTTP NodePort (H-PATH): 127.0.0.1:<HTTPNodePort>/
// <repository>/<name>:<tag>. Step 12 settles whether the instance serves it.
func nexusImageRef(p Params, repository string, img DatasetImage) ImageRef {
	return ImageRef{
		Transport: "docker",
		Location:  seedLoopbackHost + ":" + strconv.Itoa(p.HTTPNodePort) + "/" + repository + "/" + img.Name + ":" + img.Tag,
	}
}

// upstreamImageRef is the reference of an image in the bench upstream reached
// through the port-forward at local ("127.0.0.1:<port>"): <local>/<name>:<tag>.
func upstreamImageRef(local string, img DatasetImage) ImageRef {
	return ImageRef{Transport: "docker", Location: local + "/" + img.Name + ":" + img.Tag}
}

// seedCredentials reads the two accounts of the bench from the store and
// returns the admin client of the final listings. Nothing is sent to Nexus.
func seedCredentials(ctx context.Context, store SecretStore, baseURL string) (admin, pull Credentials, c *client, err error) {
	c, sec, err := adminFromStore(ctx, baseURL, store)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return admin, pull, nil, cerr
		}
		return admin, pull, nil, err
	}
	if sec.PullPassword == "" {
		return admin, pull, nil, &BenchError{
			Kind:  ProvisionFailed,
			Cause: "the bench has no " + pullUserID + " password stored: its provisioning did not finish",
			Check: upCheck,
			Err:   errors.New("no pull password in the secrets file"),
		}
	}
	return Credentials{User: adminUserID, Password: sec.AdminPassword}, Credentials{User: pullUserID, Password: sec.PullPassword}, c, nil
}

// requireSeedableUpstream refuses what the seed cannot fill: an upstream URL the
// bench did not deploy (the proxy would not read what the seed pushes), and an
// upstream that is missing, cut or still starting. It writes nothing: bringing a
// cut upstream back is NX-02b.
func requireSeedableUpstream(ctx context.Context, r Runner, p Params) error {
	if !p.UpstreamIsBench() {
		return &BenchError{
			Kind:  SeedFailed,
			Cause: fmt.Sprintf("%s points at an upstream the bench did not deploy, and the seed pushes to the upstream of the bench only", envUpstreamURL),
			Check: "unset " + envUpstreamURL + " to use the bench upstream",
		}
	}
	state, err := ReadUpstream(ctx, r, p)
	if err != nil {
		return err
	}
	switch state {
	case UpstreamMissing:
		return &BenchError{
			Kind:  SeedFailed,
			Cause: fmt.Sprintf("the upstream %s does not exist in namespace %q", upstreamDeployment, p.Namespace),
			Check: "run `mise run nexus-bench:up`, which deploys it",
		}
	case UpstreamCut:
		return &BenchError{
			Kind:  SeedFailed,
			Cause: "the upstream is cut",
			Check: "run `mise run nexus-bench:restore-upstream`, then the seed again",
		}
	case UpstreamStarting:
		return &BenchError{
			Kind:  SeedFailed,
			Cause: "the upstream is still starting",
			Check: "wait for its pod to be Ready (" + podsCommand(p) + "), then run the seed again",
		}
	}
	return nil
}

// seeder is one run of Seed.
type seeder struct {
	ctx     context.Context
	d       SeedDeps
	p       Params
	out     io.Writer
	c       *client
	baseURL string
	admin   Credentials
	pull    Credentials
	images  []DatasetImage // Dataset(), digests filled
	layout  string         // the OCI layout folder
	refs    []string       // the layout reference of images[i]
}

func (s *seeder) say(format string, args ...any) {
	_, _ = fmt.Fprintf(s.out, "seed: "+format+"\n", args...)
}

// writeLayout writes the data set as an OCI layout under root.
func (s *seeder) writeLayout(root string) error {
	s.layout = filepath.Join(root, "layout")
	err := os.Mkdir(s.layout, 0o700)
	if err == nil {
		s.refs, err = WriteLayout(s.layout, s.images)
	}
	if err != nil {
		return &BenchError{
			Kind:  SeedFailed,
			Cause: "the OCI layout of the data set cannot be written",
			Check: "check that TMPDIR has room and is writable",
			Err:   err,
		}
	}
	return nil
}

// source is the layout reference of images[i].
func (s *seeder) source(i int) ImageRef {
	return ImageRef{Transport: "oci", Location: s.layout + ":" + s.refs[i]}
}

func isPullThrough(img DatasetImage) bool { return img.PullThrough }

func isHosted(img DatasetImage) bool { return img.Location == LocationHosted }

// wanted lists the images of the data set that keep.
func (s *seeder) wanted(keep func(DatasetImage) bool) []DatasetImage {
	var out []DatasetImage
	for _, img := range s.images {
		if keep(img) {
			out = append(out, img)
		}
	}
	return out
}

// seedUpstream pushes the upstream images through a port-forward that is always
// stopped before it returns.
func (s *seeder) seedUpstream() error {
	local, stop, err := s.d.Forwarder.Forward(s.ctx, s.p.Namespace, upstreamServiceName, upstreamServicePort)
	if err != nil {
		return s.failure(err, "the port-forward to the upstream")
	}
	if stop != nil {
		defer func() { _ = stop() }()
	}
	if !strings.HasPrefix(local, seedLoopbackHost+":") || !loopbackRegistry(local) {
		return &BenchError{
			Kind:  SeedFailed,
			Cause: "the port-forward to the upstream is not on 127.0.0.1",
			Check: "the bench pushes to a registry on the loopback only: check the kubectl on PATH",
		}
	}
	for i, img := range s.images {
		if img.Location != LocationUpstream {
			continue
		}
		if err := s.push(i, upstreamImageRef(local, img), nil, "the upstream"); err != nil {
			return err
		}
	}
	return nil
}

// seedHosted pushes the docker-hosted images with the admin account.
func (s *seeder) seedHosted() error {
	for i, img := range s.images {
		if img.Location != LocationHosted {
			continue
		}
		if err := s.push(i, nexusImageRef(s.p, dockerHostedRepository, img), &s.admin, dockerHostedRepository); err != nil {
			return err
		}
	}
	return nil
}

// push copies images[i] from the layout to dst unless dst already holds the
// digest of the data set. A proven absence (errImageAbsent) is a copy; any
// other failure to read stops the seed.
func (s *seeder) push(i int, dst ImageRef, creds *Credentials, where string) error {
	img := s.images[i]
	name := img.Name + ":" + img.Tag
	have, err := s.d.Copier.ManifestDigest(s.ctx, dst, creds)
	if errors.Is(err, errImageAbsent) {
		have, err = "", nil
	}
	if err != nil {
		return s.failure(err, "reading "+name+" in "+where)
	}
	if have == img.Digest {
		s.say("%s is already in %s", name, where)
		return nil
	}
	s.say("copying %s to %s", name, where)
	err = s.d.Copier.Copy(s.ctx, s.source(i), dst, CopyOptions{DestCreds: creds, PreserveDigests: true})
	if err != nil {
		return s.failure(err, "copying "+name+" to "+where)
	}
	return nil
}

// pullThrough has docker-proxy pull each PullThrough image from the upstream
// with the bench-pull account, into a folder that is thrown away. Only these
// images are ever read through the proxy (F9): it caches whatever it serves. An
// image the proxy already lists with the right digest is not pulled again.
func (s *seeder) pullThrough(root string) error {
	cached, err := s.c.listComponents(s.ctx, dockerProxyRepository)
	if err != nil {
		return s.restFailure(err)
	}
	for i, img := range s.images {
		if !img.PullThrough {
			continue
		}
		name := img.Name + ":" + img.Tag
		if listed(cached, img) {
			s.say("%s is already cached by %s", name, dockerProxyRepository)
			continue
		}
		s.say("pulling %s through %s", name, dockerProxyRepository)
		dst := ImageRef{Transport: "dir", Location: filepath.Join(root, fmt.Sprintf("pull-%02d", i+1))}
		src := nexusImageRef(s.p, dockerProxyRepository, img)
		if err := s.d.Copier.Copy(s.ctx, src, dst, CopyOptions{SrcCreds: &s.pull, PreserveDigests: true}); err != nil {
			return s.failure(err, "pulling "+name+" through "+dockerProxyRepository)
		}
	}
	return nil
}

// listed reports whether components hold img: its name and tag are enough, a
// component the instance lists without a digest counts as present (H-COMPONENTS),
// and a digest it does list must be the one of the data set.
func listed(components []component, img DatasetImage) bool {
	for _, comp := range components {
		if comp.Name == img.Name && comp.Version == img.Tag {
			return comp.Digest == "" || comp.Digest == img.Digest
		}
	}
	return false
}

// verify waits, for at most seedListWait, until repo lists every image of want
// and nothing else (H-COMPONENTS). A component outside the data set cannot be
// waited away: it fails at once and asks for a reset.
func (s *seeder) verify(repo string, want []DatasetImage) error {
	deadline := s.d.Clock.Now().Add(seedListWait)
	for {
		comps, err := s.c.listComponents(s.ctx, repo)
		if err != nil {
			return s.restFailure(err)
		}
		if extra := outside(comps, want); len(extra) > 0 {
			return &BenchError{
				Kind:  SeedFailed,
				Cause: fmt.Sprintf("%s holds images outside the dataset (%s)", repo, shownNames(extra)),
				Check: resetCheck,
			}
		}
		missing := absent(comps, want)
		if len(missing) == 0 {
			s.say("%s lists the %d images of the data set", repo, len(want))
			return nil
		}
		if !s.d.Clock.Now().Before(deadline) {
			return &BenchError{
				Kind:  SeedFailed,
				Cause: fmt.Sprintf("%s does not list %s after %s", repo, strings.Join(missing, ", "), seedListWait),
				Check: "run the seed again, and read the log of the pod if it persists: " + podsCommand(s.p),
			}
		}
		if err := s.d.Clock.Sleep(s.ctx, seedListInterval); err != nil {
			return err
		}
	}
}

// shownNames quotes the names a listing returned, so that an escape sequence of
// a terminal shows as text, and prints at most maxSeedNamesShown of them: the
// rest is summed up as "and K more" (F3 of the audit).
func shownNames(names []string) string {
	shown := names[:min(len(names), maxSeedNamesShown)]
	quoted := make([]string, len(shown))
	for i, n := range shown {
		quoted[i] = strconv.Quote(n)
	}
	out := strings.Join(quoted, ", ")
	if more := len(names) - len(shown); more > 0 {
		out += fmt.Sprintf(" and %d more", more)
	}
	return out
}

// outside lists the "name:tag" of the components that are not in want.
func outside(components []component, want []DatasetImage) []string {
	var out []string
	for _, comp := range components {
		known := false
		for _, img := range want {
			known = known || (comp.Name == img.Name && comp.Version == img.Tag)
		}
		if !known {
			out = append(out, comp.Name+":"+comp.Version)
		}
	}
	return out
}

// absent lists the "name:tag" of the images of want that components do not list
// with the right digest.
func absent(components []component, want []DatasetImage) []string {
	var out []string
	for _, img := range want {
		if !listed(components, img) {
			out = append(out, img.Name+":"+img.Tag)
		}
	}
	return out
}

// failure is the error of a step of a door (copier, forwarder): a cancelled
// context is returned as is, a *BenchError keeps its kind and gains the step,
// and anything else becomes a *BenchError that keeps the cause for errors.Is
// without printing it (the text of a sub-process can carry a credential).
func (s *seeder) failure(err error, step string) error {
	if cerr := s.ctx.Err(); cerr != nil {
		return cerr
	}
	var be *BenchError
	if errors.As(err, &be) {
		return &BenchError{Kind: be.Kind, Cause: step + ": " + be.Cause, Check: be.Check, Err: err}
	}
	return &BenchError{
		Kind:  Unclassified,
		Cause: step + " failed",
		Check: "run the same command by hand to see what it says",
		Err:   err,
	}
}

// restFailure is the error of a listing of Nexus.
func (s *seeder) restFailure(err error) error {
	if cerr := s.ctx.Err(); cerr != nil {
		return cerr
	}
	return asBenchError(err, s.p, s.baseURL)
}
