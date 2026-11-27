package enginecontract_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-09a2 (DEVDESK-943), criterion 3: on the test VM, a container created WITHOUT
// the label survives a contract run. contract.sh creates it (the sentinel) before
// the contracts and checks it after them. That is the real negative control of the
// cleanup; the one that runs everywhere without an engine is in package docker.
//
// The script is never run here: this machine has a real docker and no test VM,
// and the sentinel needs a VM. These tests read the text of tools/engine-vms/
// contract.sh, as script_test.go does for the gate, and they hold the order and
// the shape of the sentinel steps. What they cannot see, and jakku checks (step 9 of
// the plan): that the sentinel really survives with each of the four engines.
//
// What the script is expected to do, besides the gate and the VM set-up it has
// from OC-09a1:
//
//	vm_as_mode() { ... }   a function that runs a command in the VM as the user of the
//	                       mode: vm_user when $mode is rootless, vm_root otherwise. The
//	                       sentinel must live in the store the contracts use (rootless
//	                       podman has the store of the dd user, not root's), or the check
//	                       would be true of a store nobody touched.
//	pull                   vm_as_mode "$engine" ... pull "$CONTRACT_IMAGE": the image is
//	                       pinned by digest in tools/engine-vms/versions.env
//	                       (CONTRACT_IMAGE), is the only image the script pulls, and the
//	                       Go code never pulls (G8).
//	sentinel=$(vm_as_mode ... run -d --pull=never --name ddc-sentinel-$run "$CONTRACT_IMAGE" sleep 3600)
//	                       no --label, after the reset and the pull, before the contracts.
//	contracts              run with DEVDESK_ENGINE_CONTRACT_RUN=$run and
//	                       DEVDESK_ENGINE_CONTRACT_IMAGE=$CONTRACT_IMAGE in their explicit environment.
//	check                  once the contracts are done, pass or fail: inspect "$sentinel";
//	                       if it is gone, the message "the cleanup touched a container
//	                       without the label" and a failure. Nothing between the contracts
//	                       and the check can leave the script (no exit, no die).
//	removal                of the sentinel by its id, and of nothing else that is a container.

type scriptLine struct {
	n    int // 1-based
	text string
}

// scriptCode is the non-comment lines of contract.sh.
func scriptCode(t *testing.T) []scriptLine {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "engine-vms", "contract.sh"))
	if err != nil {
		t.Fatalf("tools/engine-vms/contract.sh: %v", err)
	}
	var out []scriptLine
	for i, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out = append(out, scriptLine{i + 1, line})
	}
	return out
}

// first is the index in lines of the first line matching re, or -1.
func first(lines []scriptLine, re *regexp.Regexp, from int) int {
	for i := max(from, 0); i < len(lines); i++ {
		if re.MatchString(lines[i].text) {
			return i
		}
	}
	return -1
}

// statement is the line at i with the lines it continues onto (a trailing backslash).
func statement(lines []scriptLine, i int) string {
	var b strings.Builder
	for ; i < len(lines); i++ {
		text := lines[i].text
		b.WriteString(text)
		b.WriteByte('\n')
		if !strings.HasSuffix(strings.TrimRight(text, " \t"), `\`) {
			break
		}
	}
	return b.String()
}

var (
	reSentinelCreate = regexp.MustCompile(`ddc-sentinel-\$\{?run\}?`)
	reContractsRun   = regexp.MustCompile(`-test\.run`)
	reImageVar       = regexp.MustCompile(`\$\{?CONTRACT_IMAGE\}?`)
	reSentinelVar    = regexp.MustCompile(`\$\{?sentinel\}?`)
	reWordPull       = regexp.MustCompile(`\bpull\b`)
	reLeavesScript   = regexp.MustCompile(`^\s*(exit|return)\b|\bdie\b`)
)

func TestTheScriptCreatesTheSentinelWithoutALabelInTheStoreOfTheMode(t *testing.T) {
	lines := scriptCode(t)
	i := first(lines, reSentinelCreate, 0)
	if i < 0 {
		t.Fatal(`contract.sh never creates a container named ddc-sentinel-$run: the cleanup is never checked against a container without the label`)
	}
	stmt := statement(lines, i)

	if !regexp.MustCompile(`\bsentinel=\$\(`).MatchString(stmt) {
		t.Errorf("contract.sh:%d does not keep the id of the sentinel in $sentinel (sentinel=$(...)):\n%s", lines[i].n, stmt)
	}
	for _, want := range []string{" run ", " -d", "--pull=never", "sleep"} {
		if !strings.Contains(" "+strings.ReplaceAll(stmt, "\n", " ")+" ", want) {
			t.Errorf("contract.sh:%d: the sentinel command does not contain %q:\n%s", lines[i].n, want, stmt)
		}
	}
	if !reImageVar.MatchString(stmt) {
		t.Errorf("contract.sh:%d: the sentinel does not run $CONTRACT_IMAGE, the pinned image:\n%s", lines[i].n, stmt)
	}
	if strings.Contains(strings.ToLower(stmt), "label") {
		t.Errorf("contract.sh:%d: the sentinel carries a label, which is the one thing it must not:\n%s", lines[i].n, stmt)
	}
	if !strings.Contains(stmt, "vm_as_mode") {
		t.Errorf("contract.sh:%d: the sentinel is not created through vm_as_mode, so it may sit in another store than the contracts':\n%s", lines[i].n, stmt)
	}
}

func TestTheScriptDefinesVmAsModeOverBothUsers(t *testing.T) {
	lines := scriptCode(t)
	i := first(lines, regexp.MustCompile(`^\s*vm_as_mode\s*\(\)`), 0)
	if i < 0 {
		t.Fatal("contract.sh defines no vm_as_mode() function")
	}
	var body strings.Builder
	for j := i; j < len(lines); j++ {
		body.WriteString(lines[j].text + "\n")
		if j > i && strings.TrimSpace(lines[j].text) == "}" {
			break
		}
	}
	for _, want := range []string{"vm_user", "vm_root", "rootless"} {
		if !strings.Contains(body.String(), want) {
			t.Errorf("vm_as_mode() does not mention %q: it must send rootless runs to the user and the others to root:\n%s", want, body.String())
		}
	}
	// The sentinel steps (create, inspect, remove) all go through it.
	for _, re := range []*regexp.Regexp{reSentinelCreate, regexp.MustCompile(`\binspect\b.*` + reSentinelVar.String()), regexp.MustCompile(`\brm\b.*` + reSentinelVar.String())} {
		j := first(lines, re, 0)
		if j < 0 {
			continue // reported by the other tests
		}
		if stmt := statement(lines, j); !strings.Contains(stmt, "vm_as_mode") {
			t.Errorf("contract.sh:%d does not go through vm_as_mode:\n%s", lines[j].n, stmt)
		}
	}
}

func TestTheScriptPinsThePullAndTheSentinelBeforeTheContracts(t *testing.T) {
	lines := scriptCode(t)
	reset := first(lines, regexp.MustCompile(`\breset\.sh\b`), 0)
	pull := first(lines, reWordPull, 0)
	create := first(lines, reSentinelCreate, 0)
	run := first(lines, reContractsRun, 0)
	if reset < 0 || pull < 0 || create < 0 || run < 0 {
		t.Fatalf("contract.sh lacks a step: reset %d, pull %d, sentinel %d, contracts %d (indexes of the non-comment lines, -1 for none)", reset, pull, create, run)
	}
	if reset >= pull || pull >= create || create >= run {
		t.Errorf("order of the steps: reset at line %d, pull at %d, sentinel at %d, contracts at %d; want reset, pull, sentinel, contracts",
			lines[reset].n, lines[pull].n, lines[create].n, lines[run].n)
	}
	// The one image the script pulls is the contract image; nothing else is pulled.
	for _, l := range lines {
		if reWordPull.MatchString(l.text) && !reImageVar.MatchString(l.text) {
			t.Errorf("contract.sh:%d pulls something other than $CONTRACT_IMAGE: %s", l.n, strings.TrimSpace(l.text))
		}
	}
}

func TestTheScriptChecksAndRemovesTheSentinelAfterTheContractsWhateverTheirResult(t *testing.T) {
	lines := scriptCode(t)
	run := first(lines, reContractsRun, 0)
	check := first(lines, regexp.MustCompile(`the cleanup touched a container without the label`), 0)
	inspect := first(lines, regexp.MustCompile(`\binspect\b.*`+reSentinelVar.String()), 0)
	if run < 0 || check < 0 || inspect < 0 {
		t.Fatalf(`contract.sh lacks a step: contracts %d, "inspect $sentinel" %d, the message "the cleanup touched a container without the label" %d`, run, inspect, check)
	}
	if run >= inspect {
		t.Errorf("the sentinel is inspected at line %d, before the contracts run at line %d", lines[inspect].n, lines[run].n)
	}
	last := max(inspect, check)
	for i := run + 1; i < last; i++ {
		if reLeavesScript.MatchString(lines[i].text) {
			t.Errorf("contract.sh:%d can leave the script between the contracts and the sentinel check (the check would not run after a failure): %s",
				lines[i].n, strings.TrimSpace(lines[i].text))
		}
	}
	rm := first(lines, regexp.MustCompile(`\brm\b.*`+reSentinelVar.String()), last)
	if rm < 0 {
		t.Errorf("contract.sh does not remove the sentinel by its id after the check")
	}
}

func TestTheScriptRemovesNoContainerButTheSentinelAndPrunesNothing(t *testing.T) {
	destructive := regexp.MustCompile(`\b(rm|rmi|stop|kill|prune|system)\b`)
	for _, l := range scriptCode(t) {
		if !destructive.MatchString(l.text) || (!strings.Contains(l.text, "vm_as_mode") && !strings.Contains(l.text, "vm_root")) {
			continue
		}
		switch {
		case strings.Contains(l.text, "prune") || regexp.MustCompile(`\bsystem\b`).MatchString(l.text):
			t.Errorf("contract.sh:%d prunes or acts on the whole engine: %s", l.n, strings.TrimSpace(l.text))
		case reSentinelVar.MatchString(l.text), strings.Contains(l.text, "vmdir"):
			// the sentinel by id, or the directory this script made in the VM
		default:
			t.Errorf("contract.sh:%d removes something that is neither the sentinel nor its own directory: %s", l.n, strings.TrimSpace(l.text))
		}
	}
}

func TestTheScriptGivesTheContractsTheRunAndTheImage(t *testing.T) {
	text := ""
	for _, l := range scriptCode(t) {
		text += l.text + "\n"
	}
	for name, re := range map[string]*regexp.Regexp{
		"DEVDESK_ENGINE_CONTRACT_RUN=$run":               regexp.MustCompile(`DEVDESK_ENGINE_CONTRACT_RUN=\$\{?run\}?`),
		"DEVDESK_ENGINE_CONTRACT_IMAGE=$CONTRACT_IMAGE":  regexp.MustCompile(`DEVDESK_ENGINE_CONTRACT_IMAGE=\$\{?CONTRACT_IMAGE\}?`),
		"DEVDESK_ENGINE_CONTRACT_TOOL=$engine (OC-09a1)": regexp.MustCompile(`DEVDESK_ENGINE_CONTRACT_TOOL=\$\{?engine\}?`),
	} {
		if !re.MatchString(text) {
			t.Errorf("the explicit environment of the contracts in contract.sh lacks %s: the run id is what the lab mark asks for, the image is what the contracts start", name)
		}
	}
}

func TestVersionsEnvPinsTheContractImageByDigest(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "engine-vms", "versions.env"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^CONTRACT_IMAGE=[a-z0-9][a-z0-9./_-]*(:[A-Za-z0-9._-]+)?@sha256:[0-9a-f]{64}\s*(#.*)?$`)
	if !re.Match(data) {
		t.Errorf("tools/engine-vms/versions.env has no line CONTRACT_IMAGE=<repository>@sha256:<64 hex>: a tag moves, and the contracts " +
			"must not depend on what a registry says today (plan question 3, option A; same trust-on-first-use note as the other sums)")
	}
	if regexp.MustCompile(`(?m)^CONTRACT_IMAGE=[^@\n]*(\s|$)`).Match(data) && !re.Match(data) {
		t.Error("CONTRACT_IMAGE is not pinned by digest")
	}
	// Adding the key must not break the reader the Go side uses.
	if _, err := enginefixture.ParseVersions(data); err != nil {
		t.Errorf("ParseVersions(versions.env) = %v after adding CONTRACT_IMAGE", err)
	}
}
