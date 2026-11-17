package nexusbench

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// The tests here never reach a real skopeo: PATH holds a stand-in started
// through the helper process (helper_process_test.go), which records its
// arguments, its environment and the files it was pointed at. They need no
// cluster, no network and no requireBench guard. TestSkopeoAgainstFakeRegistry
// (fakeregistry_test.go) is the one test that runs the real binary, against a
// registry served from the test process.

const (
	srcHost  = "127.0.0.1:41234"
	destHost = "127.0.0.1:43210"
	srcPass  = "src-pw-9f3a1c"
	destPass = "dest-pw-77c1d2"
)

// newHelperSkopeo installs the stand-in skopeo (alone on PATH) and returns the
// copier that finds it.
func newHelperSkopeo(t *testing.T, env map[string]string) (ImageCopier, helperTool) {
	t.Helper()
	tool := installHelperTool(t, "skopeo", env)
	c, err := NewSkopeoCopier()
	if err != nil {
		t.Fatalf("NewSkopeoCopier: %v", err)
	}
	return c, tool
}

// refArg is how skopeo is given an ImageRef: docker://<location>, or
// <transport>:<location>.
func refArg(r ImageRef) string {
	if r.Transport == "docker" {
		return "docker://" + r.Location
	}
	return r.Transport + ":" + r.Location
}

// manifestBody is a valid-looking manifest the stand-in prints for inspect.
const manifestBody = `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","size":2},"layers":[]}`

func sha256Digest(data string) string {
	sum := sha256.Sum256([]byte(data))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ---- NewSkopeoCopier ---------------------------------------------------------

func TestNewSkopeoCopierSkopeoMissing(t *testing.T) {
	// An empty directory as the whole PATH: no skopeo can be found, whatever
	// the host has installed.
	t.Setenv("PATH", t.TempDir())

	c, err := NewSkopeoCopier()
	if err == nil {
		t.Fatal("NewSkopeoCopier succeeded without skopeo on PATH")
	}
	if c != nil {
		t.Errorf("copier = %v, want nil on error", c)
	}
	var be *BenchError
	if !errors.As(err, &be) {
		t.Fatalf("error %T is not a *BenchError: %v", err, err)
	}
	if be.Kind != SkopeoMissing {
		t.Errorf("Kind = %d, want SkopeoMissing (%d)", be.Kind, SkopeoMissing)
	}
	if !strings.Contains(err.Error(), "skopeo") {
		t.Errorf("message %q does not name skopeo", err.Error())
	}
}

// ---- the arguments of copy -----------------------------------------------------

// credCombo is one choice of credentials for a copy.
type credCombo struct {
	name     string
	src, dst *Credentials
}

// Every no-credentials, TLS and digest flag follows from the two references and
// the options; nothing else decides it.
func TestSkopeoCopyArguments(t *testing.T) {
	srcs := map[string]ImageRef{
		"oci":    {Transport: "oci", Location: "/var/tmp/layout:img-01"},
		"dir":    {Transport: "dir", Location: "/var/tmp/in"},
		"docker": {Transport: "docker", Location: srcHost + "/mirror/cached:1.0"},
	}
	dsts := map[string]ImageRef{
		"oci":    {Transport: "oci", Location: "/var/tmp/out-layout:img-01"},
		"dir":    {Transport: "dir", Location: "/var/tmp/out"},
		"docker": {Transport: "docker", Location: destHost + "/docker-hosted/api:1.0.0"},
	}
	srcCreds := &Credentials{User: "bench-pull", Password: srcPass}
	destCreds := &Credentials{User: "admin", Password: destPass}

	for _, sn := range []string{"oci", "dir", "docker"} {
		for _, dn := range []string{"oci", "dir", "docker"} {
			// Credentials only make sense on a registry side.
			combos := []credCombo{{"anonymous", nil, nil}}
			if sn == "docker" {
				combos = append(combos, credCombo{"source credentials", srcCreds, nil})
			}
			if dn == "docker" {
				combos = append(combos, credCombo{"destination credentials", nil, destCreds})
			}
			if sn == "docker" && dn == "docker" {
				combos = append(combos, credCombo{"both credentials", srcCreds, destCreds})
			}
			for _, combo := range combos {
				for _, preserve := range []bool{false, true} {
					name := fmt.Sprintf("%s to %s, %s, preserve=%v", sn, dn, combo.name, preserve)
					t.Run(name, func(t *testing.T) {
						c, tool := newHelperSkopeo(t, nil)
						src, dst := srcs[sn], dsts[dn]
						o := CopyOptions{SrcCreds: combo.src, DestCreds: combo.dst, PreserveDigests: preserve}

						if err := c.Copy(context.Background(), src, dst, o); err != nil {
							t.Fatalf("Copy: %v", err)
						}

						rec := tool.last(t)
						checkCopyArgs(t, rec, src, dst, o)
						checkNoSecretLeak(t, rec, srcPass, destPass)
						checkAuthfiles(t, rec, src, dst, o)
					})
				}
			}
		}
	}
}

func checkCopyArgs(t *testing.T, rec helperRecord, src, dst ImageRef, o CopyOptions) {
	t.Helper()
	args := rec.Args
	copyAt := slices.Index(args, "copy")
	if copyAt < 0 {
		t.Fatalf("no copy subcommand in %q", args)
	}
	if len(args) < copyAt+3 || args[len(args)-2] != refArg(src) || args[len(args)-1] != refArg(dst) {
		t.Errorf("the arguments end with %q, want the source %q then the destination %q", args[max(0, len(args)-2):], refArg(src), refArg(dst))
	}
	if policyAt := slices.IndexFunc(args, func(a string) bool { return a == "--policy" || strings.HasPrefix(a, "--policy=") }); policyAt < 0 || policyAt > copyAt {
		t.Errorf("--policy must be given, before the copy subcommand: %q", args)
	}
	if slices.Contains(args, "--insecure-policy") {
		t.Errorf("--insecure-policy is used, want an explicit policy file: %q", args)
	}

	for _, side := range []struct {
		name  string
		ref   ImageRef
		creds *Credentials
	}{{"src", src, o.SrcCreds}, {"dest", dst, o.DestCreds}} {
		tls := "--" + side.name + "-tls-verify"
		switch {
		case side.ref.Transport == "docker" && !slices.Contains(args, tls+"=false"):
			t.Errorf("%s is a docker reference but %s=false is missing: %q", side.name, tls, args)
		case side.ref.Transport != "docker" && argWithPrefix(args, tls):
			t.Errorf("%s is %s, not a registry, but %s is given: %q", side.name, side.ref.Transport, tls, args)
		}
		for _, a := range args {
			if strings.HasPrefix(a, tls) && a != tls+"=false" {
				t.Errorf("TLS verification must only ever be switched off, got %q", a)
			}
		}
		noCreds := "--" + side.name + "-no-creds"
		if has := slices.Contains(args, noCreds); has != (side.creds == nil) {
			t.Errorf("%s present = %v, want %v (credentials given: %v): %q", noCreds, has, side.creds == nil, side.creds != nil, args)
		}
	}
	if has := slices.Contains(args, "--preserve-digests"); has != o.PreserveDigests {
		t.Errorf("--preserve-digests present = %v, want %v: %q", has, o.PreserveDigests, args)
	}
}

func argWithPrefix(args []string, prefix string) bool {
	return slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, prefix) })
}

// checkNoSecretLeak fails when a password, or a flag that carries one, shows in
// the arguments (visible to every account through /proc) or in the environment.
func checkNoSecretLeak(t *testing.T, rec helperRecord, passwords ...string) {
	t.Helper()
	for _, a := range rec.Args {
		for _, pw := range passwords {
			if strings.Contains(a, pw) {
				t.Errorf("argument %q carries a password", a)
			}
		}
		for _, banned := range []string{"--creds", "--src-creds", "--dest-creds", "--src-password", "--dest-password", "--password", "--src-username", "--dest-username", "--username", "--src-registry-token", "--dest-registry-token", "--registry-token"} {
			if a == banned || strings.HasPrefix(a, banned+"=") {
				t.Errorf("argument %q hands a credential to skopeo on the command line", a)
			}
		}
	}
	for _, kv := range rec.Env {
		for _, pw := range passwords {
			if strings.Contains(kv, pw) {
				name, _, _ := strings.Cut(kv, "=")
				t.Errorf("the environment variable %s carries a password", name)
			}
		}
	}
}

// checkAuthfiles checks that the credentials of a side reach skopeo through an
// authentication file, 0600 in a 0700 folder, and only when there are some.
func checkAuthfiles(t *testing.T, rec helperRecord, src, dst ImageRef, o CopyOptions) {
	t.Helper()
	if o.SrcCreds == nil && o.DestCreds == nil {
		if len(rec.Authfiles) != 0 {
			t.Errorf("an authentication file is given without credentials: %+v", rec.Authfiles)
		}
		return
	}
	for _, af := range rec.Authfiles {
		if af.Mode != 0o600 {
			t.Errorf("%s %s has mode %04o, want 0600", af.Flag, af.Path, af.Mode)
		}
		if af.DirMode != 0o700 {
			t.Errorf("the folder of %s has mode %04o, want 0700", af.Path, af.DirMode)
		}
	}
	for _, side := range []struct {
		ref   ImageRef
		creds *Credentials
	}{{src, o.SrcCreds}, {dst, o.DestCreds}} {
		if side.creds == nil {
			continue
		}
		host, _, _ := strings.Cut(side.ref.Location, "/")
		if !authfilesHold(rec.Authfiles, host, side.creds) {
			t.Errorf("no authentication file holds the credentials of %s for %s: %+v", side.creds.User, host, rec.Authfiles)
		}
	}
}

// authfilesHold tells whether one of the files, read as a container auth file
// ({"auths": {"<host>": {"auth": base64(user:password)}}}), has the credentials
// for host (a key may carry a path under the host).
func authfilesHold(files []helperAuthfile, host string, c *Credentials) bool {
	for _, af := range files {
		var cfg struct {
			Auths map[string]struct {
				Auth string `json:"auth"`
			} `json:"auths"`
		}
		if json.Unmarshal([]byte(af.Content), &cfg) != nil {
			continue
		}
		for key, entry := range cfg.Auths {
			if key != host && !strings.HasPrefix(key, host+"/") {
				continue
			}
			if raw, err := base64.StdEncoding.DecodeString(entry.Auth); err == nil && string(raw) == c.User+":"+c.Password {
				return true
			}
		}
	}
	return false
}

// ---- the loopback rule -----------------------------------------------------------

// Whatever skopeo is given, TLS verification is switched off for a docker
// reference, so a host that is not the loopback is refused before any process
// starts.
func TestSkopeoRefusesANonLoopbackDockerReference(t *testing.T) {
	locations := []string{
		"192.0.2.1:30481/docker-hosted/api:1.0.0", // a LAN address (TEST-NET-1)
		"example.com/library/busybox:1",
		"example.com:5000/library/busybox:1",
		// localhost is not accepted: it resolves through the host's own files
		// and resolver, which the bench does not control. Only the literals do.
		"localhost:5000/x:1",
		"localhost/x:1",
		"LOCALHOST:5000/x:1",
		// No path: skopeo reads such a reference as an image of docker.io, with
		// TLS verification off, whatever the host part looks like.
		"localhost:5000",
		"localhost",
		"127.0.0.1:5000",
		"127.0.0.1",
		"[::1]:5000",
		"[::1]",
		"127.0.0.1:5000/", // an empty path is no path
		"localhost.evil.com/x:1",
		"localhost.evil.com:5000/x:1",
		"127.0.0.1.evil.com/x:1",
		"127.0.0.1.evil.com:5000/x:1",
		"127.0.0.1@evil.com/x:1",
		"127.0.0.1:5000@evil.com/x:1",
		"evil.com/127.0.0.1:5000/x:1",
		"0.0.0.0:5000/x:1",
		"10.0.0.1:5000/x:1",
		"[::2]:5000/x:1",
		"nexus.bench.svc.cluster.local:5000/x:1",
		"/x:1", // no host
		"",
	}
	creds := &Credentials{User: "admin", Password: destPass}
	local := ImageRef{Transport: "dir", Location: "/var/tmp/out"}

	for _, loc := range locations {
		bad := ImageRef{Transport: "docker", Location: loc}
		for _, op := range []string{"source", "destination", "inspect"} {
			t.Run(op+" "+loc, func(t *testing.T) {
				c, tool := newHelperSkopeo(t, nil)
				var err error
				switch op {
				case "source":
					err = c.Copy(context.Background(), bad, local, CopyOptions{SrcCreds: creds})
				case "destination":
					err = c.Copy(context.Background(), local, bad, CopyOptions{DestCreds: creds})
				case "inspect":
					_, err = c.ManifestDigest(context.Background(), bad, creds)
				}
				if err == nil {
					t.Fatalf("%s %q was accepted", op, loc)
				}
				var be *BenchError
				if !errors.As(err, &be) {
					t.Errorf("error %T is not a *BenchError: %v", err, err)
				}
				if strings.Contains(err.Error(), destPass) {
					t.Errorf("the message carries a password: %q", err.Error())
				}
				tool.neverRan(t)
				entries, _ := os.ReadDir(tool.scratch)
				if len(entries) != 0 {
					t.Errorf("the refusal left %d file(s) in the temporary folder", len(entries))
				}
			})
		}
	}
}

func TestSkopeoAcceptsLoopbackDockerReferences(t *testing.T) {
	locations := []string{
		"127.0.0.1:41234/docker-hosted/api:1.0.0",
		"127.0.0.1/x:1",
		"127.0.0.1:5000/x@sha256:" + strings.Repeat("a", 64),
		"[::1]:5000/x:1",
		"[::1]/x:1",
		"127.0.0.1:5000/a/b/c:1",
	}
	local := ImageRef{Transport: "dir", Location: "/var/tmp/out"}
	for _, loc := range locations {
		t.Run(loc, func(t *testing.T) {
			c, tool := newHelperSkopeo(t, nil)
			good := ImageRef{Transport: "docker", Location: loc}

			if err := c.Copy(context.Background(), good, local, CopyOptions{}); err != nil {
				t.Fatalf("Copy from %q: %v", loc, err)
			}
			if err := c.Copy(context.Background(), local, good, CopyOptions{}); err != nil {
				t.Fatalf("Copy to %q: %v", loc, err)
			}
			if got := len(tool.records(t)); got != 2 {
				t.Errorf("skopeo ran %d time(s), want 2", got)
			}
		})
	}
}

// Only a docker reference is checked: an oci or dir location is a path.
func TestSkopeoDoesNotReadAPathAsAHost(t *testing.T) {
	c, tool := newHelperSkopeo(t, nil)
	src := ImageRef{Transport: "oci", Location: "/home/example.com/layout:img-01"}
	dst := ImageRef{Transport: "dir", Location: "/srv/192.0.2.1/out"}

	if err := c.Copy(context.Background(), src, dst, CopyOptions{}); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	tool.last(t)
}

// ---- the environment -------------------------------------------------------------

// removedFromSkopeoEnvironment are the variables that would let the host's own
// settings (proxies, an authentication file, containers configuration) change
// where skopeo goes or who it is. CONTAINERS_REGISTRIES_CONF is replaced, not
// dropped.
var removedFromSkopeoEnvironment = []string{
	"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy",
	"REGISTRY_AUTH_FILE",
	"CONTAINERS_CONF", "CONTAINERS_STORAGE_CONF", "CONTAINERS_POLICY", "CONTAINERS_CERT_DIR", "CONTAINERS_ANYTHING_ELSE",
}

func TestSkopeoEnvironment(t *testing.T) {
	for _, name := range removedFromSkopeoEnvironment {
		t.Setenv(name, "http://elsewhere.invalid:3128")
	}
	t.Setenv("CONTAINERS_REGISTRIES_CONF", "/etc/containers/registries.conf")
	t.Setenv("DEVDESK_KEEP", "1")
	t.Setenv("HOME", "/home/keep")

	env := skopeoEnvironment("/run/empty/registries.conf")

	counts := map[string]int{}
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		counts[name]++
		if slices.Contains(removedFromSkopeoEnvironment, name) {
			t.Errorf("skopeoEnvironment keeps %s", kv)
		}
	}
	if !slices.Contains(env, "CONTAINERS_REGISTRIES_CONF=/run/empty/registries.conf") || counts["CONTAINERS_REGISTRIES_CONF"] != 1 {
		t.Errorf("CONTAINERS_REGISTRIES_CONF is not pinned to the empty file exactly once (seen %d, env %q)", counts["CONTAINERS_REGISTRIES_CONF"], env)
	}
	for _, keep := range []string{"DEVDESK_KEEP=1", "HOME=/home/keep"} {
		if !slices.Contains(env, keep) {
			t.Errorf("skopeoEnvironment dropped %s, which is not about proxies or containers settings", keep)
		}
	}
	if _, ok := envOf(env, "PATH"); !ok {
		t.Error("skopeoEnvironment dropped PATH")
	}
}

// The same rule seen from the process: what skopeo is started with.
func TestSkopeoRunsWithACleanEnvironment(t *testing.T) {
	c, tool := newHelperSkopeo(t, nil)
	for _, name := range removedFromSkopeoEnvironment {
		t.Setenv(name, "http://elsewhere.invalid:3128")
	}
	t.Setenv("CONTAINERS_REGISTRIES_CONF", "/etc/containers/registries.conf")
	t.Setenv("DEVDESK_KEEP", "1")

	err := c.Copy(context.Background(),
		ImageRef{Transport: "docker", Location: srcHost + "/x:1"},
		ImageRef{Transport: "dir", Location: "/var/tmp/out"},
		CopyOptions{})
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}

	rec := tool.last(t)
	for _, kv := range rec.Env {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(removedFromSkopeoEnvironment, name) {
			t.Errorf("skopeo was started with %s", kv)
		}
	}
	if v, _ := envOf(rec.Env, "DEVDESK_KEEP"); v != "1" {
		t.Error("skopeo was started without a variable that is not about proxies or containers settings")
	}
	if rec.RegistriesConf == "" || rec.RegistriesConf == "/etc/containers/registries.conf" {
		t.Fatalf("CONTAINERS_REGISTRIES_CONF = %q, want a file of the bench", rec.RegistriesConf)
	}
	if !rec.RegistriesConfExists || rec.RegistriesConfSize != 0 {
		t.Errorf("registries.conf exists = %v, size = %d, want an existing empty file while skopeo runs", rec.RegistriesConfExists, rec.RegistriesConfSize)
	}
	if !strings.HasPrefix(rec.RegistriesConf, tool.scratch) {
		t.Errorf("registries.conf %s is outside the temporary folder %s", rec.RegistriesConf, tool.scratch)
	}
}

// ---- the policy ------------------------------------------------------------------

func TestSkopeoPolicyAcceptsAnythingFromATemporaryFile(t *testing.T) {
	c, tool := newHelperSkopeo(t, nil)

	err := c.Copy(context.Background(),
		ImageRef{Transport: "oci", Location: "/var/tmp/layout:img-01"},
		ImageRef{Transport: "docker", Location: destHost + "/x:1"},
		CopyOptions{})
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}

	rec := tool.last(t)
	if rec.PolicyPath == "" {
		t.Fatalf("no --policy in %q", rec.Args)
	}
	if !strings.HasPrefix(rec.PolicyPath, tool.scratch) {
		t.Errorf("the policy %s is outside the temporary folder %s", rec.PolicyPath, tool.scratch)
	}
	var policy struct {
		Default []struct {
			Type string `json:"type"`
		} `json:"default"`
		Transports map[string]any `json:"transports"`
	}
	if err := json.Unmarshal([]byte(rec.Policy), &policy); err != nil {
		t.Fatalf("the policy is not JSON: %v\n%s", err, rec.Policy)
	}
	if len(policy.Default) != 1 || policy.Default[0].Type != "insecureAcceptAnything" {
		t.Errorf("policy default = %+v, want exactly insecureAcceptAnything", policy.Default)
	}
	if len(policy.Transports) != 0 {
		t.Errorf("the policy has per-transport rules %v, want the default alone", policy.Transports)
	}
}

// ---- nothing left behind ---------------------------------------------------------

func scratchIsEmpty(t *testing.T, tool helperTool) {
	t.Helper()
	entries, err := os.ReadDir(tool.scratch)
	if err != nil {
		t.Fatalf("reading the temporary folder: %v", err)
	}
	for _, e := range entries {
		t.Errorf("%s was left in the temporary folder", e.Name())
	}
}

func TestSkopeoLeavesNothingBehind(t *testing.T) {
	creds := &Credentials{User: "admin", Password: destPass}
	src := ImageRef{Transport: "oci", Location: "/var/tmp/layout:img-01"}
	dst := ImageRef{Transport: "docker", Location: destHost + "/x:1"}

	t.Run("success", func(t *testing.T) {
		c, tool := newHelperSkopeo(t, nil)
		if err := c.Copy(context.Background(), src, dst, CopyOptions{DestCreds: creds}); err != nil {
			t.Fatalf("Copy: %v", err)
		}
		// The file existed, with the credentials, while skopeo ran...
		if rec := tool.last(t); !authfilesHold(rec.Authfiles, destHost, creds) {
			t.Fatalf("the authentication file did not hold the credentials while skopeo ran: %+v", rec.Authfiles)
		}
		// ...and is gone now.
		scratchIsEmpty(t, tool)
	})

	t.Run("skopeo fails", func(t *testing.T) {
		c, tool := newHelperSkopeo(t, map[string]string{helperExitEnv: "1", helperStderrEnv: "FATA[0000] boom\n"})
		if err := c.Copy(context.Background(), src, dst, CopyOptions{DestCreds: creds}); err == nil {
			t.Fatal("Copy succeeded although skopeo exited 1")
		}
		if rec := tool.last(t); len(rec.Authfiles) == 0 || rec.Authfiles[0].Content == "" {
			t.Fatalf("the authentication file did not exist while skopeo ran: %+v", rec.Authfiles)
		}
		scratchIsEmpty(t, tool)
	})

	t.Run("inspect fails", func(t *testing.T) {
		c, tool := newHelperSkopeo(t, map[string]string{helperExitEnv: "1", helperStderrEnv: "FATA[0000] boom\n"})
		if _, err := c.ManifestDigest(context.Background(), dst, creds); err == nil {
			t.Fatal("ManifestDigest succeeded although skopeo exited 1")
		}
		scratchIsEmpty(t, tool)
	})

	t.Run("context cancelled while skopeo runs", func(t *testing.T) {
		c, tool := newHelperSkopeo(t, map[string]string{helperHangEnv: "1"})
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			tool.waitRan(5 * time.Second)
			cancel()
		}()
		done := make(chan error, 1)
		go func() { done <- c.Copy(ctx, src, dst, CopyOptions{DestCreds: creds}) }()
		select {
		case err := <-done:
			if err == nil {
				t.Error("Copy succeeded on a cancelled context")
			}
		case <-time.After(10 * time.Second):
			cancel()
			t.Fatal("Copy did not return after the cancellation")
		}
		if !waitProcessGone(t, tool.pid(t), 5*time.Second) {
			t.Error("skopeo survived the cancellation of its context")
		}
		scratchIsEmpty(t, tool)
	})

	t.Run("context already cancelled", func(t *testing.T) {
		c, tool := newHelperSkopeo(t, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := c.Copy(ctx, src, dst, CopyOptions{DestCreds: creds}); err == nil {
			t.Error("Copy succeeded on a cancelled context")
		}
		scratchIsEmpty(t, tool)
	})
}

// ---- the digest of a manifest ----------------------------------------------------

func TestSkopeoManifestDigest(t *testing.T) {
	ref := ImageRef{Transport: "docker", Location: srcHost + "/mirror/cached:1.0"}
	creds := &Credentials{User: "bench-pull", Password: srcPass}

	tests := []struct {
		name      string
		creds     *Credentials
		wantNoCrd bool
	}{
		{"anonymous", nil, true},
		{"with credentials", creds, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, tool := newHelperSkopeo(t, map[string]string{helperStdoutEnv: manifestBody})

			got, err := c.ManifestDigest(context.Background(), ref, tt.creds)
			if err != nil {
				t.Fatalf("ManifestDigest: %v", err)
			}

			// The digest is computed here from the raw manifest, not read from
			// anything skopeo says about it.
			if want := sha256Digest(manifestBody); got != want {
				t.Errorf("digest = %q, want %q", got, want)
			}
			rec := tool.last(t)
			inspectAt := slices.Index(rec.Args, "inspect")
			if inspectAt < 0 || !slices.Contains(rec.Args, "--raw") {
				t.Errorf("want `skopeo inspect --raw`, got %q", rec.Args)
			}
			if last := rec.Args[len(rec.Args)-1]; last != refArg(ref) {
				t.Errorf("last argument = %q, want %q", last, refArg(ref))
			}
			if !slices.Contains(rec.Args, "--tls-verify=false") {
				t.Errorf("--tls-verify=false is missing for a docker reference: %q", rec.Args)
			}
			if has := slices.Contains(rec.Args, "--no-creds"); has != tt.wantNoCrd {
				t.Errorf("--no-creds present = %v, want %v: %q", has, tt.wantNoCrd, rec.Args)
			}
			checkNoSecretLeak(t, rec, srcPass)
			if tt.creds != nil {
				if len(rec.Authfiles) == 0 || !authfilesHold(rec.Authfiles, srcHost, tt.creds) {
					t.Errorf("the credentials did not reach skopeo through an authentication file: %+v", rec.Authfiles)
				}
				for _, af := range rec.Authfiles {
					if af.Mode != 0o600 || af.DirMode != 0o700 {
						t.Errorf("authentication file mode %04o in a folder of mode %04o, want 0600 in 0700", af.Mode, af.DirMode)
					}
				}
			} else if len(rec.Authfiles) != 0 {
				t.Errorf("an authentication file is given without credentials: %+v", rec.Authfiles)
			}
			scratchIsEmpty(t, tool)
		})
	}
}

func TestSkopeoManifestDigestOnAnOCILayout(t *testing.T) {
	c, tool := newHelperSkopeo(t, map[string]string{helperStdoutEnv: manifestBody})

	got, err := c.ManifestDigest(context.Background(), ImageRef{Transport: "oci", Location: "/var/tmp/layout:img-01"}, nil)
	if err != nil {
		t.Fatalf("ManifestDigest: %v", err)
	}
	if want := sha256Digest(manifestBody); got != want {
		t.Errorf("digest = %q, want %q", got, want)
	}
	if argWithPrefix(tool.last(t).Args, "--tls-verify") {
		t.Errorf("--tls-verify is given for an oci reference: %q", tool.last(t).Args)
	}
}

func TestSkopeoManifestDigestFailure(t *testing.T) {
	c, _ := newHelperSkopeo(t, map[string]string{
		helperStdoutEnv: "stdout-marker-77",
		helperStderrEnv: "FATA[0000] reading manifest 1.0: manifest unknown; password=" + srcPass + "\n",
		helperExitEnv:   "1",
	})

	d, err := c.ManifestDigest(context.Background(), ImageRef{Transport: "docker", Location: srcHost + "/x:1.0"}, nil)
	if err == nil {
		t.Fatalf("ManifestDigest = %q, want an error", d)
	}
	if d != "" {
		t.Errorf("digest = %q on error, want empty", d)
	}
	var be *BenchError
	if !errors.As(err, &be) {
		t.Fatalf("error %T is not a *BenchError: %v", err, err)
	}
	if be.Kind != SeedFailed {
		t.Errorf("Kind = %d, want SeedFailed (%d)", be.Kind, SeedFailed)
	}
	requireNoSkopeoOutput(t, err)
}

// requireNoSkopeoOutput fails when the error carries what a process printed.
func requireNoSkopeoOutput(t *testing.T, err error) {
	t.Helper()
	for _, secret := range []string{srcPass, destPass, "stdout-marker", "FATA[", "manifest unknown", "connection refused", "unauthorized"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the message carries %q of the output of skopeo: %q", secret, err.Error())
		}
	}
	for e := errors.Unwrap(err); e != nil; e = errors.Unwrap(e) {
		for _, secret := range []string{srcPass, destPass} {
			if strings.Contains(e.Error(), secret) {
				t.Errorf("a wrapped error carries a password: %q", e.Error())
			}
		}
	}
}

// ---- errors ----------------------------------------------------------------------

func TestSkopeoCopyErrorsAreClassifiedAndQuiet(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   ErrKind
		strict bool // the kind must be exactly want
	}{
		{"connection refused", "FATA[0000] initializing source docker://" + srcHost + "/x:1: pinging container registry: Get \"http://" + srcHost + "/v2/\": dial tcp " + srcHost + ": connect: connection refused", RegistryUnreachable, true},
		{"no such host", "FATA[0000] dial tcp: lookup nexus.invalid: no such host", RegistryUnreachable, true},
		{"timeout", "FATA[0000] dial tcp 10.0.0.1:5000: i/o timeout", RegistryUnreachable, true},
		{"unauthorized", "FATA[0000] trying to reuse blob: unauthorized: authentication required", RegistryRefused, true},
		{"denied", "FATA[0000] requested access to the resource is denied", RegistryRefused, true},
		{"authentication required", "FATA[0000] trying to reuse blob at destination: checking whether a blob exists in " + destHost + "/x: authentication required", RegistryRefused, true},
		{"403", "FATA[0000] invalid status code from registry 403 (Forbidden)", RegistryRefused, true},
		{"manifest unknown", "FATA[0000] reading manifest 1.0 in " + srcHost + "/x: manifest unknown", SeedFailed, true},
		{"permission denied on a local folder", "FATA[0000] writing manifest: open /srv/out/manifest.json: permission denied", Unclassified, true},
		{"nobody knows this one", "FATA[0000] something nobody classified", Unclassified, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr := tt.stderr + "; password=" + destPass + "\n"
			c, tool := newHelperSkopeo(t, map[string]string{helperStdoutEnv: "stdout-marker-77", helperStderrEnv: stderr, helperExitEnv: "1"})
			src := ImageRef{Transport: "oci", Location: "/var/tmp/layout:img-01"}
			dst := ImageRef{Transport: "docker", Location: destHost + "/x:1"}

			err := c.Copy(context.Background(), src, dst, CopyOptions{DestCreds: &Credentials{User: "admin", Password: destPass}})
			if err == nil {
				t.Fatal("Copy succeeded although skopeo exited 1")
			}
			var be *BenchError
			if !errors.As(err, &be) {
				t.Fatalf("error %T is not a *BenchError: %v", err, err)
			}
			if tt.strict && be.Kind != tt.want {
				t.Errorf("Kind = %d, want %d; message: %v", be.Kind, tt.want, err)
			}
			if be.Cause == "" || be.Check == "" {
				t.Errorf("the BenchError has no cause or no check: %+v", be)
			}
			requireNoSkopeoOutput(t, err)
			tool.last(t)
		})
	}
}

func TestClassifySkopeo(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   ErrKind
	}{
		{"connection refused", "dial tcp 127.0.0.1:41234: connect: connection refused", RegistryUnreachable},
		{"connection refused, upper case", "Dial TCP: Connection Refused", RegistryUnreachable},
		{"connection refused behind pinging", "pinging container registry 127.0.0.1:41234: Get \"https://127.0.0.1:41234/v2/\": dial tcp 127.0.0.1:41234: connect: connection refused", RegistryUnreachable},
		{"no such host", "dial tcp: lookup nexus.invalid on 10.0.0.2:53: no such host", RegistryUnreachable},
		{"i/o timeout", "dial tcp 10.0.0.1:5000: i/o timeout", RegistryUnreachable},
		{"unauthorized", "unauthorized: authentication required", RegistryRefused},
		// What skopeo 1.18.0 really says to a push without credentials against a
		// Basic challenge (seen by TestSkopeoAgainstFakeRegistry): neither
		// "unauthorized" nor "denied" nor "403" appears.
		{"authentication required alone", "trying to reuse blob sha256:1d09 at destination: checking whether a blob sha256:1d09 exists in 127.0.0.1:35839/team/api: authentication required", RegistryRefused},
		{"unauthorized, upper case", "UNAUTHORIZED: bad credentials", RegistryRefused},
		{"denied", "denied: requested access to the resource is denied", RegistryRefused},
		{"denied, registry wording", "requested access to the resource is denied", RegistryRefused},
		{"denied with a prefix only", "denied: access forbidden", RegistryRefused},
		// A local folder skopeo cannot write is not a registry that refused.
		{"permission denied on a dir folder", "FATA[0000] writing manifest: open /srv/out/manifest.json: permission denied", Unclassified},
		{"permission denied on an oci layout", "FATA[0000] initializing destination oci:/srv/layout:img-01: mkdir /srv/layout: permission denied", Unclassified},
		{"permission denied, upper case", "Open /srv/out: Permission Denied", Unclassified},
		{"403", "invalid status code from registry 403 (Forbidden)", RegistryRefused},
		{"manifest unknown", "reading manifest 1.0 in 127.0.0.1:41234/x: manifest unknown", SeedFailed},
		{"manifest unknown, upper case", "MANIFEST UNKNOWN", SeedFailed},
		{"empty", "", Unclassified},
		{"unrelated", "FATA[0000] something else entirely", Unclassified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifySkopeo(tt.stderr); got != tt.want {
				t.Errorf("classifySkopeo(%q) = %d, want %d", tt.stderr, got, tt.want)
			}
		})
	}
}

// The kubectl classifier must keep answering for kubectl texts alone: the new
// kinds of skopeo are not its business.
func TestClassifySkopeoKindsAreNotKubectlKinds(t *testing.T) {
	for _, stderr := range []string{"unauthorized: authentication required", "manifest unknown", "denied: requested access to the resource is denied"} {
		switch got := classifyKubectl(stderr); got {
		case SkopeoMissing, RegistryUnreachable, RegistryRefused, SeedFailed:
			t.Errorf("classifyKubectl(%q) = %d, a kind of skopeo", stderr, got)
		}
	}
}

// ---- credentials never print -------------------------------------------------------

func TestSkopeoCredentialsAreRedacted(t *testing.T) {
	const password = "hunter2-do-not-print"
	c := Credentials{User: "admin", Password: password}
	opts := CopyOptions{SrcCreds: &c, DestCreds: &c, PreserveDigests: true}

	direct := map[string]string{
		"Sprint":      fmt.Sprint(c),
		"%v":          fmt.Sprintf("%v", c),
		"%+v":         fmt.Sprintf("%+v", c),
		"%#v":         fmt.Sprintf("%#v", c),
		"%s":          fmt.Sprintf("%s", c), //nolint:gosimple // the verb is the point
		"%q":          fmt.Sprintf("%q", c),
		"pointer %v":  fmt.Sprintf("%v", &c),
		"pointer %+v": fmt.Sprintf("%+v", &c),
		"pointer %#v": fmt.Sprintf("%#v", &c),
	}
	for verb, got := range direct {
		if strings.Contains(got, password) {
			t.Errorf("%s prints the password: %s", verb, got)
		}
		if !strings.Contains(strings.ToLower(got), "redacted") {
			t.Errorf("%s = %q does not say it is redacted", verb, got)
		}
	}

	nested := map[string]string{
		"options %v":   fmt.Sprintf("%v", opts),
		"options %+v":  fmt.Sprintf("%+v", opts),
		"options %#v":  fmt.Sprintf("%#v", opts),
		"slice %v":     fmt.Sprintf("%v", []Credentials{c}),
		"slice %+v":    fmt.Sprintf("%+v", []Credentials{c}),
		"map %v":       fmt.Sprintf("%v", map[string]Credentials{"a": c}),
		"struct %+v":   fmt.Sprintf("%+v", struct{ C Credentials }{c}),
		"error %v":     fmt.Sprintf("%v", fmt.Errorf("copy with %v failed", c)),
		"error %+v":    fmt.Sprintf("%+v", fmt.Errorf("copy with %+v failed", &c)),
		"pointer opts": fmt.Sprintf("%+v", &opts),
	}
	for verb, got := range nested {
		if strings.Contains(got, password) {
			t.Errorf("%s prints the password: %s", verb, got)
		}
	}

	if _, ok := any(c).(slog.LogValuer); !ok {
		t.Error("Credentials does not implement slog.LogValuer")
	}
	for name, newHandler := range map[string]func(*bytes.Buffer) slog.Handler{
		"text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
		"json": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
	} {
		var buf bytes.Buffer
		slog.New(newHandler(&buf)).Info("copy", "creds", c, "pointer", &c)
		if strings.Contains(buf.String(), password) {
			t.Errorf("the %s log handler records the password: %s", name, buf.String())
		}
	}
}

// ---- the limits of the manifest, the sides of a call ---------------------------------

func TestSkopeoManifestDigestSizeLimit(t *testing.T) {
	ref := ImageRef{Transport: "docker", Location: srcHost + "/mirror/cached:1.0"}
	const limit = 4 << 20

	t.Run("exactly 4 MiB is read", func(t *testing.T) {
		c, tool := newHelperSkopeo(t, map[string]string{helperStdoutBytesEnv: fmt.Sprint(limit)})

		got, err := c.ManifestDigest(context.Background(), ref, nil)
		if err != nil {
			t.Fatalf("ManifestDigest: %v", err)
		}
		if want := sha256Digest(strings.Repeat("x", limit)); got != want {
			t.Errorf("digest = %q, want %q", got, want)
		}
		scratchIsEmpty(t, tool)
	})

	t.Run("one byte more is refused", func(t *testing.T) {
		c, tool := newHelperSkopeo(t, map[string]string{helperStdoutBytesEnv: fmt.Sprint(limit + 1)})

		got, err := c.ManifestDigest(context.Background(), ref, nil)
		if err == nil {
			t.Fatalf("ManifestDigest = %q for a manifest above 4 MiB, want an error", got)
		}
		if got != "" {
			t.Errorf("digest = %q on error, want empty", got)
		}
		var be *BenchError
		if !errors.As(err, &be) {
			t.Fatalf("error %T is not a *BenchError: %v", err, err)
		}
		if be.Kind != SeedFailed {
			t.Errorf("Kind = %d, want SeedFailed (%d); message: %v", be.Kind, SeedFailed, err)
		}
		scratchIsEmpty(t, tool)
	})

	t.Run("a far larger output is refused too", func(t *testing.T) {
		c, _ := newHelperSkopeo(t, map[string]string{helperStdoutBytesEnv: fmt.Sprint(3 * limit)})
		if got, err := c.ManifestDigest(context.Background(), ref, nil); err == nil {
			t.Errorf("ManifestDigest = %q for 12 MiB, want an error", got)
		}
	})

	t.Run("an empty output is no manifest", func(t *testing.T) {
		c, _ := newHelperSkopeo(t, nil)
		got, err := c.ManifestDigest(context.Background(), ref, nil)
		if err == nil {
			t.Fatalf("ManifestDigest = %q for an empty output, want an error (the sha256 of nothing is not a digest of the image)", got)
		}
		var be *BenchError
		if !errors.As(err, &be) || be.Kind != SeedFailed {
			t.Errorf("error = %v, want a SeedFailed BenchError", err)
		}
	})
}

// Credentials belong to a registry; a transport that is not one of the three is
// not run at all. Both are refused before a process starts, for every entry
// point.
func TestSkopeoRefusesCredentialsOnAFileSideAndUnknownTransports(t *testing.T) {
	creds := &Credentials{User: "admin", Password: destPass}
	docker := ImageRef{Transport: "docker", Location: destHost + "/x:1"}
	oci := ImageRef{Transport: "oci", Location: "/var/tmp/layout:img-01"}
	dir := ImageRef{Transport: "dir", Location: "/var/tmp/out"}

	copies := []struct {
		name     string
		src, dst ImageRef
		opts     CopyOptions
	}{
		{"source credentials on an oci source", oci, docker, CopyOptions{SrcCreds: creds}},
		{"source credentials on a dir source", dir, docker, CopyOptions{SrcCreds: creds}},
		{"destination credentials on a dir destination", docker, dir, CopyOptions{DestCreds: creds}},
		{"destination credentials on an oci destination", docker, oci, CopyOptions{DestCreds: creds}},
		{"unknown source transport", ImageRef{Transport: "docker-daemon", Location: "busybox:latest"}, docker, CopyOptions{}},
		{"unknown destination transport", docker, ImageRef{Transport: "containers-storage", Location: "busybox"}, CopyOptions{}},
		{"empty transport", ImageRef{Location: "/var/tmp/x"}, docker, CopyOptions{}},
		{"transport spelled with a capital", ImageRef{Transport: "Docker", Location: destHost + "/x:1"}, dir, CopyOptions{}},
		{"unknown transport with credentials", ImageRef{Transport: "docker-archive", Location: "/var/tmp/x.tar"}, docker, CopyOptions{SrcCreds: creds}},
	}
	for _, tt := range copies {
		t.Run("copy: "+tt.name, func(t *testing.T) {
			c, tool := newHelperSkopeo(t, nil)

			err := c.Copy(context.Background(), tt.src, tt.dst, tt.opts)
			requireRefusedBeforeAnyProcess(t, err, tool)
		})
	}

	inspects := []struct {
		name  string
		ref   ImageRef
		creds *Credentials
	}{
		{"credentials on an oci reference", oci, creds},
		{"credentials on a dir reference", dir, creds},
		{"unknown transport", ImageRef{Transport: "docker-daemon", Location: "busybox:latest"}, nil},
		{"empty transport", ImageRef{Location: "/var/tmp/x"}, nil},
	}
	for _, tt := range inspects {
		t.Run("inspect: "+tt.name, func(t *testing.T) {
			c, tool := newHelperSkopeo(t, nil)

			_, err := c.ManifestDigest(context.Background(), tt.ref, tt.creds)
			requireRefusedBeforeAnyProcess(t, err, tool)
		})
	}
}

func requireRefusedBeforeAnyProcess(t *testing.T, err error, tool helperTool) {
	t.Helper()
	if err == nil {
		t.Fatal("the call was accepted")
	}
	var be *BenchError
	if !errors.As(err, &be) {
		t.Errorf("error %T is not a *BenchError: %v", err, err)
	}
	if strings.Contains(err.Error(), destPass) {
		t.Errorf("the message carries a password: %q", err.Error())
	}
	tool.neverRan(t)
	scratchIsEmpty(t, tool)
}

// ---- the marker of a proven absence ---------------------------------------------------

// What `skopeo inspect --raw` really prints when the image is not there (skopeo
// 1.18.0 against a registry that answers 404, checked by hand): the first three
// are the texts of the distribution error codes, the fourth a bare 404.
const (
	textManifestUnknown = `time="2026-11-17T07:48:36+01:00" level=fatal msg="Error parsing image name \"docker://127.0.0.1:41234/known/x:1.0\": reading manifest 1.0 in 127.0.0.1:41234/known/x: manifest unknown"`
	textNameUnknown     = `time="2026-11-17T07:48:36+01:00" level=fatal msg="Error parsing image name \"docker://127.0.0.1:41234/nobody/home:1.0\": reading manifest 1.0 in 127.0.0.1:41234/nobody/home: name unknown"`
	textNameUnknownCode = "FATA[0000] reading manifest 1.0 in 127.0.0.1:41234/nobody/home: NAME_UNKNOWN: repository name not known to registry"
	textStatus404       = `time="2026-11-17T07:48:36+01:00" level=fatal msg="Error parsing image name \"docker://127.0.0.1:41234/x:1.0\": reading manifest 1.0 in 127.0.0.1:41234/x: StatusCode: 404, \"404 page not found\""`
	textNotFound        = "FATA[0000] reading manifest 1.0 in 127.0.0.1:41234/x: StatusCode: 404, <html><body><h1>Not Found</h1></body></html>"
)

// absentTexts are the outputs that prove an absence, by name.
var absentTexts = map[string]string{
	"manifest unknown":        textManifestUnknown,
	"name unknown":            textNameUnknown,
	"NAME_UNKNOWN":            textNameUnknownCode,
	"a bare 404":              textStatus404,
	"not found":               textNotFound,
	"manifest unknown, upper": "FATA[0000] reading manifest 1.0 in 127.0.0.1:41234/x: MANIFEST UNKNOWN",
}

// noAbsenceTexts are outputs that prove nothing about the image.
var noAbsenceTexts = map[string]string{
	"connection refused":             "FATA[0000] pinging container registry 127.0.0.1:41234: dial tcp 127.0.0.1:41234: connect: connection refused",
	"no such host":                   "FATA[0000] dial tcp: lookup nexus.invalid: no such host",
	"timeout":                        "FATA[0000] dial tcp 10.0.0.1:5000: i/o timeout",
	"unauthorized":                   "FATA[0000] reading manifest 1.0 in 127.0.0.1:41234/x: unauthorized: authentication required",
	"denied":                         "FATA[0000] reading manifest 1.0 in 127.0.0.1:41234/x: denied: requested access to the resource is denied",
	"a refusal that mentions a 404":  "FATA[0000] reading manifest 1.0: unauthorized: authentication required (probe /v2/ answered 404 not found)",
	"permission denied on a folder":  "FATA[0000] writing manifest: open /srv/out/manifest.json: permission denied",
	"a 404 that is not a status":     "FATA[0000] invalid reference at line 40411",
	"a path that is not found":       "FATA[0000] open /etc/containers/policy.json: no such file or directory",
	"nobody classified this one":     "FATA[0000] something nobody classified",
	"empty":                          "",
	"an internal error of the proxy": "FATA[0000] reading manifest 1.0 in 127.0.0.1:41234/x: StatusCode: 500, internal error",
}

func TestIsImageAbsent(t *testing.T) {
	for name, text := range absentTexts {
		if !isImageAbsent(text) {
			t.Errorf("%s: isImageAbsent(%q) = false, want true (the output proves the absence)", name, text)
		}
	}
	for name, text := range noAbsenceTexts {
		if isImageAbsent(text) {
			t.Errorf("%s: isImageAbsent(%q) = true, want false (the output proves nothing)", name, text)
		}
	}
}

// inspectFailure is the error ManifestDigest builds from the output of a failed
// inspect: the kind of classifySkopeo, the marker on a proven absence only, and
// nothing of the output in the message.
func TestInspectFailure(t *testing.T) {
	for name, text := range absentTexts {
		err := inspectFailure(text+"; password="+destPass, errFakeExit1)
		if !errors.Is(err, errImageAbsent) {
			t.Errorf("%s: the error does not carry errImageAbsent", name)
		}
		requireNoSkopeoOutput(t, err)
	}
	for name, text := range noAbsenceTexts {
		err := inspectFailure(text+"; password="+destPass, errFakeExit1)
		if errors.Is(err, errImageAbsent) {
			t.Errorf("%s: the error carries errImageAbsent for an output that proves nothing", name)
		}
		if !errors.Is(err, errFakeExit1) {
			t.Errorf("%s: the error does not keep the cause of the process", name)
		}
		requireNoSkopeoOutput(t, err)
	}
}

// The real copier, with a stand-in skopeo: a failed inspect carries the marker
// exactly when the output proves the absence.
func TestSkopeoManifestDigestEmitsTheAbsenceMarker(t *testing.T) {
	ref := ImageRef{Transport: "docker", Location: srcHost + "/x:1.0"}
	run := func(t *testing.T, env map[string]string) error {
		t.Helper()
		c, _ := newHelperSkopeo(t, env)
		got, err := c.ManifestDigest(context.Background(), ref, nil)
		if err == nil {
			t.Fatalf("ManifestDigest = %q, want an error", got)
		}
		if got != "" {
			t.Errorf("digest = %q on error, want empty", got)
		}
		var be *BenchError
		if !errors.As(err, &be) {
			t.Fatalf("error %T is not a *BenchError: %v", err, err)
		}
		return err
	}

	for name, text := range absentTexts {
		t.Run("absent: "+name, func(t *testing.T) {
			err := run(t, map[string]string{helperStdoutEnv: "stdout-marker-77", helperStderrEnv: text + "; password=" + srcPass + "\n", helperExitEnv: "1"})
			if !errors.Is(err, errImageAbsent) {
				t.Errorf("the error does not carry errImageAbsent: %v", err)
			}
			requireNoSkopeoOutput(t, err)
		})
	}
	for name, text := range noAbsenceTexts {
		if text == "" {
			continue // an exit 1 with no output is covered below
		}
		t.Run("not absent: "+name, func(t *testing.T) {
			err := run(t, map[string]string{helperStdoutEnv: "stdout-marker-77", helperStderrEnv: text + "; password=" + srcPass + "\n", helperExitEnv: "1"})
			if errors.Is(err, errImageAbsent) {
				t.Errorf("the error carries errImageAbsent for an output that proves nothing: %v", err)
			}
			requireNoSkopeoOutput(t, err)
		})
	}

	t.Run("not absent: exit 1 and no output at all", func(t *testing.T) {
		if err := run(t, map[string]string{helperExitEnv: "1"}); errors.Is(err, errImageAbsent) {
			t.Errorf("the error carries errImageAbsent: %v", err)
		}
	})
	t.Run("not absent: exit 0 and no manifest printed", func(t *testing.T) {
		err := run(t, nil)
		if errors.Is(err, errImageAbsent) {
			t.Errorf("an empty output was taken for an absence: %v", err)
		}
		var be *BenchError
		if errors.As(err, &be) && be.Kind != SeedFailed {
			t.Errorf("Kind = %d, want SeedFailed (%d)", be.Kind, SeedFailed)
		}
	})
	t.Run("not absent: a manifest above 4 MiB", func(t *testing.T) {
		err := run(t, map[string]string{helperStdoutBytesEnv: fmt.Sprint(4<<20 + 1)})
		if errors.Is(err, errImageAbsent) {
			t.Errorf("an oversized manifest was taken for an absence: %v", err)
		}
	})
	t.Run("not absent: an interrupted call", func(t *testing.T) {
		c, _ := newHelperSkopeo(t, map[string]string{helperStderrEnv: textManifestUnknown, helperExitEnv: "1"})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := c.ManifestDigest(ctx, ref, nil)
		if err == nil || errors.Is(err, errImageAbsent) {
			t.Errorf("error = %v, want a failure that is not an absence", err)
		}
	})
}
