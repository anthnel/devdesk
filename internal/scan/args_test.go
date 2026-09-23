package scan

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
)

// ── Where a tool's extra arguments go ───────────────────────────────────────

// after reports whether want sits right after the first occurrence of anchor.
func after(t *testing.T, args []string, anchor string, want ...string) {
	t.Helper()
	i := slices.Index(args, anchor)
	if i < 0 || i+len(want) >= len(args)+1 || !slices.Equal(args[i+1:i+1+len(want)], want) {
		t.Errorf("want %v right after %q in %v", want, anchor, args)
	}
}

var extra = []string{"--debug", "--label", "a b"}

// The user's arguments go after the subcommand and before DevDesk's own and the
// target, for every tool, from a binary and from an image.
func TestExtraArgumentsFollowTheSubcommand(t *testing.T) {
	for _, source := range []ToolSource{ToolSourceBinary, ToolSourceContainer} {
		tool := ToolSpec{Source: source, Args: extra}

		tc, _ := trivyArgs("/repo", TargetDirectory, false, tool, "", false, false)
		after(t, tc.Args, "fs", extra...)

		after(t, gitleaksArgs("/repo", tool, false, "", "").Args, "detect", extra...)
		after(t, plumberArgs("/repo", tool, PlumberOptions{}, "/out.json").Args, "analyze", extra...)
		after(t, helmArgs("/repo", "charts/api", tool, "template").Args, "template", extra...)
		after(t, kustomizeArgs("/repo", "k8s/prod", tool).Args, "build", extra...)
	}
}

// kubeconform has no subcommand and parses with Go's flag package, which stops
// at the first positional argument: its extra flags come before every file.
func TestKubeconformsExtraFlagsComeBeforeTheFiles(t *testing.T) {
	for _, source := range []ToolSource{ToolSourceBinary, ToolSourceContainer} {
		args := kubeconformArgs("/repo", []string{"deploy/app.yaml"}, ToolSpec{Source: source, Args: extra},
			KubeconformOptions{KubernetesVersion: "1.36.0"}).Args
		debug, file := slices.Index(args, "--debug"), slices.Index(args, "deploy/app.yaml")
		if debug < 0 || debug > slices.Index(args, "-output") || debug > file {
			t.Errorf("%s: extra flags not before DevDesk's and the files: %v", source, args)
		}
	}
}

// ── Trivy's rules file ──────────────────────────────────────────────────────

func TestTrivysConfigIsPassedAndMounted(t *testing.T) {
	tc, _ := trivyArgs("/repo", TargetDirectory, false,
		ToolSpec{Source: ToolSourceBinary, Config: "/etc/trivy.yaml"}, "", false, false)
	after(t, tc.Args, "fs", "--config", "/etc/trivy.yaml")

	tc, _ = trivyArgs("/repo", TargetDirectory, false,
		ToolSpec{Source: ToolSourceContainer, Config: "/etc/trivy.yaml"}, "", false, false)
	after(t, tc.Args, "fs", "--config", trivyConfigMount)
	if cmd := tc.String(); !strings.Contains(cmd, "-v /etc/trivy.yaml:"+trivyConfigMount+":ro") ||
		strings.Index(cmd, trivyConfigMount+":ro") > strings.Index(cmd, DefaultTrivyImage) {
		t.Errorf("the rules file is not mounted before the image: %s", cmd)
	}
	// DevDesk's --format still comes after, so it wins over a format: in the file.
	if slices.Index(tc.Args, "--config") > slices.Index(tc.Args, "--format") {
		t.Errorf("--format comes before --config: %v", tc.Args)
	}
}

// A rules file that cannot be read is refused before anything starts: `docker
// run -v` would create a directory in its place.
func TestAnUnreadableTrivyConfigIsRefusedBeforeAnythingStarts(t *testing.T) {
	r := byStage(t, map[string]stageReply{})
	missing := filepath.Join(t.TempDir(), "nope.yaml")

	_, err := RunTrivy(context.Background(), "/repo", TargetDirectory, false,
		ToolSpec{Source: ToolSourceContainer, Config: missing}, "", false, false, nil)

	if err == nil || !strings.Contains(err.Error(), "trivy config") {
		t.Errorf("err = %v, want the rules file refused", err)
	}
	if r.count() != 0 {
		t.Errorf("%d process(es) started with an unreadable rules file", r.count())
	}
	if _, statErr := os.Stat(missing); statErr == nil {
		t.Error("something was created at the missing path")
	}
}

// What a scan runs carries the context's arguments and rules file — the
// command shown is built by the same builder, so it says the same (D19).
func TestTheContextsArgumentsReachTheScan(t *testing.T) {
	r := byStage(t, map[string]stageReply{"vuln": {stdout: `{"Results":[]}`}})
	rules := filepath.Join(t.TempDir(), "trivy.yaml")
	if err := os.WriteFile(rules, []byte("severity: HIGH\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := scanFor(CategoryIDVuln)
	opts.Tools.Trivy.Args = []string{"--skip-dirs", "vendor"}
	opts.Tools.Trivy.Config = rules

	if _, err := newScannerWithDeps(opts, everyTool()).Scan(context.Background(), "/repos", TargetDirectory); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	ran := r.commandForStage("vuln")
	for _, want := range []string{"--skip-dirs vendor", "--config " + rules} {
		if !strings.Contains(ran, want) {
			t.Errorf("the vuln stage ran without %q: %s", want, ran)
		}
	}
	s := newScannerWithDeps(opts, everyTool())
	if shown := GetTrivyCommand("/repos", TargetDirectory, false, s.spec(ToolTrivy), "", false, false); shown != ran {
		t.Errorf("shown %q, ran %q", shown, ran)
	}
}

// ── The line the configuration view edits ───────────────────────────────────

func TestArgsSplitAndJoinBackToTheSameList(t *testing.T) {
	for line, want := range map[string][]string{
		"":                               nil,
		"--skip-dirs vendor":             {"--skip-dirs", "vendor"},
		`--label "a b"  --x='c "d"'`:     {"--label", "a b", `--x=c "d"`},
		"  --set image.tag=1   --debug ": {"--set", "image.tag=1", "--debug"},
	} {
		got, err := SplitArgs(line)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("SplitArgs(%q) = %q, %v; want %q", line, got, err, want)
			continue
		}
		if again, _ := SplitArgs(JoinArgs(got)); !slices.Equal(again, want) {
			t.Errorf("JoinArgs(%q) = %q does not split back", got, JoinArgs(got))
		}
	}
	if _, err := SplitArgs(`--label "open`); err == nil {
		t.Error("an unclosed quote was accepted")
	}
}

// A flag DevDesk sets is refused by name, spelled either way.
func TestAReservedFlagIsRefusedByName(t *testing.T) {
	trivy, _ := ToolByID(ToolTrivy)
	for _, arg := range []string{"--format", "--format=table", "-o"} {
		err := trivy.CheckArgs([]string{"--debug", arg})
		name, _, _ := strings.Cut(arg, "=")
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("CheckArgs(%q) = %v, want it refused by name", arg, err)
		}
	}
	if err := trivy.CheckArgs([]string{"--skip-dirs", "vendor"}); err != nil {
		t.Errorf("an ordinary flag was refused: %v", err)
	}
}

// Every tool reserves at least the flags that carry its output — the one thing
// an extra argument could break without anyone noticing until the parse fails.
func TestEveryToolReservesItsOutputFlags(t *testing.T) {
	for _, tool := range Tools() {
		if len(tool.ReservedArgs) == 0 {
			t.Errorf("%s reserves nothing", tool.ID)
		}
	}
	var tools config.ScanTools
	tools.Trivy.Args = []string{"--debug"}
	if !SameDetection(config.ScanTools{}, tools) {
		t.Error("extra arguments count as a change of where a tool runs from")
	}
}
