package nexusbench

// Guard of the mise tasks of the bench (DEVDESK-504, step 8, CA1 and CA7). It
// reads mise.toml at the repository root and runs nothing: the ten
// nexus-bench:* tasks (NX-02a, DEVDESK-505, and NX-02b, DEVDESK-534) must exist, call the command, stay out of every task the
// standard checks run (ci, check, test, test-race), and kubectl must be
// declared where it does not slow the CI down.
//
// go.mod carries no TOML library and none may be added for a test, so
// mise.toml is read by a small line-based reader that understands what the
// file uses: [tables] and [tasks."quoted:name"] headers, key = value, values
// spread over several lines (arrays, inline tables, ''' and """ strings) and
// comments.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const miseBenchPrefix = "nexus-bench"

// seed is the task NX-02a adds (DEVDESK-544); cut-upstream, restore-upstream and
// reset are NX-02b (DEVDESK-534, step 7, DEVDESK-554). The messages of seed.go
// already quote `mise run nexus-bench:restore-upstream` and
// `mise run nexus-bench:reset`, so those two names are fixed.
var miseBenchSubcommands = []string{"up", "provision", "down", "seed", "status", "render", "cut-upstream", "restore-upstream", "reset"}

// miseFile maps a table path ("tasks|build", "tools") to its keys, each with
// the raw text of its value.
type miseFile map[string]map[string]string

// miseRepoRoot walks up from the working directory to the folder holding go.mod.
func miseRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the working directory")
		}
		dir = parent
	}
}

func loadMise(t *testing.T) miseFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(miseRepoRoot(t), "mise.toml"))
	if err != nil {
		t.Fatalf("read mise.toml: %v", err)
	}
	return parseMise(string(data))
}

// miseStripComment removes a trailing "# ..." that is outside a string.
func miseStripComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return line[:i]
		}
	}
	return line
}

// miseDepth is how many [ and { are still open after line, outside strings.
func miseDepth(line string) int {
	depth := 0
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
		}
	}
	return depth
}

// miseHeader splits "[tasks.\"nexus-bench:up\".tools]" into its parts.
func miseHeader(line string) []string {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
	var parts []string
	var cur strings.Builder
	var quote byte
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '.':
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	return append(parts, strings.TrimSpace(cur.String()))
}

func parseMise(text string) miseFile {
	file := miseFile{}
	section := ""
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[[") && strings.HasSuffix(miseStripComment(trimmed), "]") && !strings.Contains(trimmed, "=") {
			section = strings.Join(miseHeader(strings.TrimSpace(miseStripComment(trimmed))), "|")
			if file[section] == nil {
				file[section] = map[string]string{}
			}
			continue
		}
		key, value, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		key = strings.Trim(strings.TrimSpace(key), `"'`)
		value = strings.TrimSpace(value)
		raw := value
		switch {
		case strings.HasPrefix(value, `"""`) || strings.HasPrefix(value, `'''`):
			delim := value[:3]
			for !strings.Contains(raw[3:], delim) && i+1 < len(lines) {
				i++
				raw += "\n" + lines[i]
			}
		default:
			raw = strings.TrimSpace(miseStripComment(value))
			depth := miseDepth(raw)
			for depth > 0 && i+1 < len(lines) {
				i++
				more := miseStripComment(lines[i])
				raw += "\n" + more
				depth += miseDepth(more)
			}
		}
		if file[section] == nil {
			file[section] = map[string]string{}
		}
		file[section][key] = raw
	}
	return file
}

// task returns the keys of a task, merging its sub-tables ([tasks."x".tools])
// under the name of the sub-table.
func (f miseFile) task(name string) (map[string]string, bool) {
	base := "tasks|" + name
	keys, found := f[base]
	merged := map[string]string{}
	for k, v := range keys {
		merged[k] = v
	}
	for section, sub := range f {
		if !strings.HasPrefix(section, base+"|") {
			continue
		}
		var lines []string
		for k, v := range sub {
			lines = append(lines, k+" = "+v)
		}
		sort.Strings(lines)
		merged[strings.TrimPrefix(section, base+"|")] = strings.Join(lines, "\n")
		found = true
	}
	return merged, found
}

var (
	miseQuoted  = regexp.MustCompile(`"([^"]*)"|'([^']*)'`)
	miseTaskRef = regexp.MustCompile(`task\s*=\s*["']([^"']+)["']`)
)

// miseRefs lists the tasks a task depends on or runs: the strings of
// depends, depends_post and wait_for, and every `{ task = "x" }` of run.
func miseRefs(keys map[string]string) []string {
	var refs []string
	for _, k := range []string{"depends", "depends_post", "wait_for"} {
		for _, m := range miseQuoted.FindAllStringSubmatch(keys[k], -1) {
			refs = append(refs, m[1]+m[2])
		}
	}
	for _, m := range miseTaskRef.FindAllStringSubmatch(keys["run"], -1) {
		refs = append(refs, m[1])
	}
	return refs
}

func TestMiseReaderUnderstandsTheFile(t *testing.T) {
	// A reader that silently misses a table would make the guards below pass
	// vacuously, so the file is checked against what it is known to contain.
	f := loadMise(t)
	for _, name := range []string{"build", "test", "test-race", "check", "ci", "lint"} {
		if _, ok := f.task(name); !ok {
			t.Errorf("task %q was not read from mise.toml", name)
		}
	}
	if ci, _ := f.task("ci"); !strings.Contains(ci["run"], `"test-race"`) {
		t.Errorf("the run array of ci was not read in full: %q", ci["run"])
	}
	if check, _ := f.task("check"); !strings.Contains(check["depends"], `"lint"`) {
		t.Errorf("depends of check was not read: %q", check["depends"])
	}
	if _, ok := f["tools"]["go"]; !ok {
		t.Error("the [tools] table was not read")
	}
}

func TestMiseReaderOnASample(t *testing.T) {
	f := parseMise(`
[tools]
go = "1.27" # trailing comment

[tasks."nexus-bench:up"]
description = "x # not a comment"
tools = { kubectl = "1.33" }
run = '''
go run ./a "quoted" up
'''

[tasks."nexus-bench:test".env]
DEVDESK_NEXUS_BENCH = "1"

[tasks.ci]
run = [
  # a comment
  { task = "fmt-check" },
  { task = "vet" },
]
`)
	if f["tools"]["go"] != `"1.27"` {
		t.Errorf("tools.go = %q", f["tools"]["go"])
	}
	up, ok := f.task("nexus-bench:up")
	if !ok || !strings.Contains(up["tools"], "kubectl") || !strings.Contains(up["run"], "go run ./a") || up["description"] != `"x # not a comment"` {
		t.Errorf("nexus-bench:up misread: %v", up)
	}
	test, _ := f.task("nexus-bench:test")
	if !strings.Contains(test["env"], "DEVDESK_NEXUS_BENCH") {
		t.Errorf("the env sub-table was not merged: %v", test)
	}
	ci, _ := f.task("ci")
	if got := miseRefs(ci); strings.Join(got, ",") != "fmt-check,vet" {
		t.Errorf("refs of ci = %v", got)
	}
}

func TestMiseDeclaresTheTenBenchTasks(t *testing.T) {
	f := loadMise(t)
	names := []string{}
	for _, sub := range miseBenchSubcommands {
		names = append(names, miseBenchPrefix+":"+sub)
	}
	names = append(names, miseBenchPrefix+":test")
	if len(names) != 10 {
		t.Fatalf("the guard expects 10 bench tasks (up, provision, down, seed, status, render, cut-upstream, restore-upstream, reset, test), it lists %d: %v", len(names), names)
	}
	for _, name := range names {
		if _, ok := f.task(name); !ok {
			t.Errorf("mise.toml has no task %q", name)
		}
	}
}

func TestMiseBenchTasksCallTheCommand(t *testing.T) {
	f := loadMise(t)
	for _, sub := range miseBenchSubcommands {
		name := miseBenchPrefix + ":" + sub
		t.Run(name, func(t *testing.T) {
			task, ok := f.task(name)
			if !ok {
				t.Fatalf("mise.toml has no task %q", name)
			}
			re := regexp.MustCompile(`go run \./internal/nexusbench/cmd/nexus-bench ` + sub + `(["'\s]|$)`)
			if !re.MatchString(task["run"]) {
				t.Errorf("run of %s does not call `go run ./internal/nexusbench/cmd/nexus-bench %s`:\n%s", name, sub, task["run"])
			}
			if strings.TrimSpace(task["description"]) == "" {
				t.Errorf("%s has no description, so `mise tasks` lists it blank", name)
			}
		})
	}
}

func TestMiseBenchTestTaskRunsTheIntegrationTestsGuarded(t *testing.T) {
	name := miseBenchPrefix + ":test"
	task, ok := loadMise(t).task(name)
	if !ok {
		t.Fatalf("mise.toml has no task %q", name)
	}
	if !strings.Contains(task["run"], "-count=1 -run Integration ./internal/nexusbench/...") || !strings.HasPrefix(strings.TrimSpace(task["run"]), `"go test `) {
		t.Errorf("run of %s must be `go test -timeout 60m -count=1 -run Integration ./internal/nexusbench/...`:\n%s", name, task["run"])
	}
	// go test stops at 10 minutes by default, and the lifecycle test refuses to
	// start with less than lifecycleMinTimeout left.
	m := regexp.MustCompile(`-timeout[ =](\S+)`).FindStringSubmatch(task["run"])
	if m == nil {
		t.Fatalf("run of %s has no -timeout: go test would stop the lifecycle after 10 minutes:\n%s", name, task["run"])
	}
	d, err := time.ParseDuration(strings.Trim(m[1], `"'`))
	if err != nil {
		t.Fatalf("-timeout %q of %s is not a duration: %v", m[1], name, err)
	}
	if d < lifecycleMinTimeout {
		t.Errorf("-timeout %s of %s is below the %s the lifecycle test needs", d, name, lifecycleMinTimeout)
	}
	// The guard variable is set either by `env DEVDESK_NEXUS_BENCH=1` in the
	// command or by the env of the task.
	if !strings.Contains(task["run"], "DEVDESK_NEXUS_BENCH=1") && !strings.Contains(task["env"], "DEVDESK_NEXUS_BENCH") {
		t.Errorf("%s does not set DEVDESK_NEXUS_BENCH=1", name)
	}
}

// No bench task is part of, or reachable from, a task the standard checks run.
func TestMiseNoStandardTaskReachesTheBench(t *testing.T) {
	f := loadMise(t)
	for _, root := range []string{"ci", "check", "test", "test-race"} {
		t.Run(root, func(t *testing.T) {
			if _, ok := f.task(root); !ok {
				t.Fatalf("mise.toml has no task %q", root)
			}
			seen := map[string]bool{}
			queue := []string{root}
			for len(queue) > 0 {
				name := queue[0]
				queue = queue[1:]
				if seen[name] {
					continue
				}
				seen[name] = true
				if strings.HasPrefix(name, miseBenchPrefix) {
					t.Errorf("%s reaches the bench task %s", root, name)
					continue
				}
				keys, _ := f.task(name)
				for k, v := range keys {
					if strings.Contains(v, miseBenchPrefix) {
						t.Errorf("task %s (reached from %s) mentions %q in %s", name, root, miseBenchPrefix, k)
					}
				}
				queue = append(queue, miseRefs(keys)...)
			}
		})
	}
}

// kubectl is declared on the tasks that reach the cluster (up, provision, down,
// seed, status, cut-upstream, restore-upstream, reset, test) and not on render,
// which contacts nothing. It is not declared in
// the global [tools], which would install it for every CI run.
func TestMiseKubectlIsDeclaredOnTheBenchTasks(t *testing.T) {
	f := loadMise(t)
	if _, globally := f["tools"]["kubectl"]; globally {
		t.Error("kubectl is declared in the global [tools]: it would be installed for every CI run; declare it on the cluster tasks")
	}
	for _, sub := range append(append([]string{}, miseBenchSubcommands...), "test") {
		name := miseBenchPrefix + ":" + sub
		task, ok := f.task(name)
		if !ok {
			t.Errorf("mise.toml has no task %q", name)
			continue
		}
		has := strings.Contains(task["tools"], "kubectl")
		switch {
		case sub == "render" && has:
			t.Errorf("%s declares kubectl but contacts no cluster", name)
		case sub != "render" && !has:
			t.Errorf("%s reaches the cluster but does not declare kubectl in its tools", name)
		}
	}
}

// The seed pushes images with skopeo, which is a prerequisite of the host (the
// version is the host's, not pinned: D158 of the plan, Q1 a): mise does not
// install it, so the description of the task says it needs it.
func TestMiseSeedTaskNamesSkopeoAsAPrerequisiteOfTheHost(t *testing.T) {
	name := miseBenchPrefix + ":seed"
	task, ok := loadMise(t).task(name)
	if !ok {
		t.Fatalf("mise.toml has no task %q", name)
	}
	if !strings.Contains(strings.ToLower(task["description"]), "skopeo") {
		t.Errorf("the description of %s does not cite skopeo as a prerequisite of the host:\n%s", name, task["description"])
	}
	if strings.Contains(task["tools"], "skopeo") {
		t.Errorf("%s declares skopeo in its tools: it is a tool of the host, not installed by mise:\n%s", name, task["tools"])
	}
	if !strings.Contains(task["tools"], "kubectl") {
		t.Errorf("%s reaches the cluster but does not declare kubectl in its tools", name)
	}
	if !strings.Contains(strings.ToLower(task["description"]), "acts on the cluster") {
		t.Errorf("the description of %s does not say it acts on the cluster, as the other tasks do:\n%s", name, task["description"])
	}
}

// restore-upstream and reset port-forward to the upstream and read digests with
// skopeo, as seed does: it is a prerequisite of the host that mise does not
// install, so the description of each task says it needs it (and does not
// declare it). cut-upstream scales a Deployment and needs nothing else than
// kubectl. All three say they act on the cluster, like the other tasks.
func TestMiseUpstreamTasksSayWhatTheyNeedAndWhatTheyDo(t *testing.T) {
	f := loadMise(t)
	for _, tt := range []struct {
		sub    string
		skopeo bool
	}{
		{"cut-upstream", false},
		{"restore-upstream", true},
		{"reset", true},
	} {
		name := miseBenchPrefix + ":" + tt.sub
		task, ok := f.task(name)
		if !ok {
			t.Errorf("mise.toml has no task %q", name)
			continue
		}
		desc := strings.ToLower(task["description"])
		if got := strings.Contains(desc, "skopeo"); got != tt.skopeo {
			t.Errorf("the description of %s names skopeo = %v, want %v:\n%s", name, got, tt.skopeo, task["description"])
		}
		if strings.Contains(task["tools"], "skopeo") {
			t.Errorf("%s declares skopeo in its tools: it is a tool of the host, not installed by mise:\n%s", name, task["tools"])
		}
		if !strings.Contains(desc, "acts on the cluster") {
			t.Errorf("the description of %s does not say it acts on the cluster, as the other tasks do:\n%s", name, task["description"])
		}
		// The versions of kubectl are the ones of the other cluster tasks.
		up, _ := f.task(miseBenchPrefix + ":up")
		if task["tools"] != up["tools"] {
			t.Errorf("%s declares tools %q, want those of nexus-bench:up (%q)", name, task["tools"], up["tools"])
		}
	}
	// reset deletes: its description says so, so that nobody runs it for a check.
	reset, _ := f.task(miseBenchPrefix + ":reset")
	if d := strings.ToLower(reset["description"]); !strings.Contains(d, "delete") && !strings.Contains(d, "erase") {
		t.Errorf("the description of nexus-bench:reset does not say it deletes the data:\n%s", reset["description"])
	}
}
