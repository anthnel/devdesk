package enginefixture_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-08b, review of the first commit: quoted environment values, the names an
// engine writes itself, the path forms that were missed, the short lab term and
// the stop list. Every case reuses the base fixture and the helpers of
// leaks_test.go.

func TestReviewLeaksAreFound(t *testing.T) {
	tests := []leakCase{
		// sensitive-env-value: a quoted value is a value.
		{name: "env/double quotes", mutate: inStdout(`X_PASSWORD="zzpw9"`), planted: "zzpw9", rules: []string{rEnv}},
		{name: "env/single quotes after export", mutate: inStdout("export DB_PASSWORD='zzpw9'"), planted: "zzpw9", rules: []string{rEnv}},
		{name: "env/quoted in an argv", mutate: inArgv("run", "-e", `MY_TOKEN="zzv9"`, "alpine"), planted: "zzv9", rules: []string{rEnv}},
		{name: "env/JSON-escaped quotes", mutate: inStdout(`MY_TOKEN=\"zzv9\"`), planted: "zzv9", rules: []string{rEnv}},
		{name: "env/a public name with another spelling", mutate: inStdout("GPG_KEYS=zzval9"), planted: "zzval9", rules: []string{rEnv}},

		// personal-path: more ways to write a home.
		{name: "path/file URL", mutate: inStdout("file:///home/zed9/x"), planted: "file:///home/zed9/x", rules: []string{rPath}},
		{name: "path/file URL on macOS", mutate: inStdout("see file:///Users/zed9/x"), planted: "file:///Users/zed9/x", rules: []string{rPath}},
		{name: "path/JSON-escaped slashes", mutate: inStdout(`\/home\/zed9\/x`), planted: `\/home\/zed9`, rules: []string{rPath}},
		{name: "path/JSON-escaped root", mutate: inStdout(`\/root\/x`), planted: `\/root\/x`, rules: []string{rPath}},
		{name: "path/WSL mount of a Windows profile", mutate: inStdout("/mnt/c/Users/zed9/x"), planted: "/mnt/c/Users/zed9/x", rules: []string{rPath}},
		{name: "path/WSL mount, another drive and case", mutate: inStdout("cd /mnt/d/users/zed9"), planted: "/mnt/d/users/zed9", rules: []string{rPath}},

		// lab-identity: the short term keeps the boundaries of a value, the long ones
		// keep the dot.
		{name: "lab/dd between quotes", mutate: inStdout(`user "dd" here`), planted: `"dd"`, rules: []string{rLab}},
		{name: "lab/dd before a colon", mutate: inStdout("dd:x:1000"), planted: "dd:x:1000", rules: []string{rLab}},
		{name: "lab/dd in a path", mutate: inStdout("/etc/dd/x"), planted: "/etc/dd/x", rules: []string{rLab}},
		{name: "lab/dd after an equal sign", mutate: inStdout("USER=dd"), planted: "USER=dd", rules: []string{rLab}},
		{name: "lab/dd alone on the line", mutate: inStdout("a\ndd\nb"), planted: "dd\nb", rules: []string{rLab}},
		{name: "lab/dd-docker before a dot", mutate: inStdout("dd-docker.corp.example"), planted: "dd-docker.corp.example", rules: []string{rLab}},
		{name: "lab/dd-podman after a dot", mutate: inStdout("srv.dd-podman"), planted: "srv.dd-podman", rules: []string{rLab}},
		{name: "lab/dd inside a name that stays an internal one", mutate: inStdout("dd.home.arpa"), planted: "dd.home.arpa", rules: []string{rInternal}},

		// internal-name: a name next to an allowed one is not allowed.
		{name: "name/under an allowed engine name", mutate: inStdout("box.host.docker.internal"), planted: "box.host.docker.internal", rules: []string{rInternal}},
		{name: "name/the cluster domain alone", mutate: inStdout("svc.cluster.local"), planted: "svc.cluster.local", rules: []string{rInternal}},
		{name: "name/another cluster domain", mutate: inStdout("api.default.cluster.local"), planted: "api.default.cluster.local", rules: []string{rInternal}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { assertPlanted(t, tc) })
	}
}

func TestReviewCleanCases(t *testing.T) {
	tests := []cleanCase{
		// sensitive-env-value: no value, a redacted value, a public one.
		{name: rEnv + "/empty double quotes", rule: rEnv, m: inStdout(`MY_TOKEN=""`)},
		{name: rEnv + "/empty single quotes", rule: rEnv, m: inStdout("MY_TOKEN=''")},
		{name: rEnv + "/redacted between quotes", rule: rEnv, m: inStdout(`MY_TOKEN="<redacted>"`)},
		{name: rEnv + "/export of an empty value", rule: rEnv, m: inStdout("export DB_PASSWORD=")},
		{name: rEnv + "/empty quotes in an argv", rule: rEnv, m: inArgv("run", "-e", `MY_TOKEN=""`, "alpine")},
		{name: rEnv + "/redacted quoted in an argv", rule: rEnv, m: inArgv("run", "-e", `MY_TOKEN="<redacted>"`, "alpine")},
		{name: rEnv + "/the public key id of the python image", rule: rEnv, m: inStdout("GPG_KEY=A035C8C19219BA821ECEA86B64E628F8D684696D")},

		// personal-path
		{name: rPath + "/file URL of the replacement home", rule: rPath, m: inStdout("file:///home/fixture-user/x")},
		{name: rPath + "/JSON-escaped replacement home", rule: rPath, m: inStdout(`\/home\/fixture-user\/x`)},
		{name: rPath + "/WSL mount of the replacement home", rule: rPath, m: inStdout("/mnt/c/Users/fixture-user/x")},
		{name: rPath + "/WSL mount outside Users", rule: rPath, m: inStdout("/mnt/c/Windows/System32")},
		{name: rPath + "/still not /rootfs, escaped", rule: rPath, m: inStdout(`\/rootfs\/x`)},
		{name: rPath + "/still not an image path, escaped", rule: rPath, m: inStdout(`team\/home\/api`)},
		{name: rPath + "/a file URL outside the homes", rule: rPath, m: inStdout("file:///usr/share/x")},

		// lab-identity: the short term is not at a boundary of a value.
		{name: rLab + "/dd.conf", rule: rLab, m: inStdout("dd.conf")},
		{name: rLab + "/a date format", rule: rLab, m: inStdout("yyyy-MM-dd")},
		{name: rLab + "/dd-mm", rule: rLab, m: inStdout("run dd-mm")},
		{name: rLab + "/after a dot", rule: rLab, m: inStdout("file.dd")},
		{name: rLab + "/before a dot and a name", rule: rLab, m: inStdout("dd.example.org")},

		// internal-name: what an engine or a cluster writes itself.
		{name: rInternal + "/the Docker host name", rule: rInternal, m: inStdout("host.docker.internal")},
		{name: rInternal + "/the Podman host name", rule: rInternal, m: inStdout("host.containers.internal")},
		{name: rInternal + "/an ExtraHosts entry", rule: rInternal, m: inStdout("host.docker.internal:host-gateway")},
		{name: rInternal + "/the Docker host name in an argv", rule: rInternal, m: inArgv("run", "--add-host", "host.docker.internal:host-gateway", "alpine")},
		{name: rInternal + "/in upper case", rule: rInternal, m: inStdout("HOST.DOCKER.INTERNAL")},
		{name: rInternal + "/a cluster service", rule: rInternal, m: inStdout("kubernetes.default.svc.cluster.local")},
		{name: rInternal + "/a cluster service with a port", rule: rInternal, m: inStdout("api.team.svc.cluster.local:8443")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := plantInto(t, tc.m)
			if leaks := enginefixture.CheckLeaksAs("clean--docker", data, tc.exact, tc.local); len(leaks) != 0 {
				t.Errorf("CheckLeaksAs = %v, want none for:\n%s", leaks, data)
			}
		})
	}
}

func TestTheExceptionListsAreExportedAndEachHasItsReason(t *testing.T) {
	lists := map[string]map[string]string{
		"AllowedInternalNames":     enginefixture.AllowedInternalNames,
		"AllowedSensitiveEnvNames": enginefixture.AllowedSensitiveEnvNames,
	}
	for name, list := range lists {
		if len(list) == 0 {
			t.Errorf("%s is empty: it starts with the names an engine writes itself", name)
		}
		for entry, reason := range list {
			if strings.TrimSpace(reason) == "" {
				t.Errorf("%s[%q] has no reason", name, entry)
			}
		}
	}
	for _, want := range []string{"host.docker.internal", "host.containers.internal", "*.svc.cluster.local"} {
		if _, ok := enginefixture.AllowedInternalNames[want]; !ok {
			t.Errorf("AllowedInternalNames has no %q", want)
		}
	}
	if _, ok := enginefixture.AllowedSensitiveEnvNames["GPG_KEY"]; !ok {
		t.Error("AllowedSensitiveEnvNames has no GPG_KEY")
	}
}

// An allowed name is allowed because it is listed: without the entry it is reported.
func TestTheExceptionListsAreWhatLetsANameThrough(t *testing.T) {
	oldNames, oldEnv := enginefixture.AllowedInternalNames, enginefixture.AllowedSensitiveEnvNames
	t.Cleanup(func() { enginefixture.AllowedInternalNames, enginefixture.AllowedSensitiveEnvNames = oldNames, oldEnv })

	host := plantInto(t, inStdout("host.docker.internal"))
	key := plantInto(t, inStdout("GPG_KEY=A035C8C19219BA821ECEA86B64E628F8D684696D"))

	enginefixture.AllowedInternalNames, enginefixture.AllowedSensitiveEnvNames = map[string]string{}, map[string]string{}
	if len(enginefixture.CheckLeaks("host--docker", host, nil)) == 0 {
		t.Error("host.docker.internal is allowed with an empty AllowedInternalNames")
	}
	if len(enginefixture.CheckLeaks("key--docker", key, nil)) == 0 {
		t.Error("GPG_KEY is allowed with an empty AllowedSensitiveEnvNames")
	}

	enginefixture.AllowedInternalNames = map[string]string{"box.test.internal": "a name of the test"}
	box := plantInto(t, inStdout("box.test.internal and other.test.internal"))
	leaks := enginefixture.CheckLeaks("box--docker", box, nil)
	if len(leaks) != 1 || leaks[0].Rule != rInternal {
		t.Errorf("CheckLeaks = %v, want the one internal-name leak of other.test.internal (box.test.internal is listed)", leaks)
	}
}

func TestLocalIdentityTermsStopListHoldsTheNamesOfCommonImagesAndHosts(t *testing.T) {
	for _, name := range []string{"localhost", "Debian", "FEDORA", "alpine", "node"} {
		if got := enginefixture.LocalIdentityTerms(name); len(got) != 0 {
			t.Errorf("LocalIdentityTerms(%q) = %q, want none: too common to tell a machine apart", name, got)
		}
	}
	if got := enginefixture.LocalIdentityTerms("nodes-01", "debian-box"); !slices.Equal(got, []string{"nodes-01", "debian-box"}) {
		t.Errorf("LocalIdentityTerms kept %q: only the whole name is in the stop list", got)
	}
}
