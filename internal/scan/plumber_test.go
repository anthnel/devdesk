package scan

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// The fixtures are two real runs of plumber 0.4.40 over one repository, the
// only difference being whether a GitHub token was reachable. Their paths are
// neutralised; nothing else is edited.
func plumberFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return data
}

// ── What a run says ──────────────────────────────────────────────────────────

func TestACompleteRunCarriesItsLetterAndItsIssues(t *testing.T) {
	report, err := parsePlumberOutput(plumberFixture(t, "plumber_complete.json"))
	if err != nil {
		t.Fatalf("parsePlumberOutput: %v", err)
	}

	if report.Score != "E" || report.Points != 30 {
		t.Errorf("score = %q/%v, want E/30", report.Score, report.Points)
	}
	if report.Withheld {
		t.Error("a complete run was reported as withheld")
	}
	if len(report.Findings) != 3 {
		t.Fatalf("findings = %d, want the three the fixture holds", len(report.Findings))
	}
}

// The one that matters most. plumber writes a letter even when it says the
// score is withheld, and on this pair of fixtures the withheld run reads **B/79**
// where the complete run reads **E/30** — a control that did not run found
// nothing, so the degraded score is *better*. Quoting it would flatter a
// repository precisely when least was known about it.
func TestAWithheldRunReportsNoLetterAlthoughTheJSONCarriesOne(t *testing.T) {
	data := plumberFixture(t, "plumber_withheld.json")

	if !strings.Contains(string(data), `"score": "B"`) {
		t.Fatal("the fixture no longer carries the letter this test exists for")
	}

	report, err := parsePlumberOutput(data)
	if err != nil {
		t.Fatalf("parsePlumberOutput: %v", err)
	}

	if !report.Withheld {
		t.Fatal("a degraded run was not reported as withheld")
	}
	if report.Score != "" {
		t.Errorf("Score = %q on a withheld run, want nothing quotable", report.Score)
	}
	if len(report.Reasons) == 0 {
		t.Error("a withheld run carries no reason, so nothing can say why")
	}
}

func TestTheSeverityOfAnIssueIsJoinedOnFromTheScoreBlock(t *testing.T) {
	report, err := parsePlumberOutput(plumberFixture(t, "plumber_complete.json"))
	if err != nil {
		t.Fatalf("parsePlumberOutput: %v", err)
	}

	// An issue carries no severity of its own; it lives in
	// plumberScore.codeLosses, indexed by code. Without the join every finding
	// would be UNKNOWN, which is why --score is not optional.
	want := map[string]SeverityLevel{
		"ISSUE-501": SeverityCritical,
		"ISSUE-411": SeverityHigh,
		"ISSUE-801": SeverityMedium,
	}
	for _, f := range report.Findings {
		if got := want[f.ID]; got != f.Severity {
			t.Errorf("%s: severity = %q, want %q", f.ID, f.Severity, got)
		}
		if f.Source != SourcePlumber {
			t.Errorf("%s: source = %q, want %q", f.ID, f.Source, SourcePlumber)
		}
	}
}

func TestAWorkflowIssueCarriesItsFileAndLine(t *testing.T) {
	report, err := parsePlumberOutput(plumberFixture(t, "plumber_complete.json"))
	if err != nil {
		t.Fatalf("parsePlumberOutput: %v", err)
	}

	var found bool
	for _, f := range report.Findings {
		if f.File == "" {
			continue
		}
		found = true
		if f.Line == 0 {
			t.Errorf("%s: file %q with no line", f.ID, f.File)
		}
		if strings.HasSuffix(f.File, ":4") {
			t.Errorf("%s: the line was left on the path: %q", f.ID, f.File)
		}
	}
	if !found {
		t.Error("no finding carried a file, though the fixture has workflow issues")
	}
}

// A Windows path holds a colon of its own, so the split is on the last one and
// only when a number follows it.
func TestTheLocationSplitSurvivesADriveLetter(t *testing.T) {
	for _, tc := range []struct {
		in   string
		file string
		line int
	}{
		{`C:\repo\.github\workflows\ci.yml:4`, `C:\repo\.github\workflows\ci.yml`, 4},
		{"/repo/.github/workflows/ci.yml:12", "/repo/.github/workflows/ci.yml", 12},
		{`C:\repo\Makefile`, `C:\repo\Makefile`, 0},
		{"", "", 0},
	} {
		file, line := splitPlumberLocation(tc.in)
		if file != tc.file || line != tc.line {
			t.Errorf("splitPlumberLocation(%q) = %q,%d — want %q,%d", tc.in, file, line, tc.file, tc.line)
		}
	}
}

// ── The invocation ───────────────────────────────────────────────────────────

func TestTheDockerInvocationMountsWhatPlumberNeedsToSee(t *testing.T) {
	opts := PlumberOptions{Provider: providerGitHub, ConfigPath: "/home/me/plumber.yaml"}
	cmd := GetPlumberCommand("/repos/devdesk", ToolSpec{Source: ToolSourceDocker}, opts)

	for _, want := range []string{
		"-v /repos/devdesk:" + containerScanPath + ":ro",
		"-v /home/me/plumber.yaml:" + plumberConfigMount + ":ro",
		"--config " + plumberConfigMount,
		// analyze takes no path argument: it works on the working directory,
		// so the mount has to be it.
		"-w " + containerScanPath,
		// Without this the image, which runs as uid 65532, cannot read the
		// mounted repository at all: git refuses it as dubious ownership and
		// plumber answers "not in a git repository".
		"GIT_CONFIG_VALUE_0=" + containerScanPath,
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("the invocation is missing %q:\n%s", want, cmd)
		}
	}
	if strings.Contains(cmd, "--config /home/me/plumber.yaml") {
		t.Errorf("the host path reached the container:\n%s", cmd)
	}
	// Everything after the image name is plumber's own argv.
	if strings.Index(cmd, "-v /home/me") > strings.Index(cmd, DefaultPlumberImage) {
		t.Errorf("a mount was declared after the image name:\n%s", cmd)
	}
}

// The token never goes in argv, which is readable from the process list, and it
// never reaches the string that is logged and shown.
func TestTheTokenIsNeverInTheInvocation(t *testing.T) {
	opts := PlumberOptions{Provider: providerGitHub, Token: "ghp_secretsecretsecret"}

	for _, source := range []ToolSource{ToolSourceBinary, ToolSourceDocker} {
		if cmd := GetPlumberCommand("/repos", ToolSpec{Source: source}, opts); strings.Contains(cmd, "ghp_secret") {
			t.Errorf("%s: the token is in the shown command:\n%s", source, cmd)
		}
	}

	// It does reach a container, through the environment.
	cmd := plumberArgs("/repos", ToolSpec{Source: ToolSourceDocker}, opts, "/dev/stdout")
	if !strings.Contains(strings.Join(cmd.Args, " "), "GH_TOKEN=ghp_secretsecretsecret") {
		t.Error("the token did not reach the container at all")
	}
}

// One variable, chosen by provider. Naming both would offer a GitLab token to
// GitHub, which is the hazard §3.17 exists for.
func TestTheTokenVariableFollowsTheProvider(t *testing.T) {
	for provider, want := range map[string]string{
		providerGitHub: "GH_TOKEN",
		providerGitLab: "GITLAB_TOKEN",
	} {
		env := plumberTokenEnv(PlumberOptions{Provider: provider, Token: "t"})
		joined := strings.Join(env, " ")
		if !strings.Contains(joined, want+"=t") {
			t.Errorf("provider %q used %q, want %q", provider, joined, want)
		}
		other := "GITLAB_TOKEN"
		if want == "GITLAB_TOKEN" {
			other = "GH_TOKEN"
		}
		if strings.Contains(joined, other) {
			t.Errorf("provider %q also declared %q: %q", provider, other, joined)
		}
	}
}

// No token means no variable rather than an empty one: plumber reads "set but
// empty" as an attempt, and then reports a rejected credential rather than a
// missing one.
func TestNoTokenDeclaresNoVariable(t *testing.T) {
	if env := plumberTokenEnv(PlumberOptions{Provider: providerGitHub}); len(env) != 0 {
		t.Errorf("plumberTokenEnv with no token = %v, want nothing", env)
	}
}

// --score is what puts the severities in the report, and --print=false is what
// keeps stdout parseable.
func TestTheInvocationAlwaysAsksForTheScoreAndSuppressesTheReport(t *testing.T) {
	cmd := GetPlumberCommand("/repos", ToolSpec{Source: ToolSourceBinary}, PlumberOptions{})

	for _, want := range []string{"analyze", "--score", "--print=false"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("the invocation is missing %q:\n%s", want, cmd)
		}
	}
}

// The five outbound flags publish or write to someone's project. The guarantee
// is that nothing builds them, which is why this asks the command rather than
// the configuration.
func TestNoOutboundFlagIsEverBuilt(t *testing.T) {
	opts := PlumberOptions{
		Provider: providerGitLab, Branch: "main",
		ConfigPath: "/etc/plumber.yaml", GitLabURL: "https://gitlab.example",
	}
	for _, source := range []ToolSource{ToolSourceBinary, ToolSourceDocker} {
		cmd := GetPlumberCommand("/repos", ToolSpec{Source: source}, opts)
		for _, forbidden := range []string{"--score-push", "--score-endpoint", "--badge", "--mr-comment", "--platform"} {
			if strings.Contains(cmd, forbidden) {
				t.Errorf("%s: %q is in the invocation:\n%s", source, forbidden, cmd)
			}
		}
	}
}

// ── Which repositories are graded ────────────────────────────────────────────

func ciConfig(forgeType, forgeURL string) ScanOptions {
	return ScanOptions{
		EnableCIScore: true,
		Forge:         config.ForgeConfig{Type: forgeType, URL: forgeURL},
	}
}

// A context targets one forge, so it grades that forge's repositories and no
// others. A repository elsewhere is not scanned anonymously — it is not
// scannable.
func TestOnlyThisContextsForgeIsGraded(t *testing.T) {
	opts := ciConfig(config.ForgeGitHub, "https://github.com")

	for _, tc := range []struct {
		remote string
		want   bool
	}{
		{"https://github.com/anthnel/devdesk.git", true},
		{"git@github.com:anthnel/devdesk.git", true},
		{"https://gitlab.com/acme/thing.git", false},
		{"https://git.customer.example/acme/thing.git", false},
		{"", false},
	} {
		if _, ok := ciOptionsFor(opts, tc.remote, "main", nil); ok != tc.want {
			t.Errorf("remote %q: graded = %v, want %v", tc.remote, ok, tc.want)
		}
	}
}

// The token is loaded only for a repository that will be graded, so one of
// another host never reaches the secret store.
func TestAForeignRepositoryNeverReachesTheSecretStore(t *testing.T) {
	asked := 0
	load := func() string { asked++; return "t" }

	opts := ciConfig(config.ForgeGitHub, "https://github.com")
	if _, ok := ciOptionsFor(opts, "https://gitlab.com/acme/thing.git", "main", load); ok {
		t.Fatal("a GitLab repository was graded by a GitHub context")
	}
	if asked != 0 {
		t.Errorf("the store was read %d times for a repository that is not graded", asked)
	}

	if _, ok := ciOptionsFor(opts, "https://github.com/anthnel/devdesk.git", "main", load); !ok {
		t.Fatal("the context's own repository was refused")
	}
	if asked != 1 {
		t.Errorf("the store was read %d times, want once", asked)
	}
}

// --provider is declared from the configuration, never sniffed from the remote.
func TestTheProviderComesFromTheContextRatherThanTheRemote(t *testing.T) {
	got, ok := ciOptionsFor(ciConfig(config.ForgeGitHub, "https://github.com"),
		"https://github.com/a/b.git", "main", nil)
	if !ok || got.Provider != providerGitHub {
		t.Errorf("provider = %q, want %q", got.Provider, providerGitHub)
	}
	// And a GitHub context never passes --gitlab-url, which is part of how
	// plumber decides to take the GitLab path at all.
	if got.GitLabURL != "" {
		t.Errorf("a GitHub context carried a GitLab URL: %q", got.GitLabURL)
	}

	got, ok = ciOptionsFor(ciConfig(config.ForgeGitLab, "https://gitlab.example"),
		"https://gitlab.example/a/b.git", "main", nil)
	if !ok || got.Provider != providerGitLab || got.GitLabURL != "https://gitlab.example" {
		t.Errorf("gitlab context = %+v, want the gitlab provider and its URL", got)
	}
}

// The setting is what turns the stage on; the rule above only decides which
// repositories it applies to.
func TestTheStageIsOffWhenTheSettingIs(t *testing.T) {
	s := newScannerWithDeps(ScanOptions{Forge: config.ForgeConfig{Type: config.ForgeGitHub, URL: "https://github.com"}},
		DependencyStatus{PlumberAvailable: true})

	if _, ok := s.ciOptions(t.TempDir(), TargetDirectory); ok {
		t.Error("the CI stage ran with enable_ci_score off")
	}
}

// An image has no pipeline, so there is nothing to grade — the tab is empty on
// an image result, and no container is started for one.
func TestAnImageIsNeverGraded(t *testing.T) {
	opts := ciConfig(config.ForgeGitHub, "https://github.com")
	s := newScannerWithDeps(opts, DependencyStatus{PlumberAvailable: true})

	if _, ok := s.ciOptions("nginx:1.25", TargetImage); ok {
		t.Error("an image was sent to plumber")
	}
}

// ── The exit codes ───────────────────────────────────────────────────────────

// Measured on 0.4.40 and 0.4.42: 0 and 1 are verdicts, 3 is "I cannot
// conclude", and only 2 is a failure. Reading 3 as an error would lose the one
// state the `?` cell exists for; reading 2 as a verdict would put a failure in
// the column as though it were a grade.
func TestOnlyARuntimeErrorFailsTheRun(t *testing.T) {
	complete := string(plumberFixture(t, "plumber_complete.json"))

	for _, tc := range []struct {
		name    string
		stdout  string
		err     error
		wantErr bool
	}{
		{"gate met", complete, nil, false},
		{"gate failed", complete, &exitError{Code: plumberGateFailed}, false},
		{"score withheld", complete, &exitError{Code: plumberScoreWithheld}, false},
		{"runtime error", "", &exitError{Code: plumberRuntimeError, Stderr: "GITLAB_TOKEN is required"}, true},
		{"never ran", "", errors.New("plumber failed to start"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			answering(t, tc.stdout, tc.err)

			_, err := RunPlumber(context.Background(), "/repos",
				ToolSpec{Source: ToolSourceDocker}, PlumberOptions{Provider: providerGitHub}, nil)

			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Errorf("error = %v, want an error: %v", err, tc.wantErr)
			}
		})
	}
}

// §3.50's guard, for §3.50's reason: `docker run -v` on a host path that is not
// there creates a directory rather than failing.
func TestAnUnreadablePlumberConfigIsRefusedBeforeAnythingStarts(t *testing.T) {
	r := answering(t, "{}", nil)

	missing := filepath.Join(t.TempDir(), "plumber.yaml")
	_, err := RunPlumber(context.Background(), "/repos", ToolSpec{Source: ToolSourceDocker},
		PlumberOptions{Provider: providerGitHub, ConfigPath: missing}, nil)

	if err == nil {
		t.Fatal("a config that is not there was accepted")
	}
	if cmds := r.commands(); len(cmds) != 0 {
		t.Errorf("the tool was started anyway: %v", cmds)
	}
}

// The vocabulary --provider takes is stated in this package and the forge types
// in internal/config; scan must not import a forge backend and config must not
// be the authority on a tool's flags, so a test is the only thing that can hold
// the two in step. Same arrangement as the registry provider names.
func TestThePlumberProviderVocabularyMatchesTheConfig(t *testing.T) {
	if providerGitLab != config.ForgeGitLab {
		t.Errorf("providerGitLab = %q, config says %q", providerGitLab, config.ForgeGitLab)
	}
	if providerGitHub != config.ForgeGitHub {
		t.Errorf("providerGitHub = %q, config says %q", providerGitHub, config.ForgeGitHub)
	}
}

// The one the user hit. `plumber analyze` takes no path argument: it works on
// the current directory. In binary mode nothing said which, so it analysed
// whatever directory DevDesk had been launched from and answered
// `--project is required (could not auto-detect from git remote)` — a failure
// about a repository nobody had asked it to look at.
//
// Measured after the fix on the reported repository: the error changes from
// "--project is required" to "GITLAB_TOKEN is required", which is plumber
// having found the project and asking for the next thing.
func TestTheBinaryRunsInTheRepositoryRatherThanWhereverDevDeskWasLaunched(t *testing.T) {
	cmd := plumberArgs("/repos/devdesk", ToolSpec{Source: ToolSourceBinary}, PlumberOptions{}, "/tmp/out.json")

	if cmd.Dir != "/repos/devdesk" {
		t.Errorf("Dir = %q, want the repository — analyze takes no path argument", cmd.Dir)
	}
	// And the shown command has to be re-runnable, or a logged invocation says
	// less than it looks like it does.
	if shown := cmd.String(); !strings.HasPrefix(shown, "cd /repos/devdesk && ") {
		t.Errorf("the shown command does not carry the directory:\n%s", shown)
	}
}

// The container gets the same thing through -w, so it sets no Dir: docker runs
// on this machine and its own working directory is irrelevant.
func TestTheContainerCarriesItsWorkingDirectoryInTheArguments(t *testing.T) {
	cmd := plumberArgs("/repos/devdesk", ToolSpec{Source: ToolSourceDocker}, PlumberOptions{}, "/dev/stdout")

	if cmd.Dir != "" {
		t.Errorf("Dir = %q on the docker path, want none", cmd.Dir)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "-w "+containerScanPath) {
		t.Error("the container has no working directory either")
	}
}

// Trivy and Gitleaks take their target in argv, so nothing changed for them —
// and must not, or a scan would start running somewhere it never did.
func TestTheOtherTwoScannersStillRunWhereTheyDid(t *testing.T) {
	if cmd := gitleaksArgs("/repos", ToolSpec{Source: ToolSourceBinary}, false, ""); cmd.Dir != "" {
		t.Errorf("gitleaks gained a working directory: %q", cmd.Dir)
	}
	cmd, err := trivyArgs("/repos", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, "", false, false)
	if err != nil {
		t.Fatalf("trivyArgs: %v", err)
	}
	if cmd.Dir != "" {
		t.Errorf("trivy gained a working directory: %q", cmd.Dir)
	}
}

// A score is a weighted sum, so a whole number is the exception. Both fixtures
// happened to be whole, which is why an int decoded them and the first
// repository with a fractional score failed the entire stage with
// `cannot unmarshal number 25.698320532936123 into Go struct field
// plumberScore.finalPoints of type int`.
func TestAFractionalScoreIsNotAParseFailure(t *testing.T) {
	report, err := parsePlumberOutput([]byte(`{
		"plumberScore": {
			"score": "D",
			"finalPoints": 25.698320532936123,
			"codeLosses": [{"code": "ISSUE-411", "severity": "high"}]
		},
		"someResult": {
			"controlName": "aControl",
			"issues": [{"code": "ISSUE-411"}]
		}
	}`))
	if err != nil {
		t.Fatalf("a fractional score failed to parse: %v", err)
	}
	if report.Score != "D" {
		t.Errorf("Score = %q, want D", report.Score)
	}
	if report.Points < 25.6 || report.Points > 25.7 {
		t.Errorf("Points = %v, want the fractional value it was given", report.Points)
	}
	if len(report.Findings) != 1 || report.Findings[0].Severity != SeverityHigh {
		t.Errorf("findings = %+v, want the one issue with its severity", report.Findings)
	}
}

// The views do not print a tool's stderr any more, so this package has to log
// it: it used to append to Result.Errors and log nothing, which made the screen
// the only copy of the reason.
func TestEveryStageFailureIsLogged(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	result := &Result{}
	recordStageError(result, "plumber", errors.New("--project is required"))

	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "plumber: ") {
		t.Errorf("Errors = %v, want the stage named", result.Errors)
	}
	logged := buf.String()
	if !strings.Contains(logged, "plumber") || !strings.Contains(logged, "--project is required") {
		t.Errorf("the reason is not in the log: %q", logged)
	}
	if !strings.Contains(logged, "ERROR") {
		t.Errorf("the log line is not marked as an error: %q", logged)
	}
}
