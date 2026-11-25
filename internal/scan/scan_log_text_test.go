package scan

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// DEVDESK-768: what the scan writes to the log and to the text of a finding.
//
// (a) The "Running: ..." line carries scan.tools.*.args, which the user writes
//     and which may hold a secret (--set password=..., --token=..., a URL with
//     credentials). The line masks them; the tool name and the arguments
//     DevDesk built stay readable, and a line with nothing sensitive is the
//     line it always was.
// (b) A folder or file name comes from the repository being scanned and may
//     hold a newline: it must not split a log entry, so it goes in as %q.
// (c) The Steps of HELM-LINT and K8S-RENDER are commands to copy: for a folder
//     named "-x" the command that works is `helm lint ./-x`.

// captureLog collects what the standard logger writes, without a timestamp so a
// line starts with its own text, and puts the logger back afterwards.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return &buf
}

// ---------------------------------------------------------------- (a)

func TestLoggableMasksTheValuesOfTheUsersArguments(t *testing.T) {
	tests := []struct {
		name string
		cmd  toolCmd
		want string   // the whole line, when it is exact
		hide []string // never in the line
		keep []string // always in the line
	}{
		{
			name: "key=value after a flag",
			cmd:  toolCmd{Name: "helm", Args: []string{"template", "--set", "password=hunter2", "./charts/app"}},
			want: "helm template --set password=*** ./charts/app",
		},
		{
			name: "flag=value",
			cmd:  toolCmd{Name: "kustomize", Args: []string{"build", "--token=hunter2", "./overlay"}},
			want: "kustomize build --token=*** ./overlay",
		},
		{
			name: "every pair of a comma list is not read, the whole value goes",
			cmd:  toolCmd{Name: "helm", Args: []string{"lint", "--set", "a=1,b=2", "./c"}},
			hide: []string{"a=1", "b=2"},
			keep: []string{"helm lint --set", "./c"},
		},
		{
			name: "credentials of a URL",
			cmd:  toolCmd{Name: "helm", Args: []string{"template", "https://user:s3cr3t@charts.example.com/stable", "./c"}},
			want: "helm template https://charts.example.com/stable ./c",
		},
		{
			name: "a token alone in the userinfo",
			cmd:  toolCmd{Name: "helm", Args: []string{"template", "https://s3cr3t@charts.example.com/stable"}},
			hide: []string{"s3cr3t"},
			keep: []string{"charts.example.com/stable"},
		},
		{
			name: "a URL with credentials as the value of a flag",
			cmd:  toolCmd{Name: "helm", Args: []string{"template", "--repo=https://user:s3cr3t@h.example/x"}},
			hide: []string{"s3cr3t", "user:"},
			keep: []string{"helm template --repo="},
		},
		{
			name: "the working directory is kept, the value is masked",
			cmd:  toolCmd{Name: "plumber", Args: []string{"analyze", "--token=hunter2"}, Dir: "/repo"},
			want: "cd /repo && plumber analyze --token=***",
		},
	}
	for _, tt := range tests {
		got := tt.cmd.Loggable()
		if tt.want != "" && got != tt.want {
			t.Errorf("%s: Loggable() = %q, want %q", tt.name, got, tt.want)
		}
		for _, h := range tt.hide {
			if strings.Contains(got, h) {
				t.Errorf("%s: %q still in %q", tt.name, h, got)
			}
		}
		for _, k := range tt.keep {
			if !strings.Contains(got, k) {
				t.Errorf("%s: %q missing from %q", tt.name, k, got)
			}
		}
	}
}

// Nothing sensitive: the line is the one String() gave before this change, to
// the byte. A log that changes for no reason is a log nobody can diff.
func TestLoggableIsTheStringOfACommandWithNothingToMask(t *testing.T) {
	tests := []struct {
		cmd  toolCmd
		want string
	}{
		{toolCmd{Name: "trivy", Args: []string{"fs", "--severity", "HIGH", "/repo"}}, "trivy fs --severity HIGH /repo"},
		{toolCmd{Name: "trivy", Args: []string{"fs", "--severity", "HIGH,CRITICAL", "--ignore-unfixed", "/repo"}},
			"trivy fs --severity HIGH,CRITICAL --ignore-unfixed /repo"},
		{toolCmd{Name: "gitleaks", Args: []string{"dir", "/repo"}}, "gitleaks dir /repo"},
		{toolCmd{Name: "helm", Args: []string{"lint", "./charts/app"}}, "helm lint ./charts/app"},
		// A URL without credentials is not a secret.
		{toolCmd{Name: "helm", Args: []string{"template", "https://charts.example.com/stable"}},
			"helm template https://charts.example.com/stable"},
		{toolCmd{Name: "plumber", Args: []string{"analyze", "--score"}, Dir: "/repo"}, "cd /repo && plumber analyze --score"},
		{toolCmd{}, ""},
	}
	for _, tt := range tests {
		if got := tt.cmd.Loggable(); got != tt.want {
			t.Errorf("Loggable() = %q, want %q", got, tt.want)
		}
		if got, str := tt.cmd.Loggable(), tt.cmd.String(); got != str {
			t.Errorf("Loggable() = %q, String() = %q: they must agree when nothing is masked", got, str)
		}
	}
}

// The real path: the line the renderers log goes through the same masking, and
// the argument the runner receives is untouched (the tool needs the value).
func TestTheRenderersLogTheirCommandWithTheUsersValuesMasked(t *testing.T) {
	isolateHome(t)
	buf := captureLog(t)
	repo := writeRepo(t, map[string]string{
		"charts/app/Chart.yaml":                 "apiVersion: v2\nname: app\nversion: 0.1.0\n",
		"kustomizations/dev/kustomization.yaml": "resources: []\n",
	})
	r := &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
		if tc.Name == "git" {
			return nil, &exitError{Code: 128, Stderr: "not a git repository"}
		}
		return nil, &exitError{Code: 1, Stderr: "Error: boom"}
	}}
	useRunner(t, r)
	opts := k8sSchemaFor("1.36.0")
	opts.Tools.Helm.Args = []string{"--set", "password=hunter2", "--repo", "https://user:s3cr3t@charts.example.com/x"}
	opts.Tools.Kustomize.Args = []string{"--token=hunter2"}

	_, _ = newScannerWithDeps(opts, renderingDeps()).Scan(t.Context(), repo, TargetDirectory)

	logged := buf.String()
	for _, secret := range []string{"hunter2", "s3cr3t"} {
		if strings.Contains(logged, secret) {
			t.Errorf("the log holds %q:\n%s", secret, logged)
		}
	}
	for _, want := range []string{"password=***", "--token=***", "helm lint", "helm template", "kustomize build"} {
		if !strings.Contains(logged, want) {
			t.Errorf("the log lacks %q:\n%s", want, logged)
		}
	}
	// What ran is what the user wrote.
	var ran bool
	for _, c := range r.calls {
		if c.Name == "helm" && strings.Contains(strings.Join(c.Args, " "), "password=hunter2") {
			ran = true
		}
	}
	if !ran {
		t.Errorf("helm did not receive the user's argument: %v", r.commands())
	}
}

// Same for the scanners of scanner.go, which log the line they run.
func TestTheScannersLogTheirCommandWithTheUsersValuesMasked(t *testing.T) {
	isolateHome(t)
	buf := captureLog(t)
	answering(t, "{}", nil)
	opts := scanFor(CategoryIDVuln)
	opts.Tools.Trivy.Args = []string{"--registry-token=hunter2", "--severity", "HIGH"}

	_, _ = newScannerWithDeps(opts, binaries(ToolTrivy, ToolGitleaks)).Scan(t.Context(), t.TempDir(), TargetDirectory)

	logged := buf.String()
	if strings.Contains(logged, "hunter2") {
		t.Errorf("the log holds the value:\n%s", logged)
	}
	if !strings.Contains(logged, "--registry-token=***") || !strings.Contains(logged, "--severity HIGH") {
		t.Errorf("the masked line or the readable arguments are missing:\n%s", logged)
	}
}

// ---------------------------------------------------------------- (b)

func TestLoggableQuotesWhatHoldsANewline(t *testing.T) {
	tests := []struct {
		name string
		cmd  toolCmd
		want string
	}{
		{"argument", toolCmd{Name: "helm", Args: []string{"lint", "./a\nb"}}, `helm lint "./a\nb"`},
		{"working directory", toolCmd{Name: "plumber", Args: []string{"analyze"}, Dir: "/repo/a\nb"},
			`cd "/repo/a\nb" && plumber analyze`},
		{"carriage return", toolCmd{Name: "helm", Args: []string{"lint", "./a\rb"}}, `helm lint "./a\rb"`},
	}
	for _, tt := range tests {
		got := tt.cmd.Loggable()
		if got != tt.want {
			t.Errorf("%s: Loggable() = %q, want %q", tt.name, got, tt.want)
		}
		if strings.ContainsAny(got, "\r\n") {
			t.Errorf("%s: the line is not one line: %q", tt.name, got)
		}
	}
	// An ordinary path is not quoted: the log stays as readable as it was.
	if got := (toolCmd{Name: "helm", Args: []string{"lint", "./charts/app"}}).Loggable(); got != "helm lint ./charts/app" {
		t.Errorf("an ordinary path was quoted: %q", got)
	}
}

// The real path: a chart and an overlay whose folder names hold a newline.
// Every entry of the log is one line (it starts with one of the prefixes the
// scan writes) and the name shows as \n.
func TestAFolderNameWithANewlineStaysOnOneLogLine(t *testing.T) {
	isolateHome(t)
	buf := captureLog(t)
	repo := writeRepo(t, map[string]string{
		"a\nb/Chart.yaml":             "apiVersion: v2\nname: x\nversion: 0.1.0\n",
		"k\nz/kustomization.yaml":     "resources: []\n",
		"plain/ok/kustomization.yaml": "resources: []\n",
	})
	useRunner(t, &scriptedRunner{reply: func(tc toolCmd) ([]byte, error) {
		if tc.Name == "git" {
			return nil, &exitError{Code: 128, Stderr: "not a git repository"}
		}
		return nil, &exitError{Code: 1, Stderr: "Error: boom"}
	}})

	_, _ = newScannerWithDeps(k8sSchemaFor("1.36.0"), renderingDeps()).Scan(t.Context(), repo, TargetDirectory)

	logged := buf.String()
	for _, line := range strings.Split(strings.TrimRight(logged, "\n"), "\n") {
		ok := false
		for _, prefix := range []string{"Running: ", "[scan", "ERROR ", "WARN "} {
			ok = ok || strings.HasPrefix(line, prefix)
		}
		if !ok {
			t.Errorf("log line %q is the tail of an entry a newline split:\n%s", line, logged)
		}
	}
	for _, sub := range []string{"helm lint", "helm template", "kustomize build"} {
		found := false
		for _, line := range strings.Split(logged, "\n") {
			if strings.HasPrefix(line, "Running: ") && strings.Contains(line, sub) && strings.Contains(line, `\n`) {
				found = true
			}
		}
		if !found {
			t.Errorf("no %q line shows the folder as %%q (\\n):\n%s", sub, logged)
		}
	}
}

// ---------------------------------------------------------------- (c)

// commandArg is the word that follows prefix in the first step holding it: the
// folder as the step tells the user to type it.
func commandArg(t *testing.T, steps []string, prefix string) string {
	t.Helper()
	for _, s := range steps {
		if _, rest, ok := strings.Cut(s, prefix); ok {
			word, _, _ := strings.Cut(rest, " ")
			return strings.TrimSuffix(word, ".")
		}
	}
	t.Fatalf("no step holds %q: %q", prefix, steps)
	return ""
}

// The steps are commands to copy. The folder is shown the way the tool must be
// given it: "-x" would be read by helm as an option, "./-x" is the folder. An
// ordinary folder keeps its plain, readable name, which the same command also
// accepts.
func TestTheStepsWriteACopiedCommandThatWorksForADashFolder(t *testing.T) {
	const overlay = "--load-restrictor=LoadRestrictionsNone"
	tests := []struct {
		name   string
		steps  []string
		prefix string
		dir    string
		want   string
	}{
		{"helm lint", helmLintSteps("-x/templates/a.yaml", "-x"), "Run helm lint ", "-x", "./-x"},
		{"helm template", renderSteps(SourceHelm, "-x", ToolSourceBinary, "boom", false), "Run helm template ", "-x", "./-x"},
		{"helm dependency build", renderSteps(SourceHelm, "-x", ToolSourceBinary, "boom", false), "run helm dependency build ", "-x", "./-x"},
		{"kustomize build", renderSteps(SourceKustomize, overlay, ToolSourceBinary, "boom", false), "Run kustomize build ", overlay, "'./" + overlay + "'"}, // "=" is quoted since DEVDESK-772 (zsh magic_equal_subst)
		{"a dash below the first segment", helmLintSteps("charts/-x/a.yaml", "charts/-x"), "Run helm lint ", "charts/-x", "charts/-x"},
		// kustomize reads these as a remote git base (DEVDESK-763): the typed
		// command gets the ./ the scan itself uses. helm reads them locally.
		{"kustomize remote-looking host", renderSteps(SourceKustomize, "github.com/org/repo", ToolSourceBinary, "boom", false), "Run kustomize build ", "github.com/org/repo", "./github.com/org/repo"},
		{"kustomize scp-like", renderSteps(SourceKustomize, "git@host:o/r", ToolSourceBinary, "boom", false), "Run kustomize build ", "git@host:o/r", "./git@host:o/r"},
		{"kustomize dotted first segment", renderSteps(SourceKustomize, "overlays.v2/dev", ToolSourceBinary, "boom", false), "Run kustomize build ", "overlays.v2/dev", "./overlays.v2/dev"},
		{"helm keeps a dotted name", renderSteps(SourceHelm, "github.com/org/repo", ToolSourceBinary, "boom", false), "Run helm template ", "github.com/org/repo", "github.com/org/repo"},
		// Ordinary folders: readable, and the typed command works as it is.
		{"ordinary chart, lint", helmLintSteps("charts/app/Chart.yaml", "charts/app"), "Run helm lint ", "charts/app", "charts/app"},
		{"ordinary chart, template", renderSteps(SourceHelm, "charts/app", ToolSourceBinary, "boom", false), "Run helm template ", "charts/app", "charts/app"},
		{"ordinary overlay", renderSteps(SourceKustomize, "kustomizations/dev", ToolSourceBinary, "boom", false), "Run kustomize build ", "kustomizations/dev", "kustomizations/dev"},
	}
	for _, tt := range tests {
		got := commandArg(t, tt.steps, tt.prefix)
		if got != tt.want {
			t.Errorf("%s: the step writes %q, want %q (steps %q)", tt.name, got, tt.want, tt.steps)
		}
		// Whatever is written there must be a word the tool reads as the folder:
		// never a bare name that starts with a dash.
		if strings.HasPrefix(got, "-") {
			t.Errorf("%s: %q is an option to the tool, not the folder", tt.name, got)
		}
	}
}

// ---------------------------------------------------------------- review 1

// The key of a key=value argument is anything without a space, "=", "/" or ":"
// that does not start with "." (a path). Helm keys carry brackets, braces,
// dots, commas and backslashes. A key with "/" is ambiguous with a relative
// path (charts/a=b), so it is left readable: the choice favours not rewriting
// a path over masking a key helm does not use.
func TestLoggableMasksTheKeysHelmWrites(t *testing.T) {
	tests := []struct {
		arg  string
		want string
	}{
		{"secrets[0].password=hunter2", "secrets[0].password=***"},
		{"image.pullSecrets{0}=hunter2", "image.pullSecrets{0}=***"},
		{`a\.b=hunter2`, `a\.b=***`},
		{"a,b=hunter2", "a,b=***"},
		{"--set-string=a.b=hunter2", "--set-string=***"},
		{"auth/token=hunter2", "auth/token=hunter2"},
		{"./charts/a=b", "./charts/a=b"},
		{"/repo/a=b", "/repo/a=b"},
		{".hidden=x", ".hidden=x"},
	}
	for _, tt := range tests {
		got := toolCmd{Name: "helm", Args: []string{"template", "--set", tt.arg}}.Loggable()
		if want := "helm template --set " + tt.want; got != want {
			t.Errorf("%q: Loggable() = %q, want %q", tt.arg, got, want)
		}
	}
}

func TestLoggableRemovesAllOfAURLsUserinfoAndItsQueryValues(t *testing.T) {
	tests := []struct {
		arg  string
		want string
		hide []string
	}{
		{"https://u:hun@ter2@h.example/x", "https://h.example/x", []string{"hun", "ter2", "u:"}},
		{"https://h.example/x?token=abc&ref=main", "https://h.example/x?token=***&ref=***", []string{"abc"}},
		{"https://h.example/x?a=1#frag", "https://h.example/x?a=***#frag", nil},
		{"https://u:p@h.example/x?token=abc", "https://h.example/x?token=***", []string{"abc", "u:p"}},
		// No "://": the query rule does not apply (a word like this one is a
		// key=value to the other rule, which masks it).
		{"ref?a=1&b=2", "ref?a=***", nil},
	}
	for _, tt := range tests {
		got := toolCmd{Name: "helm", Args: []string{"pull", tt.arg}}.Loggable()
		if want := "helm pull " + tt.want; got != want {
			t.Errorf("%q: Loggable() = %q, want %q", tt.arg, got, want)
		}
		for _, h := range tt.hide {
			if strings.Contains(got, h) {
				t.Errorf("%q: %q still in %q", tt.arg, h, got)
			}
		}
	}
}

func TestLogWordQuotesWhatIsNotPrintable(t *testing.T) {
	quoted := []string{
		"a\u2028b",   // line separator
		"a\u2029b",   // paragraph separator
		"a\u202eb",   // right-to-left override
		"a\u0085b",   // C1 next line
		"a\xffb",     // invalid UTF-8
		"a\x1b[31mb", // escape
	}
	for _, w := range quoted {
		got := logWord(w)
		if !strings.HasPrefix(got, `"`) || got == w {
			t.Errorf("logWord(%q) = %q, want it quoted", w, got)
		}
	}
	for _, w := range []string{"charts/app", "répertoire/été", "日本語/チャート", "with space", "a=b"} {
		if got := logWord(w); got != w {
			t.Errorf("logWord(%q) = %q, an ordinary name must stay bare", w, got)
		}
	}
}
