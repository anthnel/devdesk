package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// DEVDESK-342: `mise run ci` replays the CI workflow locally. It chains,
// sequentially and in order, the `mise run <task>` steps of ci.yml, so the two
// cannot drift apart: ci.yml runs exactly the steps of `mise run ci` minus
// localOnlySteps, which is empty. The relation is checked in both directions,
// so a step missing from ci.yml fails, and so does a local-only task that
// ci.yml runs. mise.toml is read without a TOML library (none is in go.mod):
// the guard only needs the task headers, the description and the
// `run = [{ task = "..." }, ...]` array of [tasks.ci].
//
// DEVDESK-648: `mise run ci` runs the tests once, with the race detector
// (`test-race`, which already covers what `go test` does). The plain `test`
// task still exists for the fast local loop but is no longer a step of `ci`.
//
// DEVDESK-649: CI runs the tests under `-race` on every pull request. The job
// named `Test` runs `mise run test-race`, including on a docs-only pull
// request: the docs-only shortcut is reserved for `Lint and build`. The job has
// a 15 minute timeout, no `needs`, installs only Go through mise-action, uses no
// secret, restores the Go build and module caches with actions/cache/restore on
// every run and saves them with actions/cache/save only on a push to main. The
// TestCITestJob* tests below read ci.yml as YAML and check each of these points.

var ciSteps = []string{"fmt-check", "vet", "lint", "build", "test-race"}

// localOnlySteps are the steps of `mise run ci` that ci.yml must NOT run. It is
// empty since DEVDESK-649: every step of `mise run ci` runs in CI. A task added
// here would be a step the CI skips, which needs an explicit decision.
var localOnlySteps []string

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
	var fromCI []string
	for _, job := range loadWorkflowJobs(t) {
		for _, s := range job.Steps {
			if task, ok := strings.CutPrefix(strings.TrimSpace(s.Run), "mise run "); ok {
				fromCI = append(fromCI, strings.TrimSpace(task))
			}
		}
	}
	body, _ := ciTaskBody(t)
	miseSteps := ciRunSteps(t, body)
	for _, s := range fromCI {
		if slices.Contains(localOnlySteps, s) {
			t.Errorf("ci.yml runs `mise run %s`, which is listed in localOnlySteps: drop it from the set", s)
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

// DEVDESK-649: since the tests run in CI, no step of `mise run ci` is local-only.
func TestNoMiseCIStepIsLocalOnly(t *testing.T) {
	if len(localOnlySteps) != 0 {
		t.Errorf("localOnlySteps = %v, want it empty: ci.yml runs every step of `mise run ci`", localOnlySteps)
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

// workflowStep is the part of a GitHub Actions step the DEVDESK-649 guard reads.
type workflowStep struct {
	ID              string         `yaml:"id"`
	If              string         `yaml:"if"`
	Uses            string         `yaml:"uses"`
	Run             string         `yaml:"run"`
	With            map[string]any `yaml:"with"`
	Env             map[string]any `yaml:"env"`
	ContinueOnError any            `yaml:"continue-on-error"`
}

// workflowJob is the part of a GitHub Actions job the guard reads. Needs is
// untyped on purpose: `needs` is either a string or a list, and the guard only
// asks whether it is present.
type workflowJob struct {
	Key            string `yaml:"-"`
	Name           string `yaml:"name"`
	If             string `yaml:"if"`
	Needs          any    `yaml:"needs"`
	TimeoutMinutes int    `yaml:"timeout-minutes"`
	Steps          []workflowStep
	Node           *yaml.Node `yaml:"-"`
}

const (
	lintJobName = "Lint and build"
	testJobName = "Test"
)

func readWorkflow(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(moduleRoot(t), ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// loadWorkflowJobs returns the jobs of ci.yml in file order (a Go map would lose
// it, and the order matters: `mise run ci` mirrors it).
func loadWorkflowJobs(t *testing.T) []workflowJob {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(readWorkflow(t), &doc); err != nil {
		t.Fatal(err)
	}
	var jobs []workflowJob
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "jobs" {
			continue
		}
		set := root.Content[i+1]
		for j := 0; j+1 < len(set.Content); j += 2 {
			var job workflowJob
			if err := set.Content[j+1].Decode(&job); err != nil {
				t.Fatalf("ci.yml job %q: %v", set.Content[j].Value, err)
			}
			job.Key = set.Content[j].Value
			job.Node = set.Content[j+1]
			jobs = append(jobs, job)
		}
	}
	return jobs
}

// testJob returns the job whose display name is exactly `Test`: the name, not
// the key, is what the main-protection ruleset requires.
func testJob(t *testing.T) workflowJob {
	t.Helper()
	var found []workflowJob
	for _, j := range loadWorkflowJobs(t) {
		if j.Name == testJobName {
			found = append(found, j)
		}
	}
	if len(found) != 1 {
		t.Fatalf("ci.yml has %d jobs named %q, want exactly 1 (the main-protection ruleset requires that check)", len(found), testJobName)
	}
	return found[0]
}

// stepUsing returns the index of the first step using the action (any version)
// or -1.
func stepUsing(job workflowJob, action string) int {
	return slices.IndexFunc(job.Steps, func(s workflowStep) bool {
		return strings.HasPrefix(s.Uses, action+"@")
	})
}

// testRaceStep returns the index of the step written exactly `mise run
// test-race`, or -1.
func testRaceStep(job workflowJob) int {
	return slices.IndexFunc(job.Steps, func(s workflowStep) bool {
		return strings.TrimSpace(s.Run) == "mise run test-race"
	})
}

func withString(s workflowStep, key string) string {
	if v, ok := s.With[key]; ok {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

// requireTestRaceStep fails the test when the Test job does not run the
// tests, and says what it runs instead.
func requireTestRaceStep(t *testing.T, job workflowJob) int {
	t.Helper()
	i := testRaceStep(job)
	if i >= 0 {
		return i
	}
	var runs []string
	for _, s := range job.Steps {
		if s.Run != "" {
			runs = append(runs, s.Run)
		}
	}
	t.Fatalf("job %q has no step written exactly `run: mise run test-race`; its run steps are %q "+
		"(an `echo` stub, a different task or a wrapper does not count)", testJobName, runs)
	return -1
}

func scalarsOf(n *yaml.Node, visit func(string)) {
	if n.Kind == yaml.ScalarNode {
		visit(n.Value)
	}
	for _, c := range n.Content {
		scalarsOf(c, visit)
	}
}

// DEVDESK-649 criterion 6: the two job names the main-protection ruleset
// requires are unchanged, and `Test` comes after `Lint and build` in the file
// (`mise run ci` runs the steps in file order, test-race last).
func TestCIJobNamesAndOrderAreUnchanged(t *testing.T) {
	var names []string
	for _, j := range loadWorkflowJobs(t) {
		names = append(names, j.Name)
	}
	if want := []string{lintJobName, testJobName}; !slices.Equal(names, want) {
		t.Errorf("ci.yml job names in file order = %q, want %q", names, want)
	}
}

// DEVDESK-649 criterion 1: the job named Test runs the tests, written exactly
// `mise run test-race`, as a plain step.
func TestCITestJobRunsMiseTestRace(t *testing.T) {
	job := testJob(t)
	step := job.Steps[requireTestRaceStep(t, job)]
	if step.ContinueOnError != nil && fmt.Sprint(step.ContinueOnError) != "false" {
		t.Errorf("the test-race step sets continue-on-error: %v, a failing test must turn the check red", step.ContinueOnError)
	}
}

// DEVDESK-649 criterion 3: the Test job runs on every pull request, docs-only
// included. Neither the job nor any step up to and including test-race may be
// conditional, and nothing in the job may consult the docs-only classifier
// (that shortcut belongs to `Lint and build`).
func TestCITestJobIsNeverSkipped(t *testing.T) {
	job := testJob(t)
	i := requireTestRaceStep(t, job)
	if job.If != "" {
		t.Errorf("job %q has `if: %s`: it must run on every pull request", testJobName, job.If)
	}
	for k, s := range job.Steps[:i+1] {
		if s.If != "" {
			t.Errorf("step %d of job %q (%s%s) has `if: %s`: nothing up to the test-race step may be skippable",
				k, testJobName, s.Uses, s.Run, s.If)
		}
	}
	scalarsOf(job.Node, func(v string) {
		if strings.Contains(v, "docs_only") || strings.Contains(v, "docs-only") {
			t.Errorf("job %q mentions the docs-only classifier (%q): only %q may short-circuit", testJobName, v, lintJobName)
		}
	})
}

// DEVDESK-649 criterion 6: explicit 15 minute timeout, no `needs`, Go only,
// no secret.
func TestCITestJobTimeoutIs15Minutes(t *testing.T) {
	if got := testJob(t).TimeoutMinutes; got != 15 {
		t.Errorf("job %q timeout-minutes = %d, want 15: a stuck test must turn the check red, not run forever", testJobName, got)
	}
}

func TestCITestJobHasNoNeeds(t *testing.T) {
	if job := testJob(t); job.Needs != nil {
		t.Errorf("job %q has `needs: %v`: it must run in parallel with %q", testJobName, job.Needs, lintJobName)
	}
}

// kubeconform is the one other tool: TestARealKustomizeIsNotStartedOnAPluginDirectory
// skips without it (DEVDESK-771 follow-up).
func TestCITestJobInstallsOnlyGo(t *testing.T) {
	job := testJob(t)
	i := stepUsing(job, "jdx/mise-action")
	if i < 0 {
		t.Fatalf("job %q has no jdx/mise-action step: mise is what runs `mise run test-race`", testJobName)
	}
	if got := withString(job.Steps[i], "install_args"); got != "go kubeconform" {
		t.Errorf("job %q mise-action install_args = %q, want \"go kubeconform\": no golangci-lint, goreleaser or uv in the test job", testJobName, got)
	}
}

func TestCITestJobUsesNoSecret(t *testing.T) {
	scalarsOf(testJob(t).Node, func(v string) {
		if v == "secrets" || strings.Contains(v, "secrets.") {
			t.Errorf("job %q references a secret (%q): the test job receives none", testJobName, v)
		}
	})
}

// DEVDESK-649 criterion 4: the Go caches come from actions/cache (never
// mise-action or setup-go). Pull requests only restore, with a key made of the
// OS, the resolved Go version, go.sum and the word race; only a push to main
// saves, under the same key and path.
func TestCITestJobRestoresAndSavesTheGoCaches(t *testing.T) {
	job := testJob(t)
	if i := stepUsing(job, "actions/cache"); i >= 0 {
		t.Errorf("step %d uses actions/cache@: it saves on pull requests too; use actions/cache/restore and actions/cache/save", i)
	}
	if i := stepUsing(job, "actions/setup-go"); i >= 0 {
		t.Errorf("step %d uses actions/setup-go: it would install a second Go next to mise's", i)
	}
	ri := stepUsing(job, "actions/cache/restore")
	si := stepUsing(job, "actions/cache/save")
	if ri < 0 || si < 0 {
		t.Fatalf("job %q needs an actions/cache/restore step (found: %v) and an actions/cache/save step (found: %v)",
			testJobName, ri >= 0, si >= 0)
	}
	restore, save := job.Steps[ri], job.Steps[si]

	key := withString(restore, "key")
	for _, part := range []string{"runner.os", "go.sum", "race"} {
		if !strings.Contains(key, part) {
			t.Errorf("restore key %q does not contain %q", key, part)
		}
	}
	checkGoVersionInKey(t, job, ri, key)
	path := strings.ToLower(withString(restore, "path"))
	for _, part := range []string{"gocache", "gomodcache"} {
		if !strings.Contains(path, part) {
			t.Errorf("restore path %q does not name %s (read both with `go env GOCACHE GOMODCACHE`)", path, part)
		}
	}
	if restore.If != "" {
		t.Errorf("restore step has `if: %s`: pull requests and pushes all restore", restore.If)
	}

	if withString(save, "key") != key || withString(save, "path") != withString(restore, "path") {
		t.Errorf("save step key/path (%q, %q) must equal the restore step's (%q, %q)",
			withString(save, "key"), withString(save, "path"), key, withString(restore, "path"))
	}
	if !strings.Contains(save.If, "refs/heads/main") || !strings.Contains(save.If, "==") {
		t.Errorf("save step `if` = %q, want a condition on github.ref == 'refs/heads/main' (save only on a push to main)", save.If)
	}
	if ti := testRaceStep(job); ri >= ti || ti >= si {
		t.Errorf("step order: restore=%d test-race=%d save=%d, want restore < test-race < save", ri, ti, si)
	}
}

// checkGoVersionInKey requires the key to carry the resolved Go version: an
// expression on the output of an earlier step that reads it from the Go mise
// installed (a literal version would go stale the day mise.toml moves).
func checkGoVersionInKey(t *testing.T, job workflowJob, restoreAt int, key string) {
	t.Helper()
	m := regexp.MustCompile(`steps\.([A-Za-z0-9_-]+)\.outputs\.`).FindStringSubmatch(key)
	if m == nil {
		t.Errorf("restore key %q has no `steps.<id>.outputs.*` expression: it must carry the resolved Go version", key)
		return
	}
	from := slices.IndexFunc(job.Steps, func(s workflowStep) bool { return s.ID == m[1] })
	if from < 0 || from >= restoreAt {
		t.Errorf("restore key reads the output of step %q, which is not an earlier step of job %q", m[1], testJobName)
		return
	}
	if !regexp.MustCompile(`(?i)goversion|go version|mise (current|ls)`).MatchString(job.Steps[from].Run) {
		t.Errorf("step %q feeds the key but does not read the Go version (GOVERSION, `go version`, `mise current`): run = %q",
			m[1], job.Steps[from].Run)
	}
}

// DEVDESK-649 hardening: the checkout keeps no token in .git (the job pushes
// nothing), and the tests never report "(cached)" results from a warm cache.
func TestCITestJobCheckoutDropsCredentialsAndTestsBypassTheResultCache(t *testing.T) {
	job := testJob(t)
	if i := stepUsing(job, "actions/checkout"); i < 0 {
		t.Error("job has no actions/checkout step")
	} else if got := withString(job.Steps[i], "persist-credentials"); got != "false" {
		t.Errorf("checkout persist-credentials = %q, want false", got)
	}
	step := job.Steps[requireTestRaceStep(t, job)]
	if got := strings.TrimSpace(fmt.Sprint(step.Env["GOFLAGS"])); got != "-count=1" {
		t.Errorf("test-race step env GOFLAGS = %q, want -count=1: a cached test result must not hide a failure", got)
	}
}

// DEVDESK-649: checkout, then mise, then the cache, then the tests.
func TestCITestJobStepsRunInOrder(t *testing.T) {
	job := testJob(t)
	order := []struct {
		name string
		at   int
	}{
		{"actions/checkout", stepUsing(job, "actions/checkout")},
		{"jdx/mise-action", stepUsing(job, "jdx/mise-action")},
		{"actions/cache/restore", stepUsing(job, "actions/cache/restore")},
		{"mise run test-race", testRaceStep(job)},
		{"actions/cache/save", stepUsing(job, "actions/cache/save")},
	}
	prev := -1
	for _, o := range order {
		if o.at < 0 {
			t.Errorf("job %q has no %s step", testJobName, o.name)
			continue
		}
		if o.at < prev {
			t.Errorf("job %q runs %s (step %d) before an earlier stage: want checkout, mise, cache restore, test-race, cache save",
				testJobName, o.name, o.at)
		}
		prev = o.at
	}
}

// DEVDESK-649 criterion 7: ci.yml no longer carries the comments that explained
// the stub Test job.
func TestCIWorkflowNoLongerClaimsTestsAreDisabled(t *testing.T) {
	raw := strings.ToLower(string(readWorkflow(t)))
	for _, stale := range []string{"d149", "tests are disabled", "tests are not run", "runs no tests"} {
		if strings.Contains(raw, stale) {
			t.Errorf("ci.yml still says %q: the Test job runs `mise run test-race` on every pull request", stale)
		}
	}
}
