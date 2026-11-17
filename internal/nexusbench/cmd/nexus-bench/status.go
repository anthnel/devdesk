package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/anthnel/devdesk/internal/nexusbench"
)

// listedImage is one image tag a repository lists, as the REST API of Nexus
// reports it: the name, the tag and the manifest digest ("sha256:<hex>", empty
// when the instance lists none).
type listedImage = nexusbench.ListedImage

// readImagesAnonymously lists the images of a repository without sending any
// credential: a read of what anonymous access allows, nothing else.
func readImagesAnonymously(ctx context.Context, baseURL, repository string) ([]listedImage, error) {
	return nexusbench.ListImagesAnonymously(ctx, baseURL, repository)
}

// nexusPodSelector is the label of the Nexus pod (see 20-deployment.yaml.tmpl).
const nexusPodSelector = "app.kubernetes.io/name=nexus"

// status reads the cluster and the descriptor and reports; it changes nothing.
// A pod that is missing or a Nexus that does not answer is a finding, not a
// failure: the exit code is 1 only when the cluster cannot be read at all.
func (s *session) status() int {
	runner, err := s.d.newRunner(s.p.Context)
	if err != nil {
		return s.fail(err)
	}
	if _, err := s.d.preflight(s.ctx, runner, s.p, nexusbench.OpStatus); err != nil {
		return s.fail(err)
	}
	pod := readPod(s, runner)
	if err := s.ctx.Err(); err != nil {
		return s.fail(err)
	}
	base := s.restURL()
	rest := "ready"
	if err := s.d.newProber().Ready(s.ctx, base); err != nil {
		// The error is not shown: the text of a failed call is not trusted.
		rest = "not ready"
	}
	if err := s.ctx.Err(); err != nil {
		return s.fail(err)
	}
	upstream := s.describeUpstream(runner)
	if err := s.ctx.Err(); err != nil {
		return s.fail(err)
	}
	path := s.descriptorPath()
	desc, descErr := nexusbench.LoadDescriptor(path)
	dataset := s.describeDataset(desc, descErr, base, rest == "ready")
	if err := s.ctx.Err(); err != nil {
		return s.fail(err)
	}
	_, _ = fmt.Fprintf(s.stdout, "bench: namespace %s, context %s\n", s.p.Namespace, s.p.Context)
	_, _ = fmt.Fprintf(s.stdout, "pod: %s\n", pod)
	_, _ = fmt.Fprintf(s.stdout, "rest: %s (%s)\n", rest, base)
	_, _ = fmt.Fprintf(s.stdout, "upstream: %s\n", upstream)
	_, _ = fmt.Fprintf(s.stdout, "descriptor: %s\n", describeDescriptor(path, desc, descErr, s.p))
	_, _ = fmt.Fprintf(s.stdout, "dataset: %s\n", dataset)
	return exitOK
}

// describeUpstream says what the cluster holds of the bench upstream. An
// upstream the bench did not deploy is not its to read; a deployment that
// cannot be read is a finding, never an error text (it can carry output of
// kubectl).
func (s *session) describeUpstream(r nexusbench.Runner) string {
	if !s.p.UpstreamIsBench() {
		return "not managed by the bench (NEXUS_BENCH_UPSTREAM_URL points elsewhere)"
	}
	state, err := s.d.readUpstream(s.ctx, r, s.p)
	if err != nil {
		return "unknown (the deployment " + upstreamName + " could not be read)"
	}
	switch state {
	case nexusbench.UpstreamReady:
		return "ready"
	case nexusbench.UpstreamCut:
		return "cut (0 replicas)"
	case nexusbench.UpstreamStarting:
		return "starting (no pod is ready yet)"
	}
	return "missing (run mise run nexus-bench:up)"
}

// upstreamName is the Deployment of the bench upstream.
const upstreamName = "bench-upstream"

// The two repositories that hold the data set. The group is never read: a read
// through it can cache an image (F9).
const (
	hostedRepository = "docker-hosted"
	proxyRepository  = "docker-proxy"
)

// describeDataset compares the data set the descriptor lists with what
// docker-hosted and docker-proxy list now. The listings are anonymous and read
// only, so what status cannot read is "unknown", never a failure.
func (s *session) describeDataset(d nexusbench.Descriptor, descErr error, base string, restReady bool) string {
	switch {
	case descErr != nil:
		return "unknown (no readable descriptor to compare with)"
	case len(d.Dataset) == 0 && !s.p.UpstreamIsBench():
		// seed refuses an upstream the bench did not deploy: no point sending there.
		return "unknown (the descriptor lists no data set)"
	case len(d.Dataset) == 0:
		return "unknown (the descriptor lists no data set: run mise run nexus-bench:seed)"
	case !restReady:
		return "unknown (Nexus does not answer)"
	case !s.p.Anonymous:
		return "unknown (anonymous access is off and status sends no credential)"
	}
	var diffs, compared int
	for _, repo := range []string{hostedRepository, proxyRepository} {
		listed, err := s.d.readImages(s.ctx, base, repo)
		if err != nil {
			return "unknown (the listing of " + repo + " failed)"
		}
		want := expectedIn(d.Dataset, repo)
		compared += len(want)
		n, ok := compareListing(want, listed)
		if !ok {
			return "unknown (the listing of " + repo + " gives no digest)"
		}
		diffs += n
	}
	if diffs == 0 {
		return fmt.Sprintf("matches (%d images compared)", compared)
	}
	noun := "images"
	if diffs == 1 {
		noun = "image"
	}
	return fmt.Sprintf("differs (%d %s)", diffs, noun)
}

// expectedIn returns the images of the data set that repo holds: the hosted
// ones in docker-hosted, the ones cached in the proxy in docker-proxy.
func expectedIn(images []nexusbench.DescriptorImage, repo string) []nexusbench.DescriptorImage {
	var out []nexusbench.DescriptorImage
	for _, img := range images {
		if (repo == hostedRepository && img.Location == string(nexusbench.LocationHosted)) ||
			(repo == proxyRepository && img.CachedInProxy) {
			out = append(out, img)
		}
	}
	return out
}

// compareListing counts the differences between what a repository should hold
// and what it lists: an image that is missing or has another digest, and an
// image the data set does not know. ok is false when the listing gives no
// digest for an image that is there, which makes the comparison impossible.
func compareListing(want []nexusbench.DescriptorImage, listed []listedImage) (diffs int, ok bool) {
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

// podList is the part of `kubectl get pods -o json` the report reads.
type podList struct {
	Items []struct {
		Metadata struct {
			Name              string `json:"name"`
			DeletionTimestamp string `json:"deletionTimestamp"`
		} `json:"metadata"`
		Status struct {
			Phase             string `json:"phase"`
			ContainerStatuses []struct {
				Ready        bool `json:"ready"`
				RestartCount int  `json:"restartCount"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

// readPod describes the Nexus pod in one line: its phase, name, readiness and restarts.
func readPod(s *session, r nexusbench.Runner) string {
	stdout, _, err := r.Run(s.ctx, nil, "get", "pods", "-n", s.p.Namespace, "-l", nexusPodSelector, "-o", "json", "--request-timeout=10s")
	if err != nil {
		return "unknown (kubectl get pods failed)"
	}
	var list podList
	if err := json.Unmarshal(stdout, &list); err != nil {
		return "unknown (kubectl get pods returned something unreadable)"
	}
	if len(list.Items) == 0 {
		return "not found"
	}
	item := list.Items[0]
	phase := strings.ToLower(item.Status.Phase)
	if item.Metadata.DeletionTimestamp != "" {
		phase = "terminating"
	}
	if phase == "" {
		phase = "unknown"
	}
	ready, restarts := 0, 0
	for _, c := range item.Status.ContainerStatuses {
		if c.Ready {
			ready++
		}
		restarts += c.RestartCount
	}
	line := fmt.Sprintf("%s (%s, %d/%d ready, %d restarts", phase, item.Metadata.Name, ready, len(item.Status.ContainerStatuses), restarts)
	if len(list.Items) > 1 {
		line += fmt.Sprintf(", %d more pods", len(list.Items)-1)
	}
	return line + ")"
}

// describeDescriptor compares the versioned descriptor, as read at path, with
// the parameters of this run and says where they differ.
func describeDescriptor(path string, d nexusbench.Descriptor, err error, p nexusbench.Params) string {
	switch {
	case errors.Is(err, nexusbench.ErrNoDescriptor):
		return "not found (" + path + ")"
	case err != nil:
		return "unreadable (" + err.Error() + ")"
	}
	diffs := descriptorDiffs(d, p)
	if len(diffs) == 0 {
		return "matches (" + path + ")"
	}
	return "differs from the current parameters (" + path + ")\n  " + strings.Join(diffs, "\n  ")
}

// descriptorDiffs lists the fields of the descriptor that the parameters of
// this run would write differently. The hosts and the versions are left out:
// they come from the cluster, not from the parameters. So is the dataset, which
// the parameters know nothing of.
func descriptorDiffs(d nexusbench.Descriptor, p nexusbench.Params) []string {
	want := nexusbench.BuildDescriptor(p, nexusbench.DeployResult{}, nexusbench.ProvisionResult{}, nexusbench.SeedResult{})
	fields := []struct{ name, got, want string }{
		{"context", d.KubeContext, want.KubeContext},
		{"namespace", d.Namespace, want.Namespace},
		{"image", d.Image, want.Image},
		{"http nodePort", strconv.Itoa(d.REST.NodePort), strconv.Itoa(want.REST.NodePort)},
		{"docker nodePort", dockerPort(d), dockerPort(want)},
		{"upstream URL", upstreamURL(d), upstreamURL(want)},
		{"upstream image", d.Upstream.Image, want.Upstream.Image},
		{"anonymous access", anonymous(d), anonymous(want)},
	}
	var diffs []string
	for _, f := range fields {
		if f.got != f.want {
			diffs = append(diffs, fmt.Sprintf("%s: descriptor %s, current %s", f.name, f.got, f.want))
		}
	}
	return diffs
}

func dockerPort(d nexusbench.Descriptor) string {
	if len(d.DockerConnectors) == 0 {
		return "none"
	}
	return strconv.Itoa(d.DockerConnectors[0].NodePort)
}

func upstreamURL(d nexusbench.Descriptor) string {
	for _, r := range d.Repositories {
		if r.Type == "proxy" {
			return r.RemoteURL
		}
	}
	return "none"
}

func anonymous(d nexusbench.Descriptor) string {
	for _, a := range d.Accounts {
		if a.UserID == "anonymous" && a.Enabled != nil {
			return strconv.FormatBool(*a.Enabled)
		}
	}
	return "unset"
}
