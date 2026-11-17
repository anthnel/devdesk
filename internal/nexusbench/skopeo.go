package nexusbench

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ImageRef names an image for skopeo: Transport is "oci", "dir" or "docker".
// For docker, Location is "<host[:port]>/<name>[:tag]" without the "//".
type ImageRef struct{ Transport, Location string }

// CopyOptions are the options of one copy.
type CopyOptions struct {
	SrcCreds, DestCreds *Credentials // nil = --*-no-creds
	PreserveDigests     bool
}

// Credentials is a user and a password for one registry. It prints as
// "[REDACTED]" under every fmt verb and in every slog handler, so neither a log
// line nor an error built with %v can carry the password.
type Credentials struct {
	User     string
	Password string `json:"-"`
}

const redactedCredentials = "Credentials{User: <redacted>, Password: [REDACTED]}"

// String implements fmt.Stringer.
func (Credentials) String() string { return redactedCredentials }

// GoString implements fmt.GoStringer.
func (Credentials) GoString() string { return redactedCredentials }

// Format implements fmt.Formatter, so no verb (not even %d or %x, which a
// Stringer does not cover) can print the fields.
func (Credentials) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redactedCredentials)) }

// LogValue implements slog.LogValuer.
func (Credentials) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// ImageCopier copies images between registries and layouts.
type ImageCopier interface {
	Copy(ctx context.Context, src, dst ImageRef, o CopyOptions) error
	ManifestDigest(ctx context.Context, ref ImageRef, creds *Credentials) (string, error)
}

const (
	// skopeoStderrMax is how much of the stderr of skopeo is kept for the
	// classifier; the text never reaches a message.
	skopeoStderrMax = 64 << 10
	// manifestMax is the largest manifest ManifestDigest reads (the limit
	// registries apply themselves is 4 MiB).
	manifestMax = 4 << 20
	// skopeoKillDelay bounds the wait for skopeo's output pipes once it is
	// killed, in case a child it started still holds them.
	skopeoKillDelay = 2 * time.Second

	// policyAcceptAnything is the policy file of every call: skopeo refuses to
	// run without a readable policy on a host that has none, and the bench has
	// no signature to check. The default rule is the whole policy.
	policyAcceptAnything = `{"default":[{"type":"insecureAcceptAnything"}]}`
)

// skopeoCopier runs the skopeo found on PATH. It is, with kubectl.go and
// secrets.go, one of the three files of the package that start a process.
type skopeoCopier struct{ path string }

// NewSkopeoCopier finds skopeo on PATH. It fails with a SkopeoMissing BenchError
// when skopeo is not installed.
func NewSkopeoCopier() (ImageCopier, error) {
	path, err := exec.LookPath("skopeo")
	if err != nil {
		return nil, &BenchError{
			Kind:  SkopeoMissing,
			Cause: "skopeo was not found on PATH",
			Check: "install skopeo with your package manager, then run `skopeo --version`",
			Err:   err,
		}
	}
	return &skopeoCopier{path: path}, nil
}

// Copy implements ImageCopier: `skopeo copy` of src to dst. A docker reference
// whose host is not the loopback is refused before any process starts.
func (c *skopeoCopier) Copy(ctx context.Context, src, dst ImageRef, o CopyOptions) error {
	if err := checkSide(src, o.SrcCreds); err != nil {
		return err
	}
	if err := checkSide(dst, o.DestCreds); err != nil {
		return err
	}
	w, err := newSkopeoWorkdir()
	if err != nil {
		return err
	}
	defer w.remove()

	args := []string{"--policy", w.policy, "copy"}
	if o.PreserveDigests {
		args = append(args, "--preserve-digests")
	}
	for _, side := range []struct {
		flagPrefix string
		ref        ImageRef
		creds      *Credentials
	}{{"--src-", src, o.SrcCreds}, {"--dest-", dst, o.DestCreds}} {
		flags, err := w.sideFlags(side.flagPrefix, side.ref, side.creds)
		if err != nil {
			return err
		}
		args = append(args, flags...)
	}
	args = append(args, imageArg(src), imageArg(dst))

	if _, err := c.run(ctx, w, "copy", args, 0, nil); err != nil {
		return err
	}
	return nil
}

// ManifestDigest implements ImageCopier: the sha256 of the raw manifest of ref,
// computed here from `skopeo inspect --raw`, not read from anything skopeo says
// about it.
func (c *skopeoCopier) ManifestDigest(ctx context.Context, r ImageRef, creds *Credentials) (string, error) {
	if err := checkSide(r, creds); err != nil {
		return "", err
	}
	w, err := newSkopeoWorkdir()
	if err != nil {
		return "", err
	}
	defer w.remove()

	flags, err := w.sideFlags("--", r, creds)
	if err != nil {
		return "", err
	}
	args := append([]string{"--policy", w.policy, "inspect", "--raw"}, flags...)
	args = append(args, imageArg(r))

	manifest, err := c.run(ctx, w, "inspect", args, manifestMax, inspectFailure)
	if err != nil {
		return "", err
	}
	if len(manifest) == 0 {
		return "", &BenchError{
			Kind:  SeedFailed,
			Cause: "skopeo printed no manifest",
			Check: "run `skopeo inspect --raw` on the same reference by hand",
		}
	}
	sum := sha256.Sum256(manifest)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// run starts skopeo with args in the environment of w and returns its stdout
// (at most stdoutMax bytes; with 0 the stdout is dropped). Nothing skopeo prints
// reaches the error: the stderr only picks its kind. fail builds the error of a
// skopeo that exited with a failure from what it printed; nil is the classified
// error of skopeoFailure.
func (c *skopeoCopier) run(ctx context.Context, w *skopeoWorkdir, op string, args []string, stdoutMax int, fail func(stderr string, runErr error) *BenchError) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.path, args...) //nolint:gosec // the binary is the skopeo found on PATH and the references were checked by checkSide
	cmd.Env = skopeoEnvironment(w.registriesConf)
	cmd.WaitDelay = skopeoKillDelay
	stdout, stderr := newCapWriter(stdoutMax), newCapWriter(skopeoStderrMax)
	cmd.Stdout, cmd.Stderr = stdout, stderr

	runErr := cmd.Run()
	if runErr == nil {
		if stdoutMax > 0 && stdout.over {
			return nil, &BenchError{
				Kind:  SeedFailed,
				Cause: "the manifest is larger than the 4 MiB registries accept",
				Check: "run `skopeo inspect --raw` on the same reference by hand",
			}
		}
		return stdout.buf.Bytes(), nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		kind := Unclassified
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			kind = Deadline
		}
		return nil, &BenchError{
			Kind:  kind,
			Cause: fmt.Sprintf("skopeo %s was interrupted", op),
			Check: "run the command again",
			Err:   ctxErr,
		}
	}
	if fail != nil {
		return nil, fail(stderr.buf.String(), runErr)
	}
	return nil, skopeoFailure(op, classifySkopeo(stderr.buf.String()), runErr)
}

// errImageAbsent is the marker ManifestDigest puts in the chain of its error (with
// errors.Is) when the output of skopeo PROVES the image is not at the reference:
// "manifest unknown", "name unknown" (NAME_UNKNOWN, an empty repository) or a
// "not found" / 404 on the inspect. It is never set for an output that proves
// nothing: no manifest printed, a manifest above 4 MiB, an unreachable or refusing
// registry, a text nobody classified.
var errImageAbsent = errors.New("the image is not in the registry")

// status404 matches a 404 that is a whole number, so a port or a line number that
// contains it is not read as a status.
var status404 = regexp.MustCompile(`\b404\b`)

// isImageAbsent tells whether the stderr of a failed `skopeo inspect` proves the
// image is absent: the registry said "manifest unknown", "name unknown",
// "NAME_UNKNOWN", "not found" or answered 404, and the output is not one that
// classifySkopeo reads as an unreachable or refusing registry (a refusal that
// quotes a 404 proves nothing). Case does not matter. H-COMPONENTS: the wording
// of a registry that has no such repository is confirmed at step 12.
func isImageAbsent(stderr string) bool {
	switch classifySkopeo(stderr) {
	case RegistryUnreachable, RegistryRefused:
		return false
	}
	s := strings.ToLower(stderr)
	for _, marker := range []string{"manifest unknown", "name unknown", "name_unknown", "not found"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return status404.MatchString(s)
}

// inspectFailure is the error ManifestDigest returns for a skopeo inspect that
// exited with runErr and printed stderr: a *BenchError that never prints stderr
// and whose chain holds errImageAbsent (and runErr) when isImageAbsent says so.
func inspectFailure(stderr string, runErr error) *BenchError {
	if !isImageAbsent(stderr) {
		return skopeoFailure("inspect", classifySkopeo(stderr), runErr)
	}
	return skopeoFailure("inspect", SeedFailed, fmt.Errorf("%w: %w", errImageAbsent, runErr))
}

// skopeoFailure builds the error of a skopeo that exited with a failure.
func skopeoFailure(op string, kind ErrKind, runErr error) *BenchError {
	be := &BenchError{Kind: kind, Err: runErr}
	switch kind {
	case RegistryUnreachable:
		be.Cause = fmt.Sprintf("skopeo %s could not reach a registry", op)
		be.Check = "check that the port-forward is running and the registry answers on its port"
	case RegistryRefused:
		be.Cause = fmt.Sprintf("a registry refused the skopeo %s", op)
		be.Check = "check the bench credentials in the secrets file and the privileges of the account"
	case SeedFailed:
		be.Cause = fmt.Sprintf("skopeo %s did not find the image", op)
		be.Check = "check that the image and its tag exist in the source registry"
	default:
		be.Cause = fmt.Sprintf("skopeo %s failed", op)
		be.Check = "run the same skopeo command by hand to see what it says"
	}
	return be
}

// imageArg is how skopeo is given an ImageRef: docker://<location>, or
// <transport>:<location>.
func imageArg(r ImageRef) string {
	if r.Transport == "docker" {
		return "docker://" + r.Location
	}
	return r.Transport + ":" + r.Location
}

// checkSide refuses, before any process starts, a reference skopeo must not be
// given: an unknown transport, credentials for a side that is not a registry,
// and a docker reference whose host is not the loopback (TLS verification is
// switched off for every docker reference, so the host is the only protection).
func checkSide(r ImageRef, creds *Credentials) error {
	switch r.Transport {
	case "oci", "dir":
		if creds != nil {
			return &BenchError{
				Kind:  Unclassified,
				Cause: "credentials were given for an image that is not in a registry",
				Check: "give credentials only for a docker reference",
			}
		}
		return nil
	case "docker":
		host, name, hasName := strings.Cut(r.Location, "/")
		if !hasName || name == "" || !loopbackRegistry(host) {
			return &BenchError{
				Kind:  Unclassified,
				Cause: "the bench only talks to a registry on the loopback, with a repository name: TLS verification is off for it",
				Check: "reach the registry through the port-forward and give 127.0.0.1:<port>/<name>[:<tag>]",
			}
		}
		return nil
	}
	return &BenchError{
		Kind:  Unclassified,
		Cause: "the image transport is not one of oci, dir and docker",
		Check: "give a reference whose transport is oci, dir or docker",
	}
}

// registryHost is the "<host[:port]>" part of a docker location. checkSide has
// already required a "/" and a repository name after it: without them skopeo
// reads "localhost:5000" as an image of docker.io.
func registryHost(location string) string {
	host, _, _ := strings.Cut(location, "/")
	return host
}

// loopbackRegistry tells whether hostPort ("127.0.0.1:5000", "[::1]:5000") is
// one of the two loopback literals and nothing else ("localhost" is not: it
// resolves through the host's configuration). It parses the host and the
// port instead of testing a prefix, so "127.0.0.1@evil.com",
// "127.0.0.1.evil.com" and "127.0.0.1:5000@evil.com" are not the loopback.
func loopbackRegistry(hostPort string) bool {
	host := hostPort
	if h, p, err := net.SplitHostPort(hostPort); err == nil {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != p {
			return false
		}
		host = h
		if strings.HasPrefix(hostPort, "[") {
			return host == "::1"
		}
	} else if strings.HasPrefix(hostPort, "[") && strings.HasSuffix(hostPort, "]") {
		return hostPort == "[::1]"
	}
	return host == "127.0.0.1"
}

// skopeoWorkdir is the temporary folder of one skopeo call: the policy, the
// empty registries.conf and the authentication files. The folder is 0700, the
// files are 0600, and remove deletes all of it whatever happened.
type skopeoWorkdir struct {
	dir            string
	policy         string
	registriesConf string
}

// newSkopeoWorkdir creates the folder with the policy and the empty
// registries.conf already in it.
func newSkopeoWorkdir() (*skopeoWorkdir, error) {
	dir, err := os.MkdirTemp("", "devdesk-skopeo-") // 0700
	if err != nil {
		return nil, &BenchError{
			Kind:  Unclassified,
			Cause: "the temporary folder for skopeo could not be created",
			Check: "check that the temporary folder is writable and not full",
			Err:   err,
		}
	}
	w := &skopeoWorkdir{dir: dir, policy: filepath.Join(dir, "policy.json"), registriesConf: filepath.Join(dir, "registries.conf")}
	for path, content := range map[string]string{w.policy: policyAcceptAnything, w.registriesConf: ""} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			w.remove()
			return nil, workdirError(err)
		}
	}
	return w, nil
}

// remove deletes the folder and everything in it. It is called by defer, so it
// runs after a failure and a cancellation as well.
func (w *skopeoWorkdir) remove() { _ = os.RemoveAll(w.dir) }

func workdirError(err error) *BenchError {
	return &BenchError{
		Kind:  Unclassified,
		Cause: "a temporary file for skopeo could not be written",
		Check: "check that the temporary folder is writable and not full",
		Err:   err,
	}
}

// sideFlags returns the flags of one side of a call, for a flag prefix of
// "--src-", "--dest-" or "--": TLS verification off for a docker reference only,
// and either "<prefix>no-creds" or "<prefix>authfile <file>" holding the
// credentials. The credentials go to a file because an argument is visible to
// every account through /proc.
func (w *skopeoWorkdir) sideFlags(prefix string, r ImageRef, creds *Credentials) ([]string, error) {
	var flags []string
	if r.Transport == "docker" {
		flags = append(flags, prefix+"tls-verify=false")
	}
	if creds == nil {
		return append(flags, prefix+"no-creds"), nil
	}
	// One file per side, so the credentials of a side are never offered to the other.
	name := "auth.json"
	if prefix != "--" {
		name = strings.Trim(prefix, "-") + "-auth.json"
	}
	path := filepath.Join(w.dir, name)
	content, err := json.Marshal(map[string]any{"auths": map[string]any{
		registryHost(r.Location): map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte(creds.User + ":" + creds.Password))},
	}})
	if err != nil {
		return nil, workdirError(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return nil, workdirError(err)
	}
	return append(flags, prefix+"authfile", path), nil
}

// skopeoEnvironment is the environment of the skopeo process: the caller's,
// without the proxy variables (which would send the bench traffic elsewhere),
// REGISTRY_AUTH_FILE (which would name a login of the host) and every
// CONTAINERS_* variable (which would change the configuration, the policy or the
// certificates), and with CONTAINERS_REGISTRIES_CONF pointing at registriesConf,
// an empty file, so no mirror or redirect of the host applies. Names are compared
// in upper case because Windows environment names are case-insensitive.
func skopeoEnvironment(registriesConf string) []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		upper := strings.ToUpper(name)
		switch upper {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "REGISTRY_AUTH_FILE":
			continue
		}
		if strings.HasPrefix(upper, "CONTAINERS_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "CONTAINERS_REGISTRIES_CONF="+registriesConf)
}
