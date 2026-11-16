package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/anthnel/devdesk/internal/nexusbench"
)

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
	_, _ = fmt.Fprintf(s.stdout, "bench: namespace %s, context %s\n", s.p.Namespace, s.p.Context)
	_, _ = fmt.Fprintf(s.stdout, "pod: %s\n", pod)
	_, _ = fmt.Fprintf(s.stdout, "rest: %s (%s)\n", rest, base)
	_, _ = fmt.Fprintf(s.stdout, "descriptor: %s\n", s.describeDescriptor())
	return exitOK
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

// describeDescriptor compares the versioned descriptor with the parameters of
// this run and says where they differ.
func (s *session) describeDescriptor() string {
	path := s.descriptorPath()
	d, err := nexusbench.LoadDescriptor(path)
	switch {
	case errors.Is(err, nexusbench.ErrNoDescriptor):
		return "not found (" + path + ")"
	case err != nil:
		return "unreadable (" + err.Error() + ")"
	}
	diffs := descriptorDiffs(d, s.p)
	if len(diffs) == 0 {
		return "matches (" + path + ")"
	}
	return "differs from the current parameters (" + path + ")\n  " + strings.Join(diffs, "\n  ")
}

// descriptorDiffs lists the fields of the descriptor that the parameters of
// this run would write differently. The hosts and the versions are left out:
// they come from the cluster, not from the parameters.
func descriptorDiffs(d nexusbench.Descriptor, p nexusbench.Params) []string {
	want := nexusbench.BuildDescriptor(p, nexusbench.DeployResult{}, nexusbench.ProvisionResult{})
	fields := []struct{ name, got, want string }{
		{"context", d.KubeContext, want.KubeContext},
		{"namespace", d.Namespace, want.Namespace},
		{"image", d.Image, want.Image},
		{"http nodePort", strconv.Itoa(d.REST.NodePort), strconv.Itoa(want.REST.NodePort)},
		{"docker nodePort", dockerPort(d), dockerPort(want)},
		{"upstream URL", upstreamURL(d), upstreamURL(want)},
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
