package testutil

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// DEVDESK-342: `mise run ci` replays the CI workflow locally. It must chain,
// sequentially and in order, the `mise run <task>` steps of ci.yml plus the
// local-only steps below (D149), so the two cannot drift apart: ci.yml is the
// steps of `mise run ci` minus localOnlySteps. mise.toml is read without a
// TOML library (none is in go.mod): the guard only needs the task headers,
// the description and the `run = [{ task = "..." }, ...]` array of [tasks.ci].
//
// DEVDESK-502 (D149 in docs/backlog.md): the CI no longer runs the tests, so
// ci.yml is `mise run ci` minus an explicit local-only set. The relation is
// checked in both directions: a step missing from ci.yml fails, and so does a
// local-only task that comes back into ci.yml, so tests return to the CI only
// by a decision that edits this file.
//
// DEVDESK-648: `mise run ci` runs the tests once, with the race detector
// (`test-race`, which already covers what `go test` does). The plain `test`
// task still exists for the fast local loop but is no longer a step of `ci`.

var ciSteps = []string{"fmt-check", "vet", "lint", "build", "test-race"}

// localOnlySteps are the steps of `mise run ci` that ci.yml must NOT run.
// D149: the `Test` job of ci.yml only writes a summary line (the main-protection
// ruleset requires a check named `Test`), and the tests run on the maintainer's
// machine through `mise run ci`. Since DEVDESK-648 the only test step of `ci`
// is `test-race`; `test` is no longer a step of `ci`, so it is not listed here.
// DEVDESK-649 (S-05) puts `test-race` in the CI: it then leaves this set, and
// the set becomes empty.
var localOnlySteps = []string{"test-race"}

// workflowSteps returns steps without the local-only ones, order preserved.
func workflowSteps(steps []string) []string {
	var out []string
	for _, s := range steps {
		if !slices.Contains(localOnlySteps, s) {
			out = append(out, s)
		}
	}
	return out
}

var (
	taskHeaderRe = regexp.MustCompile(`(?m)^\[tasks\.([A-Za-z0-9_.-]+)\]\s*$`)
	sectionRe    = regexp.MustCompile(`(?m)^\[`)
	stepRe       = regexp.MustCompile(`task\s*=\s*"([^"]+)"`)
)

// miseSection returns the body of [tasks.<name>] and the set of all task names.
func miseSection(t *testing.T, name string) (body string, all map[string]bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), "mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	all = map[string]bool{}
	for _, m := range taskHeaderRe.FindAllStringSubmatch(src, -1) {
		all[m[1]] = true
	}
	loc := regexp.MustCompile(`(?m)^\[tasks\.` + regexp.QuoteMeta(name) + `\]\s*$`).FindStringIndex(src)
	if loc == nil {
		return "", all
	}
	rest := src[loc[1]:]
	if next := sectionRe.FindStringIndex(rest); next != nil {
		rest = rest[:next[0]]
	}
	return rest, all
}

func ciTaskBody(t *testing.T) (string, map[string]bool) {
	t.Helper()
	body, all := miseSection(t, "ci")
	if body == "" {
		t.Fatal("mise.toml has no [tasks.ci] section (DEVDESK-342)")
	}
	return body, all
}

func ciRunSteps(t *testing.T, body string) []string {
	t.Helper()
	var steps []string
	for _, m := range stepRe.FindAllStringSubmatch(body, -1) {
		steps = append(steps, m[1])
	}
	return steps
}

func TestMiseCIHasDescriptionMentioningCIAndCheck(t *testing.T) {
	body, _ := ciTaskBody(t)
	m := regexp.MustCompile(`(?m)^description\s*=\s*"((?:[^"\\]|\\.)*)"`).FindStringSubmatch(body)
	if m == nil || strings.TrimSpace(m[1]) == "" {
		t.Fatal("[tasks.ci] needs a non-empty description")
	}
	d := strings.ToLower(m[1])
	if !strings.Contains(d, "ci") {
		t.Errorf("description %q must say it reproduces the CI", m[1])
	}
	if !strings.Contains(d, "check") {
		t.Errorf("description %q must say how it differs from `check`", m[1])
	}
}

func TestMiseCIRunsStepsSequentiallyInOrder(t *testing.T) {
	body, _ := ciTaskBody(t)
	if regexp.MustCompile(`(?m)^depends\w*\s*=`).MatchString(body) {
		t.Error("[tasks.ci] must not use depends/depends_post: mise runs those in parallel")
	}
	got := ciRunSteps(t, body)
	if strings.Join(got, ",") != strings.Join(ciSteps, ",") {
		t.Errorf("[tasks.ci] run steps = %v, want %v", got, ciSteps)
	}
}

func TestMiseCIMatchesWorkflowSteps(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatal(err)
	}
	// Job order is not preserved by a map: walk the two jobs in file order.
	var order yaml.Node
	if err := yaml.Unmarshal(raw, &order); err != nil {
		t.Fatal(err)
	}
	var jobNames []string
	for i := 0; i+1 < len(order.Content[0].Content); i += 2 {
		if order.Content[0].Content[i].Value != "jobs" {
			continue
		}
		jobs := order.Content[0].Content[i+1]
		for j := 0; j+1 < len(jobs.Content); j += 2 {
			jobNames = append(jobNames, jobs.Content[j].Value)
		}
	}
	var fromCI []string
	for _, name := range jobNames {
		for _, s := range wf.Jobs[name].Steps {
			if task, ok := strings.CutPrefix(strings.TrimSpace(s.Run), "mise run "); ok {
				fromCI = append(fromCI, strings.TrimSpace(task))
			}
		}
	}
	body, _ := ciTaskBody(t)
	miseSteps := ciRunSteps(t, body)
	for _, s := range fromCI {
		if slices.Contains(localOnlySteps, s) {
			t.Errorf("ci.yml runs `mise run %s`, which is local-only (D149: tests are not run in CI; "+
				"bringing them back needs an explicit decision and an edit of localOnlySteps)", s)
		}
	}
	for _, s := range localOnlySteps {
		if !slices.Contains(miseSteps, s) {
			t.Errorf("localOnlySteps lists %q, which [tasks.ci] does not run: drop it from the set", s)
		}
	}
	want := workflowSteps(miseSteps)
	if strings.Join(fromCI, ",") != strings.Join(want, ",") {
		t.Errorf("ci.yml `mise run` steps = %v, want %v: the steps of [tasks.ci] (%v) minus the local-only %v, in the same order",
			fromCI, want, miseSteps, localOnlySteps)
	}
}

// DEVDESK-648: the plain `test` task stays defined (fast local loop, and the
// only test task usable without a C compiler) but `ci` must not call it, so the
// suite is not run twice.
func TestMiseTestTaskExistsButCIDoesNotCallIt(t *testing.T) {
	body, all := ciTaskBody(t)
	if !all["test"] {
		t.Error("mise.toml must still define the `test` task (fast local loop without -race)")
	}
	if !all["test-race"] {
		t.Error("mise.toml must define the `test-race` task that `ci` runs")
	}
	steps := ciRunSteps(t, body)
	if slices.Contains(steps, "test") {
		t.Errorf("[tasks.ci] runs `test` (steps = %v): tests run once, through `test-race`", steps)
	}
	// A test step is `test` or any `test-*` task; exactly one is allowed and it
	// is `test-race`.
	var testSteps []string
	for _, s := range steps {
		if s == "test" || strings.HasPrefix(s, "test-") {
			testSteps = append(testSteps, s)
		}
	}
	if !slices.Equal(testSteps, []string{"test-race"}) {
		t.Errorf("[tasks.ci] test steps = %v (steps = %v), want exactly [test-race]", testSteps, steps)
	}
}

func TestMiseCIStepsAreDefinedTasks(t *testing.T) {
	body, all := ciTaskBody(t)
	for _, s := range ciRunSteps(t, body) {
		if !all[s] {
			t.Errorf("[tasks.ci] runs %q, which mise.toml does not define", s)
		}
	}
}
