package enginefixture_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-08b, criterion 2 (second half): a "record: pending" file is never
// versioned, a recording session is a local matter, and nothing the project runs
// by itself (mise tasks of the standard checks, GitHub workflows) opens one.
//
// A pending file holds an argv and no answer: the recorder writes it before it
// has run anything. Load refuses it on purpose, so a campaign that holds one is
// out of the replay, the stale and the round-trip checks while it stays. The
// leak check still reads it (leaks_tree_test.go).

const (
	// recordingSessionEnv lets pending files exist: set only while the recorder is
	// filling a campaign on this machine. Same name as for the Nexus fixtures.
	recordingSessionEnv = "DEVDESK_FIXTURE_RECORDING"
	// ciEnv is set (to anything) by every CI system, GitHub Actions included.
	ciEnv = "CI"
)

// pendingIn lists, sorted, the fixture files of the pattern whose header says
// "record: pending". It reads the headers only (up to the blank line), so a
// pending file that Parse would refuse for another reason is still found.
func pendingIn(t testing.TB, patterns ...string) []string {
	t.Helper()
	var pending []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range matches {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			sc := bufio.NewScanner(bytes.NewReader(data))
			for sc.Scan() && sc.Text() != "" {
				if sc.Text() == "# record: "+enginefixture.RecordPending {
					pending = append(pending, file)
					break
				}
			}
		}
	}
	slices.Sort(pending)
	return pending
}

// recordingVerdict is the decision, as a pure function of the environment and of
// the pending files found:
//
//   - DEVDESK_FIXTURE_RECORDING=1 while CI is set is an error, whether or not a
//     pending file exists: a recording session never runs in CI;
//   - DEVDESK_FIXTURE_RECORDING=1 without CI tolerates the pending files, with a
//     note that says so;
//   - otherwise a pending file is an error that names it.
//
// Only the value "1" opens a recording session.
func recordingVerdict(getenv func(string) string, pending []string) (note string, err error) {
	recording := getenv(recordingSessionEnv) == "1"
	if recording && getenv(ciEnv) != "" {
		return "", fmt.Errorf("%s=1 while %s is set: a recording session never runs in CI; unset %s", recordingSessionEnv, ciEnv, recordingSessionEnv)
	}
	if len(pending) == 0 {
		return "no pending fixture", nil
	}
	if recording {
		return fmt.Sprintf("%s=1: %d pending fixture(s) tolerated for this recording session", recordingSessionEnv, len(pending)), nil
	}
	return "", fmt.Errorf("%d pending fixture(s), e.g. %s: record them with the recorder, or set %s=1 for a local recording session",
		len(pending), pending[0], recordingSessionEnv)
}

func envOf(pairs ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return func(k string) string { return m[k] }
}

func TestRecordingVerdict(t *testing.T) {
	const pendingFile = "fixtures/lifecycle/rm--docker-29.10.1-rootful.cli"
	one := []string{pendingFile}
	tests := []struct {
		name      string
		env       func(string) string
		pending   []string
		wantErr   bool
		wantInErr []string
		wantNote  string
	}{
		{"nothing pending, no session", envOf(), nil, false, nil, ""},
		{"nothing pending, CI", envOf("CI", "true"), nil, false, nil, ""},
		{"a pending file, no session", envOf(), one, true, []string{pendingFile, recordingSessionEnv}, ""},
		{"a pending file in CI", envOf("CI", "true"), one, true, []string{pendingFile}, ""},
		{"a pending file, a session outside CI", envOf(recordingSessionEnv, "1"), one, false, nil, "tolerated"},
		{"a pending file, a session, CI empty", envOf(recordingSessionEnv, "1", "CI", ""), one, false, nil, "tolerated"},
		{"a session in CI, with a pending file", envOf(recordingSessionEnv, "1", "CI", "true"), one, true, []string{recordingSessionEnv, "CI"}, ""},
		{"a session in CI, without a pending file", envOf(recordingSessionEnv, "1", "CI", "true"), nil, true, []string{recordingSessionEnv, "CI"}, ""},
		{"a session in CI, whatever the value of CI", envOf(recordingSessionEnv, "1", "CI", "false"), nil, true, []string{recordingSessionEnv}, ""},
		{"only 1 opens a session: true", envOf(recordingSessionEnv, "true"), one, true, []string{pendingFile}, ""},
		{"only 1 opens a session: 0", envOf(recordingSessionEnv, "0"), one, true, []string{pendingFile}, ""},
		{"a session variable that is not 1 is harmless in CI", envOf(recordingSessionEnv, "0", "CI", "true"), nil, false, nil, ""},
		{"a session outside CI, nothing pending", envOf(recordingSessionEnv, "1"), nil, false, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			note, err := recordingVerdict(tt.env, tt.pending)
			if (err != nil) != tt.wantErr {
				t.Fatalf("recordingVerdict error = %v, want error: %v", err, tt.wantErr)
			}
			for _, want := range tt.wantInErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			if !strings.Contains(note, tt.wantNote) {
				t.Errorf("note %q does not contain %q", note, tt.wantNote)
			}
		})
	}
}

// Criterion 2: a pending file under fixtures/ makes the test fail, and the
// failure names it.
func TestAPendingFileUnderFixturesFailsAndIsNamed(t *testing.T) {
	pendingFx := plantInto(t, asPending(inArgv("rm", "ddm-run")))
	root := writeTree(t, map[string][]byte{
		"fixtures/lifecycle/rm-pending--docker-29.10.1-rootful.cli": pendingFx,
		"fixtures/lifecycle/rm-done--docker-29.10.1-rootful.cli":    syntheticFile(t, fileRmDocker),
		"fixtures/lifecycle/README.md":                              []byte("# record: pending\n\n"), // not a .cli file
	})
	found := pendingIn(t, filepath.Join(root, "fixtures", "*", "*.cli"))
	want := filepath.Join(root, "fixtures", "lifecycle", "rm-pending--docker-29.10.1-rootful.cli")
	if !slices.Equal(found, []string{want}) {
		t.Fatalf("pendingIn = %v, want only %s", found, want)
	}

	// The file is what the recorder writes: a valid pending fixture.
	parsed, err := enginefixture.Parse("lifecycle", "rm-pending--docker-29.10.1-rootful.cli", pendingFx)
	if err != nil || parsed.Meta.Record != enginefixture.RecordPending {
		t.Fatalf("the planted file is not a pending fixture: %v", err)
	}

	_, err = recordingVerdict(envOf(), found)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("verdict error = %v, want one that names %s", err, want)
	}
	// And a loader refuses it too, naming it: nothing replays a request without answer.
	if _, err := enginefixture.LoadFS(os.DirFS(filepath.Join(root, "fixtures")), "lifecycle"); err == nil ||
		!strings.Contains(err.Error(), "rm-pending--docker-29.10.1-rootful.cli") || !strings.Contains(strings.ToLower(err.Error()), "pending") {
		t.Errorf("LoadFS error = %v, want one that names the pending file and says it is pending", err)
	}
}

// TestNoPendingFixtureIsVersioned is the guard on the tree: while a pending file
// exists its campaign is out of the replay, stale and round-trip checks, so the
// suite would stay green on files nothing verifies. Only a local recording
// session (DEVDESK_FIXTURE_RECORDING=1, never in CI) may hold one.
func TestNoPendingFixtureIsVersioned(t *testing.T) {
	pending := pendingIn(t, filepath.Join("fixtures", "*", "*.cli"))
	note, err := recordingVerdict(os.Getenv, pending)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(note)

	// The same rule holds for the synthetic files: none of them is pending.
	if p := pendingIn(t, filepath.Join("testdata", "*", "*.cli")); len(p) != 0 {
		t.Errorf("pending file(s) in testdata: %v: a synthetic fixture has an answer", p)
	}
}

// The variable that opens a recording session is set by hand, in a local shell.
// Neither the mise tasks nor a workflow sets it: it would turn the guard off.

var errMentions = errors.New("mentions the recording session variable")

// mentionsRecording reports the (1-based) lines of text that name the variable,
// outside the sections of mise.toml that belong to a recorder.
func mentionsRecording(text string, recorderSections ...string) []int {
	var lines []int
	inRecorder := false
	for i, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "[") {
			inRecorder = false
			for _, s := range recorderSections {
				inRecorder = inRecorder || strings.Contains(line, s)
			}
		}
		if !inRecorder && strings.Contains(line, recordingSessionEnv) {
			lines = append(lines, i+1)
		}
	}
	return lines
}

func TestMentionsRecordingFindsTheVariable(t *testing.T) {
	text := "[env]\nA = 1\n[tasks.test]\nrun = \"DEVDESK_FIXTURE_RECORDING=1 go test\"\n[tasks.\"engine-contract:record\"]\nenv = { DEVDESK_FIXTURE_RECORDING = \"1\" }\n"
	if got := mentionsRecording(text); !slices.Equal(got, []int{4, 6}) {
		t.Errorf("without an exception, lines = %v, want [4 6]", got)
	}
	if got := mentionsRecording(text, `"engine-contract:record"`); !slices.Equal(got, []int{4}) {
		t.Errorf("with the recorder section allowed, lines = %v, want [4]", got)
	}
	if got := mentionsRecording("[tasks.ci]\nrun = []\n"); len(got) != 0 {
		t.Errorf("lines = %v, want none", got)
	}
}

func TestRecordingSessionIsNeverSetByTheStandardChecks(t *testing.T) {
	root := filepath.Join("..", "..")

	mise, err := os.ReadFile(filepath.Join(root, "mise.toml"))
	if err != nil || !bytes.Contains(mise, []byte("[tasks.ci]")) {
		t.Fatalf("mise.toml is missing or is not the one of the project (the checks would pass on nothing): %v", err)
	}
	// ci, check, test and test-race, and the global [env], are the standard
	// checks; only a recorder task may name the variable.
	recorders := []string{`"engine-contract:record"`, `"nexus-bench:record"`}
	for _, line := range mentionsRecording(string(mise), recorders...) {
		t.Errorf("mise.toml:%d: %v outside a recorder task: the standard checks must never open a recording session", line, errMentions)
	}

	workflows, err := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(workflows) == 0 {
		t.Fatal("no workflow found: the check would pass on nothing")
	}
	for _, w := range workflows {
		data, err := os.ReadFile(w)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range mentionsRecording(string(data)) {
			t.Errorf("%s:%d: %v: a workflow must never open a recording session", filepath.ToSlash(w), line, errMentions)
		}
	}
}
