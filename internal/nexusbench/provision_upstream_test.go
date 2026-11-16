package nexusbench

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// The first real run (DEVDESK-504, step 10) showed that Nexus 3.98.1-01 resolves
// the host of a proxy's remote URL when the repository is created and answers
// 400 (a validation error on proxy.remoteUrl) when it does not resolve. The
// upstream of the bench is deployed by NX-02, so on a bench without it the proxy
// cannot exist yet. Provision then creates the other repositories, leaves the
// proxy out of the group, says so, and a later run adds both.

func TestProvisionWithoutUpstreamLeavesTheProxyPending(t *testing.T) {
	b := newBench(t)
	b.nexus.BlockUpstream()
	res, err := b.run()
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	b.requireQuiet(nil)
	if !res.ProxyPending {
		t.Error("ProvisionResult.ProxyPending = false, want true")
	}
	requireRepo(t, b.nexus, "hosted", dockerHostedRepository)
	if _, ok := b.nexus.Repo("proxy", dockerProxyRepository); ok {
		t.Error("the proxy exists although the instance refused it")
	}
	group := requireRepo(t, b.nexus, "group", dockerGroupRepository)
	if got, want := stringsAt(group, "group", "memberNames"), []string{dockerHostedRepository}; !reflect.DeepEqual(got, want) {
		t.Errorf("group members = %v, want %v", got, want)
	}
	if !strings.Contains(b.out.String(), "provision: warning: ") || !strings.Contains(b.out.String(), dockerProxyRepository) {
		t.Errorf("output does not warn about %s:\n%s", dockerProxyRepository, b.out.String())
	}
}

func TestProvisionAddsTheProxyOnceTheUpstreamResolves(t *testing.T) {
	b := newBench(t)
	b.nexus.BlockUpstream()
	b.mustRun()
	b.nexus.ResolveUpstream()
	b.ip.ok = false

	res := b.mustRun()
	if res.ProxyPending {
		t.Error("ProvisionResult.ProxyPending = true once the upstream resolves")
	}
	requireRepo(t, b.nexus, "proxy", dockerProxyRepository)
	group := requireRepo(t, b.nexus, "group", dockerGroupRepository)
	want := []string{dockerHostedRepository, dockerProxyRepository}
	if got := stringsAt(group, "group", "memberNames"); !reflect.DeepEqual(got, want) {
		t.Errorf("group members = %v, want %v", got, want)
	}
}

func TestProvisionKeepsAnExistingProxyWhenTheUpstreamStopsResolving(t *testing.T) {
	b := newBench(t)
	b.mustRun()
	b.nexus.BlockUpstream()
	b.ip.ok = false

	res := b.mustRun()
	if res.ProxyPending {
		t.Error("ProxyPending = true although the proxy exists")
	}
	group := requireRepo(t, b.nexus, "group", dockerGroupRepository)
	want := []string{dockerHostedRepository, dockerProxyRepository}
	if got := stringsAt(group, "group", "memberNames"); !reflect.DeepEqual(got, want) {
		t.Errorf("group members = %v, want %v", got, want)
	}
}

func TestProvisionOtherRefusalOfTheProxyStillFails(t *testing.T) {
	b := newBench(t)
	b.nexus.Forbid(http.MethodPost, repoPath("proxy", ""), http.StatusBadRequest)
	err := b.failure()
	var api *APIError
	if !errors.As(err, &api) || api.Status != http.StatusBadRequest {
		t.Fatalf("error = %v, want an *APIError with status 400", err)
	}
	if api.RemoteURLBlocked {
		t.Error("a 400 without the remoteUrl validation error was taken for an unresolved upstream")
	}
}

func TestBuildDescriptorLeavesAPendingProxyOut(t *testing.T) {
	d := BuildDescriptor(Defaults(), DeployResult{NodeIP: sampleNodeIP}, ProvisionResult{
		NexusVersion: sampleNexusVersion, Edition: "COMMUNITY", EULAAccepted: true, ProxyPending: true,
	})
	want := []DescriptorRepository{
		{Name: "docker-hosted", Type: "hosted"},
		{Name: "docker-group", Type: "group", Members: []string{"docker-hosted"}},
	}
	if !reflect.DeepEqual(d.Repositories, want) {
		t.Errorf("repositories = %+v, want %+v", d.Repositories, want)
	}
}

func TestRemoteURLRefusedOnlyForAnUnresolvedHost(t *testing.T) {
	real := `[{"id":"HelperBean.proxy.remoteUrl","message":"Proxy URL blocked: Failed to resolve host: x"}]`
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"the real answer", real, true},
		{"the message in another case", `[{"id":"HelperBean.proxy.remoteUrl","message":"FAILED TO RESOLVE HOST: x"}]`, true},
		{"among other errors", `[{"id":"HelperBean.name","message":"Invalid"},` + real[1:], true},
		{"another message on the field", `[{"id":"HelperBean.proxy.remoteUrl","message":"Malformed URL"}]`, false},
		{"the message on another id", `[{"id":"HelperBean.name","message":"Failed to resolve host: x"}]`, false},
		{"no message", `[{"id":"HelperBean.proxy.remoteUrl"}]`, false},
		{"not JSON", `Failed to resolve host`, false},
		{"an object instead of a list", `{"id":"HelperBean.proxy.remoteUrl","message":"Failed to resolve host: x"}`, false},
		{"an empty list", `[]`, false},
		{"empty", ``, false},
		{"over 4 KiB", `[{"id":"HelperBean.proxy.remoteUrl","message":"Failed to resolve host: ` + strings.Repeat("x", drainBytes) + `"}]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := remoteURLRefused([]byte(tc.body)); got != tc.want {
				t.Errorf("remoteURLRefused(%.80q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestProvisionOutputNeverCarriesTheServerAnswer(t *testing.T) {
	b := newBench(t)
	b.nexus.BlockUpstream()
	res, err := b.run()
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !res.ProxyPending {
		t.Fatal("ProxyPending = false, the instance did refuse the proxy")
	}
	seen := b.out.String()
	if err != nil {
		seen += err.Error()
	}
	for _, leak := range []string{"upstream.bench.invalid", "HelperBean", "Failed to resolve"} {
		if strings.Contains(seen, leak) {
			t.Errorf("the output carries %q from the server's answer:\n%s", leak, seen)
		}
	}
}

func TestProvisionValidationErrorOnAnotherIDStillFails(t *testing.T) {
	b := newBench(t)
	b.nexus.ForbidWithBody(http.MethodPost, repoPath("proxy", ""), http.StatusBadRequest, `[{"id":"HelperBean.name","message":"Failed to resolve host: x"}]`)
	err := b.failure()
	var api *APIError
	if !errors.As(err, &api) || api.RemoteURLBlocked {
		t.Fatalf("error = %v, want an *APIError that is not an unresolved upstream", err)
	}
}

func TestProvisionRemoteURLRefusalOnAnotherRepositoryFails(t *testing.T) {
	for _, kind := range []string{"hosted", "group"} {
		t.Run(kind, func(t *testing.T) {
			b := newBench(t)
			b.nexus.ForbidWithBody(http.MethodPost, repoPath(kind, ""), http.StatusBadRequest, unresolvedBody)
			err := b.failure()
			var api *APIError
			if !errors.As(err, &api) || api.Status != http.StatusBadRequest {
				t.Fatalf("error = %v, want an *APIError with status 400", err)
			}
		})
	}
}

const unresolvedBody = `[{"id":"HelperBean.proxy.remoteUrl","message":"Proxy URL blocked: Failed to resolve host: upstream.bench.invalid"}]`
