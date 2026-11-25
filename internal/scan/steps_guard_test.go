package scan

import (
	"fmt"
	"go/ast"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// What a step may say (docs/architecture/scanning.md, "Writing a Resolution,
// Steps and Repro for a new finding"). A step is an English US sentence with one
// action and no key name: the same fix is read in the finding detail, in `scan_result`
// and in the README, and a key name is true in only one of them.
//
// stepProblems checks what can be checked without judging prose:
//
//	S1  no key name: a modifier chord (ctrl+, alt+, shift+, cmd+), `esc` or `ctrl`
//	    on their own, a usual key name or a capital letter between angle brackets
//	    (<Y>, <Enter>; a placeholder such as <chart> is fine), a function key
//	    (F1 to F12), "<name> key" for a usual key name, "hit the key" or "hit the
//	    button", and the words press, keystroke, keybinding and shortcut;
//	S2  no step is empty, padded with blanks, or spread over several lines;
//	S3  a step starts with a capital letter, except the "move the file below ..." step
//	    right after the exact legacy step below;
//	S4  a step ends with a period, except the legacy step "Reference a directory that
//	    has its own kustomization.yaml, or" when "move the file below ..." follows it
//	    (the two-step alternative of a load restrictor failure, written before the
//	    convention and pinned by several tests and the cache: inherited, not a model);
//	S5  no step appears twice in the list.
//
// What it does not check, because no reliable check exists: that the first word is an
// imperative verb (a step may open with "If this reference points outside…"), that
// there is one action per step, and that the English is US. A bare "enter", "tab" or
// "return" is not flagged either: each is also an ordinary verb or noun.
var keyNamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:ctrl|alt|shift|cmd|super)\+`),
	regexp.MustCompile(`(?i)\b(?:ctrl|esc)\b`),
	regexp.MustCompile(`<(?:Enter|Esc|Tab|Space|Ctrl|Alt|Shift|[A-Z])>`),
	regexp.MustCompile(`\bF(?:[1-9]|1[0-2])\b`),
	regexp.MustCompile(`(?i)\b(?:enter|return|esc|escape|tab|space|backspace|delete|arrow|up|down|left|right)\s+key\b`),
	regexp.MustCompile(`(?i)\b(?:press|presses|pressed|pressing|keystrokes?|keybindings?|shortcuts?)\b`),
	regexp.MustCompile(`(?i)\bhit\s+(?:the\s+)?(?:key|button)\b`),
}

const loadRestrictorAlternative = "Reference a directory that has its own kustomization.yaml, or"

// legacyAlternativeStep and legacyAlternativePartner are the one two-step alternative
// that predates the convention: the text of a load restrictor failure in steps.go,
// pinned by several tests and by the scan cache. It is inherited, not a model: a new
// alternative is two sentences that each end with a period.
const legacyAlternativePartner = "move the file below "

// stepsFunctionsOutsideStepsGo lists the *Steps functions (and methods) defined in a
// file other than steps.go. TestEveryStepsFunctionHasASample reads steps.go only, so
// the provenance rule P2 (a Steps comes from a *Steps function) is only sound if no
// such function lives elsewhere.
func stepsFunctionsOutsideStepsGo(sources []guardSource) []string {
	var out []string
	for _, src := range sources {
		if src.name == "steps.go" {
			continue
		}
		for _, d := range src.file.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && strings.HasSuffix(fd.Name.Name, "Steps") {
				out = append(out, pos(src, fd)+": "+fd.Name.Name+" is a *Steps function outside steps.go: move it there so stepSamples covers it")
			}
		}
	}
	return out
}

func stepProblems(steps []string) []string {
	var problems []string
	seen := map[string]bool{}
	for i, s := range steps {
		label := fmt.Sprintf("step %d %q", i+1, s)
		switch {
		case s == "":
			problems = append(problems, label+": empty")
			continue
		case s != strings.TrimSpace(s) || strings.ContainsAny(s, "\n\r\t"):
			problems = append(problems, label+": blanks around it or a line break inside")
		}
		for _, re := range keyNamePatterns {
			if m := re.FindString(s); m != "" {
				problems = append(problems, fmt.Sprintf("%s: names a key (%q)", label, m))
			}
		}
		afterAlternative := i > 0 && steps[i-1] == loadRestrictorAlternative && strings.HasPrefix(s, legacyAlternativePartner)
		first, _ := utf8.DecodeRuneInString(s)
		if !unicode.IsUpper(first) && !afterAlternative {
			problems = append(problems, label+": does not start with a capital letter")
		}
		last := i == len(steps)-1
		switch {
		case strings.HasSuffix(s, "."):
		case s == loadRestrictorAlternative && !last && strings.HasPrefix(steps[i+1], legacyAlternativePartner):
		default:
			problems = append(problems, label+": does not end with a period")
		}
		if seen[s] {
			problems = append(problems, label+": appears twice")
		}
		seen[s] = true
	}
	return problems
}

func TestTheStepGuardCatchesWhatTheConventionForbids(t *testing.T) {
	cases := []struct {
		name  string
		steps []string
		want  string
	}{
		{"a chord", []string{"Press ctrl+r to scan again."}, "names a key"},
		{"press alone", []string{"Press the key shown in the footer."}, "names a key"},
		{"an angle key", []string{"Open the detail with <Y> and read it."}, "names a key"},
		{"a key word", []string{"Use the esc key to go back."}, "names a key"},
		{"a function key", []string{"Open the help with F1."}, "names a key"},
		{"a shortcut", []string{"Use the shortcut in the header."}, "names a key"},
		{"no capital", []string{"run helm template charts/api."}, "capital letter"},
		{"no period", []string{"Run helm template charts/api"}, "period"},
		{"empty", []string{""}, "empty"},
		{"a line break", []string{"Run it.\nThen run it again."}, "line break"},
		{"a duplicate", []string{"Run it.", "Run it."}, "twice"},
		{"a dangling alternative", []string{"Do this, or"}, "period"},
		{"a lowercase step after a plain one", []string{"Do this.", "do that."}, "capital letter"},
		{"an angle key, named", []string{"Open the menu with <Enter>."}, "names a key"},
		{"hit the key", []string{"Hit the key shown in the footer."}, "names a key"},
		{"another alternative ending in or", []string{"Do this, or", "Do that."}, "period"},
		{"a lowercase step after another alternative", []string{"Do this, or", "do that."}, "capital letter"},
		{"the legacy alternative without its partner", []string{loadRestrictorAlternative, "Do that."}, "period"},
	}
	for _, c := range cases {
		got := strings.Join(stepProblems(c.steps), "\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: problems = %q, want one mentioning %q", c.name, got, c.want)
		}
	}
}

func TestTheStepGuardIgnoresPlaceholdersAndCacheHits(t *testing.T) {
	for _, steps := range [][]string{
		{"Run helm template <chart> and fix the first error it reports."},
		{"Check the cache hit rate in the report."},
		{"Clear the <cache-dir> directory."},
	} {
		if problems := stepProblems(steps); len(problems) > 0 {
			t.Errorf("%q flagged: %v", steps, problems)
		}
	}
}

func TestTheStepGuardAcceptsTheConventionsOwnWording(t *testing.T) {
	for _, steps := range [][]string{
		{"Run helm template charts/api and fix the first error it reports.", "If a dependency is missing, run helm dependency build charts/api."},
		{"Reference a directory that has its own kustomization.yaml, or", "move the file below dev."},
		{"If this reference points outside the directory DevDesk mounts, scan the enclosing directory."},
	} {
		if problems := stepProblems(steps); len(problems) > 0 {
			t.Errorf("%q flagged: %v", steps, problems)
		}
	}
}

// stepSamples calls every *Steps function of steps.go with inputs that reach each
// of its branches, and returns the lists it produced. TestEveryStepsFunctionHasASample
// fails when steps.go gains a function this map does not know, so a new one cannot
// escape the guard by being forgotten.
func stepSamples() map[string][][]string {
	restrictor := loadRestrictorStderr
	problem := kubeconformFieldProblem{Path: "/spec/replicas", Msg: "got string, want integer"}
	unknownField := kubeconformFieldProblem{Path: "/spec/", Msg: "additional properties 'foo' not allowed"}
	resource := kubeconformResource{Kind: "Deployment", Version: "apps/v1"}
	return map[string][][]string{
		"renderSteps": {
			renderSteps(SourceHelm, "charts/api", ToolSourceBinary, "boom", false),
			renderSteps(SourceKustomize, "overlays/dev", ToolSourceBinary, "boom", false),
			renderSteps(SourceKustomize, "overlays/dev", ToolSourceBinary, restrictor, false),
			renderSteps(SourceKustomize, "overlays/dev", ToolSourceContainer, restrictor, false),
			renderSteps(SourceKustomize, "overlays/dev", ToolSourceContainer, "no such file or directory", false),
			renderSteps(SourceKustomize, "overlays/dev", ToolSourceContainer, "evalsymlink failure", false),
			// DEVDESK-767: a remote base the cut network refused.
			renderSteps(SourceKustomize, "overlays/dev", ToolSourceBinary, sshBlockedStderr, true),
			renderSteps(SourceKustomize, "overlays/dev", ToolSourceContainer, containerNoSuchHostStderr, true),
		},
		// DEVDESK-783: an overlay built as Flux builds it that still failed.
		"fluxRenderSteps": {
			fluxRenderSteps("overlays/dev", ToolSourceBinary, "boom", false),
			fluxRenderSteps("overlays/dev", ToolSourceContainer, "no such file or directory", false),
			fluxRenderSteps("overlays/dev", ToolSourceBinary, sshBlockedStderr, true),
		},
		"uncheckedOverlaySteps": {uncheckedOverlaySteps("overlays/dev", "an anchor in kustomization.yaml")},
		"helmLintSteps":         {helmLintSteps("templates/deployment.yaml", "charts/api")},
		"schemaSteps": {
			schemaSteps(resource, problem, "1.36.0", ""),
			schemaSteps(resource, problem, "", ""),
			schemaSteps(resource, problem, "1.36.0", "helm"),
			schemaSteps(resource, problem, "", "kustomize"),
			schemaSteps(resource, unknownField, "1.36.0", ""),
			schemaSteps(resource, kubeconformFieldProblem{Msg: "invalid"}, "1.36.0", ""),
		},
		"secretSteps": {secretSteps()},
	}
}

func TestEveryStepsFunctionHasASample(t *testing.T) {
	samples := stepSamples()
	src := parseGuardSources(t, map[string]string{"steps.go": packageGuardSources(t)["steps.go"]})[0]
	var discovered []string
	for _, d := range src.file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || !strings.HasSuffix(fd.Name.Name, "Steps") {
			continue
		}
		discovered = append(discovered, fd.Name.Name)
		if _, ok := samples[fd.Name.Name]; !ok {
			t.Errorf("%s writes steps and has no entry in stepSamples: add inputs that reach each of its branches", fd.Name.Name)
		}
	}
	if len(discovered) < 4 {
		t.Errorf("found %d *Steps functions in steps.go, expected at least 4: did the file move?", len(discovered))
	}
	for name := range samples {
		if !slices.Contains(discovered, name) {
			t.Errorf("stepSamples has %s, which steps.go no longer defines", name)
		}
	}
}

func TestAStepsFunctionOutsideStepsGoIsRefused(t *testing.T) {
	sources := parseGuardSources(t, map[string]string{
		"steps.go": "package scan\nfunc okSteps() []string { return nil }\n",
		"other.go": "package scan\nfunc fooSteps() []string { return []string{\"free text\"} }\n",
	})
	got := stepsFunctionsOutsideStepsGo(sources)
	if len(got) != 1 || !strings.Contains(got[0], "fooSteps") {
		t.Errorf("stepsFunctionsOutsideStepsGo = %q, want only fooSteps", got)
	}
	if got := stepsFunctionsOutsideStepsGo(sources[1:]); len(got) != 0 {
		t.Errorf("a function of steps.go flagged: %q", got)
	}
}

func TestEveryStepsFunctionOfThePackageIsDefinedInStepsGo(t *testing.T) {
	for _, v := range stepsFunctionsOutsideStepsGo(parseGuardSources(t, packageGuardSources(t))) {
		t.Error(v)
	}
}

func TestTheStepsDevDeskWritesFollowTheConvention(t *testing.T) {
	for name, lists := range stepSamples() {
		for _, steps := range lists {
			if len(steps) == 0 {
				t.Errorf("%s returned no step for a sample", name)
			}
			for _, p := range stepProblems(steps) {
				t.Errorf("%s: %s", name, p)
			}
		}
	}
	// The one list a function does not build: the constant the kustomize branch appends.
	for _, p := range stepProblems([]string{kustomizeMountStep}) {
		t.Errorf("kustomizeMountStep: %s", p)
	}
}
