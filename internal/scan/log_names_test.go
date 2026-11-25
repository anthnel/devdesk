package scan

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/anthnel/devdesk/internal/k8s"
	"github.com/anthnel/devdesk/internal/trust"
)

// DEVDESK-775: the log calls of this package that DEVDESK-768 left with a raw
// name. A folder or file name comes from the repository being scanned, which is
// not trusted: a newline in it splits one log entry in two (the second half can
// pass for an entry of its own), U+2028 and U+202E fake or reorder it, and an
// invalid UTF-8 byte corrupts the file the log is read from.
//
// Each site below is one log call, reached for real: a temporary repository
// whose folder or file name is hostile, the production code run against it, and
// the log read back. Two tests walk the table:
//
//   - the hostile test: the entry is one line, holds nothing a terminal or a
//     log reader would take for something else, and every name the call prints
//     on its own is there quoted the way logWord does it (%q);
//   - the ordinary test: for a name that prints (accents and spaces included)
//     the line is byte for byte the line the call wrote before, because
//     logWord quotes a word only when it is not valid UTF-8 or holds a rune
//     that does not print. A readable name gets no quote marks.
//
// What an error carries is another matter: a *fs.PathError printed with %v
// holds the path as the system was given it. The helper that protects it is
// named logErr (func logErr(err error) string, declared by TestLogErrIsDeclared
// in log_guard_test.go): the same text for an error that prints, the quoted
// text otherwise, and "<nil>" for a nil error, which is what %v printed and
// what the "core.worktree is set" entry of resolveRenderScope still prints.

// hostileNames are the four ways a name breaks a log line. Each is valid for
// the file systems the tests run on; a file system that refuses one skips the
// case (see mustWrite and mustMkdir).
var hostileNames = []struct{ name, value string }{
	{"newline", "a\nb"},
	{"line separator U+2028", "a\u2028b"},
	{"bidi override U+202E", "a\u202eb"},
	{"invalid UTF-8", "a\xffb"},
}

// ordinaryNames are names that print. withSlash is true for a name that is a
// path (a folder below a folder), false for one that is a single file name.
func ordinaryNames(withSlash bool) []struct{ name, value string } {
	if withSlash {
		return []struct{ name, value string }{
			{"plain", "charts/app"},
			{"accents and a space", "chârts/àpp ünïcode"},
		}
	}
	return []struct{ name, value string }{
		{"plain", "app"},
		{"accents and a space", "àpp ünï"},
	}
}

// entryPrefixes are what every line of this package's log starts with. A line
// that starts with none of them is the tail of an entry a name split.
var entryPrefixes = []string{"Running: ", "[scan", "ERROR ", "WARN "}

// loggedSite is what one trigger leaves behind.
type loggedSite struct {
	buf *bytes.Buffer
	// prefix selects the entry among the lines the trigger logged.
	prefix string
	// words are the values the call prints as a word of its own: a hostile one
	// must show in the entry as strconv.Quote(value), exactly as logWord writes it.
	words []string
	// embedded are values inside the text of an error: the entry must hold
	// their escaped form (the quoted text without its outer quotes), whether
	// logErr quotes the whole error or only the path.
	embedded []string
	// old is the entry as the call wrote it before DEVDESK-775, built with the
	// raw values and the original format, without the final newline.
	old string
}

// logSite is one log call and the way to reach it.
type logSite struct {
	name string
	// file names the production file and call, for the failure message.
	file string
	// perFileName is true when the hostile part ends up in a single file name,
	// so the ordinary names must not hold a slash.
	perFileName bool
	// trigger runs the production code against a repository whose hostile (or
	// ordinary) part is value, and returns what it logged.
	trigger func(t *testing.T, value string) loggedSite
}

// mustMkdir and mustWrite skip the test when the file system refuses the name
// (macOS refuses invalid UTF-8, Windows refuses a newline): the case is not
// about the file system.
func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skipf("the file system refuses the name %q: %v", dir, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Skipf("the file system refuses the name %q: %v", path, err)
	}
}

// unreadable makes path a file that exists and cannot be opened. It skips the
// test where chmod does not do that (Windows, or a test run as root): the read
// error sites of buildcontext.go and signature.go cannot be reached otherwise
// without a seam in production code.
func unreadable(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0 does not make a file unreadable on Windows")
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if f, err := os.Open(path); err == nil {
		_ = f.Close()
		t.Skip("the file is readable despite chmod 0 (running as root?)")
	}
}

// tempBase is a temporary directory with its symlinks resolved, so the paths a
// test builds are the ones the code under test resolves to.
func tempBase(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return base
}

const wholeContextDockerfile = "FROM scratch\nCOPY . .\n"

// logSites is every log call of DEVDESK-775 whose entry can be reached from a
// test. Not here: nothing. The 14 calls of the story are all reached; the
// extra variants are for the error text (a nil error, a wrapped one).
func logSites() []logSite {
	return []logSite{
		{
			name: "no git repository", file: "kubeconform_render.go resolveRenderScope: no git repository to mount",
			trigger: func(t *testing.T, value string) loggedSite {
				buf := captureLog(t)
				target := filepath.Join(tempBase(t), value)
				gitErr := &fs.PathError{Op: "chdir", Path: target, Err: fs.ErrNotExist}
				useRunner(t, &scriptedRunner{reply: func(toolCmd) ([]byte, error) { return nil, gitErr }})
				_ = resolveRenderScope(t.Context(), target)
				return loggedSite{
					buf: buf, prefix: "[scan/k8s] ", words: []string{target}, embedded: []string{target},
					old: fmt.Sprintf("[scan/k8s] %s: no git repository to mount, the renderers see the target only: %v", target, gitErr),
				}
			},
		},
		{
			name: "core.worktree is set", file: "kubeconform_render.go resolveRenderScope: core.worktree (nil error)",
			trigger: func(t *testing.T, value string) loggedSite {
				buf := captureLog(t)
				target := filepath.Join(tempBase(t), value)
				useRunner(t, &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
					if strings.Join(tc.Args, " ") == "rev-parse --show-toplevel" {
						return []byte("/somewhere\n"), nil
					}
					return []byte("/etc\n"), nil
				}})
				_ = resolveRenderScope(t.Context(), target)
				return loggedSite{
					buf: buf, prefix: "[scan/k8s] ", words: []string{target},
					old: fmt.Sprintf("[scan/k8s] %s: core.worktree is set or unreadable (%q, %v), the renderers see the target only",
						target, "/etc", error(nil)),
				}
			},
		},
		{
			name: "core.worktree is unreadable", file: "kubeconform_render.go resolveRenderScope: core.worktree (error with the path)",
			trigger: func(t *testing.T, value string) loggedSite {
				buf := captureLog(t)
				target := filepath.Join(tempBase(t), value)
				cfgErr := &fs.PathError{Op: "open", Path: filepath.Join(target, ".git", "config"), Err: fs.ErrPermission}
				useRunner(t, &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
					if strings.Join(tc.Args, " ") == "rev-parse --show-toplevel" {
						return []byte("/somewhere\n"), nil
					}
					return nil, cfgErr
				}})
				_ = resolveRenderScope(t.Context(), target)
				return loggedSite{
					buf: buf, prefix: "[scan/k8s] ", words: []string{target}, embedded: []string{cfgErr.Path},
					old: fmt.Sprintf("[scan/k8s] %s: core.worktree is set or unreadable (%q, %v), the renderers see the target only",
						target, "", cfgErr),
				}
			},
		},
		{
			name: "container mount", file: "kubeconform_render.go resolveRenderScope: renderers in a container mount",
			trigger: func(t *testing.T, value string) loggedSite {
				buf := captureLog(t)
				base := tempBase(t)
				repo := filepath.Join(base, "src", value)
				target := filepath.Join(repo, "deploy")
				mustMkdir(t, filepath.Join(repo, ".git"))
				mustMkdir(t, target)
				useHome(t, filepath.Join(base, "home"), nil)
				useRunner(t, &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
					if strings.Join(tc.Args, " ") == "rev-parse --show-toplevel" {
						return []byte(repo + "\n"), nil
					}
					return nil, &exitError{Code: 1}
				}})
				scope := resolveRenderScope(t.Context(), target)
				if scope.mount != repo {
					t.Fatalf("the mount is %q, want the repository %q: the setup does not reach the call", scope.mount, repo)
				}
				return loggedSite{
					buf: buf, prefix: "[scan/k8s] renderers in a container", words: []string{repo, target},
					old: fmt.Sprintf("[scan/k8s] renderers in a container mount %s, the repository holding %s", repo, target),
				}
			},
		},
		{
			name: "custom resources not validated", file: "kubeconform.go runKubeconformStage: custom resources not validated",
			trigger: func(t *testing.T, value string) loggedSite {
				isolateHome(t)
				buf := captureLog(t)
				target := filepath.Join(tempBase(t), value)
				mustWrite(t, filepath.Join(target, "cm.yaml"), "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n")
				useRunner(t, &scriptedRunner{reply: func(toolCmd) ([]byte, error) { return kubeconformFixture(t), nil }})
				var result Result
				var mu sync.Mutex
				newScannerWithDeps(k8sSchemaFor("1.36.0"), binaries(ToolKubeconform)).
					runKubeconformStage(t.Context(), target, &result, &mu, func(ProgressUpdate) {})
				// The fixture holds two custom resources (kubeconform_test.go).
				return loggedSite{
					buf: buf, prefix: "[scan/kubeconform] ", words: []string{target},
					old: fmt.Sprintf("[scan/kubeconform] %s: %d custom resources not validated (their schema is in a CRD)", target, 2),
				}
			},
		},
		{
			name: "not rendered", file: "kubeconform.go runKubeconformStage: not rendered, so not validated",
			trigger: func(t *testing.T, value string) loggedSite {
				isolateHome(t)
				buf := captureLog(t)
				target := filepath.Join(tempBase(t), value)
				// The chart is the folder named value, below the target; helm is not
				// in the report, so the chart is listed as not rendered.
				mustWrite(t, filepath.Join(target, filepath.FromSlash(value), "Chart.yaml"),
					"apiVersion: v2\nname: x\nversion: 0.1.0\n")
				useRunner(t, &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
					return nil, fmt.Errorf("unexpected invocation %s", tc)
				}})
				var result Result
				var mu sync.Mutex
				newScannerWithDeps(k8sSchemaFor("1.36.0"), binaries(ToolKubeconform)).
					runKubeconformStage(t.Context(), target, &result, &mu, func(ProgressUpdate) {})
				if len(result.K8sUnrendered) != 1 {
					t.Fatalf("K8sUnrendered = %q, want the one chart: the setup does not reach the call (errors: %v)",
						result.K8sUnrendered, result.Errors)
				}
				chart := result.K8sUnrendered[0]
				return loggedSite{
					buf: buf, prefix: "[scan/kubeconform] ", words: []string{target, chart},
					old: fmt.Sprintf("[scan/kubeconform] %s: not rendered, so not validated: %s", target, chart),
				}
			},
		},
		{
			name: "build context: Dockerfile unreadable", file: "buildcontext.go CheckBuildContext: read of a Dockerfile", perFileName: true,
			trigger: func(t *testing.T, value string) loggedSite {
				buf := captureLog(t)
				root := tempBase(t)
				name := "Dockerfile." + value
				path := filepath.Join(root, name)
				mustWrite(t, path, wholeContextDockerfile)
				unreadable(t, path)
				_, readErr := os.ReadFile(path)
				_, _ = CheckBuildContext(root)
				return loggedSite{
					buf: buf, prefix: "ERROR [scan/build-context] ", words: []string{name}, embedded: []string{path},
					old: fmt.Sprintf("ERROR [scan/build-context] read %s: %v", name, readErr),
				}
			},
		},
		{
			name: "build context: ignore file unreadable", file: "buildcontext.go contextFilter: read of an ignore file", perFileName: true,
			trigger: func(t *testing.T, value string) loggedSite {
				buf := captureLog(t)
				root := tempBase(t)
				name := "Dockerfile." + value
				rel := name + ".dockerignore"
				mustWrite(t, filepath.Join(root, name), wholeContextDockerfile)
				mustWrite(t, filepath.Join(root, rel), "*.log\n")
				unreadable(t, filepath.Join(root, rel))
				_, readErr := os.ReadFile(filepath.Join(root, rel))
				_, _ = CheckBuildContext(root)
				return loggedSite{
					buf: buf, prefix: "ERROR [scan/build-context] ", words: []string{rel}, embedded: []string{filepath.Join(root, rel)},
					old: fmt.Sprintf("ERROR [scan/build-context] read %s: %v", rel, readErr),
				}
			},
		},
		{
			name: "build context: Dockerfile outside the root", file: "buildcontext.go logUncheckedDockerfiles",
			trigger: func(t *testing.T, value string) loggedSite {
				buf := captureLog(t)
				root := filepath.Join(tempBase(t), value)
				rel := value + "/Dockerfile"
				mustWrite(t, filepath.Join(root, filepath.FromSlash(rel)), wholeContextDockerfile)
				_, _ = CheckBuildContext(root)
				return loggedSite{
					buf: buf, prefix: "[scan/build-context] ", words: []string{root, rel},
					old: fmt.Sprintf("[scan/build-context] %s: %s not checked — a Dockerfile outside the root does not say where its build context is", root, rel),
				}
			},
		},
		{
			name: "seccomp origin: root cannot be opened", file: "seccomp_origin.go annotateSeccompContext: cannot open",
			trigger: func(t *testing.T, value string) loggedSite {
				buf := captureLog(t)
				root := filepath.Join(tempBase(t), value) // never created
				_, openErr := os.OpenRoot(root)
				annotateSeccompContext(t.Context(), root, []Finding{seccompFinding("deploy.yaml")})
				return loggedSite{
					buf: buf, prefix: "WARN [scan] seccomp origin: cannot open ", words: []string{root}, embedded: []string{root},
					old: fmt.Sprintf("WARN [scan] seccomp origin: cannot open %s: %v", root, openErr),
				}
			},
		},
		{
			name: "seccomp origin: file outside the directory", file: "seccomp_origin.go annotateSeccompContext: outside the scanned directory",
			trigger: func(t *testing.T, value string) loggedSite {
				buf := captureLog(t)
				file := "../" + value
				annotateSeccompContext(t.Context(), tempBase(t), []Finding{seccompFinding(file)})
				return loggedSite{
					buf: buf, prefix: "WARN [scan] seccomp origin: ", words: []string{file},
					old: fmt.Sprintf("WARN [scan] seccomp origin: %s is outside the scanned directory", file),
				}
			},
		},
		{
			name: "seccomp origin: file cannot be read", file: "seccomp_origin.go annotateSeccompContext: cannot read",
			trigger: func(t *testing.T, value string) loggedSite {
				buf := captureLog(t)
				root := tempBase(t)
				file := value // never created, below the root
				dir, err := os.OpenRoot(root)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = dir.Close() }()
				_, readErr := k8s.ReadManifestIn(dir, filepath.FromSlash(file))
				annotateSeccompContext(t.Context(), root, []Finding{seccompFinding(file)})
				return loggedSite{
					buf: buf, prefix: "WARN [scan] seccomp origin: cannot read ", words: []string{file}, embedded: []string{file},
					old: fmt.Sprintf("WARN [scan] seccomp origin: cannot read %s: %v", file, readErr),
				}
			},
		},
		{
			name: "signature: Dockerfile unreadable", file: "signature.go checkBaseImageSignatures: read of a Dockerfile",
			trigger: func(t *testing.T, value string) loggedSite {
				buf := captureLog(t)
				target := tempBase(t)
				rel := value + "/Dockerfile"
				path := filepath.Join(target, filepath.FromSlash(rel))
				mustWrite(t, path, "FROM alpine:3.20\n")
				unreadable(t, path)
				_, readErr := os.ReadFile(path)
				_, _ = checkBaseImageSignatures(t.Context(), target, trust.CheckDeps{})
				return loggedSite{
					buf: buf, prefix: "ERROR [scan/signature] ", words: []string{rel}, embedded: []string{path},
					old: fmt.Sprintf("ERROR [scan/signature] read %s: %v", rel, readErr),
				}
			},
		},
		{
			name: "stage error with a path", file: "scanner.go recordStageError",
			trigger: func(t *testing.T, value string) loggedSite {
				buf := captureLog(t)
				path := filepath.Join(tempBase(t), value)
				stageErr := &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist}
				recordStageError(&Result{}, "kubeconform", stageErr)
				return loggedSite{
					buf: buf, prefix: "ERROR [scan/kubeconform] ", embedded: []string{path},
					old: fmt.Sprintf("ERROR [scan/kubeconform] stage failed: %v", stageErr),
				}
			},
		},
		{
			name: "stage error wrapping a path", file: "scanner.go recordStageError (error wrapped with %w)",
			trigger: func(t *testing.T, value string) loggedSite {
				buf := captureLog(t)
				path := filepath.Join(tempBase(t), value)
				stageErr := fmt.Errorf("listing manifests: %w", &fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist})
				recordStageError(&Result{}, "kubeconform", stageErr)
				return loggedSite{
					buf: buf, prefix: "ERROR [scan/kubeconform] ", embedded: []string{path},
					old: fmt.Sprintf("ERROR [scan/kubeconform] stage failed: %v", stageErr),
				}
			},
		},
	}
}

// seccompFinding is a KSV-0104 finding of a Kubernetes manifest, the only kind
// annotateSeccompContext reads.
func seccompFinding(file string) Finding {
	return Finding{Source: SourceTrivyMisconfig, IaCType: "kubernetes", ID: "KSV-0104", File: file}
}

// assertOneSafeEntry is the check of the hostile test, on the log one trigger
// left. It returns the entry.
func assertOneSafeEntry(t *testing.T, site loggedSite) string {
	t.Helper()
	logged := site.buf.String()
	if logged == "" {
		t.Fatal("nothing was logged: the setup does not reach the call")
	}
	// Every line is the start of an entry: a line that is not is the tail of
	// one a newline split.
	var entries []string
	for _, line := range strings.Split(strings.TrimSuffix(logged, "\n"), "\n") {
		if !hasAnyPrefix(line, entryPrefixes) {
			t.Errorf("the line %q starts no entry: a name split one\nlog:\n%s", line, logged)
		}
		if strings.HasPrefix(line, site.prefix) {
			entries = append(entries, line)
		}
	}
	if len(entries) != 1 {
		t.Fatalf("%d lines start with %q, want the one entry\nlog:\n%s", len(entries), site.prefix, logged)
	}
	entry := entries[0]
	// Nothing in the log that a reader could take for something else.
	if !utf8.ValidString(logged) {
		t.Errorf("the log holds bytes that are not UTF-8: %q", logged)
	}
	// Line by line: the newline that ends an entry is the separator and is the
	// only one tolerated (an empty line was reported above as starting no entry).
	for _, line := range strings.Split(strings.TrimSuffix(logged, "\n"), "\n") {
		for _, r := range line {
			if r != utf8.RuneError && !unicode.IsPrint(r) {
				t.Errorf("the log holds the unprintable rune %U inside the line %q", r, line)
				break
			}
		}
	}
	for _, w := range site.words {
		if q := strconv.Quote(w); !strings.Contains(entry, q) {
			t.Errorf("the entry does not hold %s, the name quoted as logWord writes it:\n%s", q, entry)
		}
	}
	for _, e := range site.embedded {
		quoted := strconv.Quote(e)
		if esc := quoted[1 : len(quoted)-1]; !strings.Contains(entry, esc) {
			t.Errorf("the entry does not hold %s, the path inside the error with its escapes:\n%s", esc, entry)
		}
	}
	return entry
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// A hostile name keeps every log entry on one line and shows as %q.
func TestAHostileNameStaysOnOneLogLine(t *testing.T) {
	for _, site := range logSites() {
		for _, h := range hostileNames {
			t.Run(site.name+"/"+h.name, func(t *testing.T) {
				t.Logf("call: %s", site.file)
				assertOneSafeEntry(t, site.trigger(t, h.value))
			})
		}
	}
}

// A name that prints leaves the line as it was: logWord adds no quote mark to a
// word that is valid UTF-8 and holds only printable runes, accents and spaces
// included, so the entry is the one the call wrote before DEVDESK-775.
func TestAnOrdinaryNameLeavesTheLogLineAsItWas(t *testing.T) {
	for _, site := range logSites() {
		for _, o := range ordinaryNames(!site.perFileName) {
			t.Run(site.name+"/"+o.name, func(t *testing.T) {
				t.Logf("call: %s", site.file)
				l := site.trigger(t, o.value)
				var entries []string
				for _, line := range strings.Split(strings.TrimSuffix(l.buf.String(), "\n"), "\n") {
					if strings.HasPrefix(line, l.prefix) {
						entries = append(entries, line)
					}
				}
				if len(entries) != 1 || entries[0] != l.old {
					t.Errorf("the entries starting with %q are\n%q\nwant the one line as it was\n%q", l.prefix, entries, l.old)
				}
			})
		}
	}
}

// What an error text holds is protected whichever way it got there: through
// the error itself, or wrapped. The text of an ordinary error is not touched,
// and a nil error prints as it did.
func TestTheErrorTextOfAStageIsProtected(t *testing.T) {
	t.Run("a wrapped error keeps its prefix", func(t *testing.T) {
		buf := captureLog(t)
		cause := &fs.PathError{Op: "open", Path: "/srv/dépôt/a\nb", Err: fs.ErrNotExist}
		recordStageError(&Result{}, "signature", fmt.Errorf("base image: %w", cause))
		entry := assertOneSafeEntry(t, loggedSite{buf: buf, prefix: "ERROR [scan/signature] ", embedded: []string{cause.Path}})
		for _, kept := range []string{"base image: ", "open ", fs.ErrNotExist.Error()} {
			if !strings.Contains(entry, kept) {
				t.Errorf("the text around the path lost %q: %s", kept, entry)
			}
		}
	})
	t.Run("the result keeps the error as it was", func(t *testing.T) {
		// Whether result.Errors, which the views show, should be quoted too is
		// open (DEVDESK-775): this pins only that the log change does not
		// touch it by accident for an ordinary error.
		captureLog(t)
		var result Result
		recordStageError(&result, "gitleaks", errors.New("exit status 2"))
		if len(result.Errors) != 1 || result.Errors[0] != "gitleaks: exit status 2" {
			t.Errorf("Errors = %q", result.Errors)
		}
	})
}

// logErr on an ordinary error that spans lines or holds a tab: the whole text
// is quoted and stays on one line (a git or helm stderr is often several
// lines), while an error that prints on one line is returned unchanged.
func TestLogErrQuotesAMultiLineErrorWhole(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"two lines", fmt.Errorf("exit status 1: l1\nl2"), `"exit status 1: l1\nl2"`},
		{"exitError with a two-line stderr", &exitError{Code: 1, Stderr: "l1\nl2"}, `"exit status 1: l1\nl2"`},
		{"a tab", errors.New("exit status 1:\tl1"), `"exit status 1:\tl1"`},
		{"one printable line", errors.New("exit status 1: l1 àpp ünï"), "exit status 1: l1 àpp ünï"},
		{"an exitError with a one-line stderr", &exitError{Code: 2, Stderr: "fatal: bad config"}, "exit status 2: fatal: bad config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := logErr(tt.err); got != tt.want {
				t.Errorf("logErr(%q) = %q, want %q", tt.err.Error(), got, tt.want)
			}
		})
	}
}
