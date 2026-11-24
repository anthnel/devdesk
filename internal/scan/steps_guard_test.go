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
//	    on their own, a letter between angle brackets (<Y>, <Enter>), a function key
//	    (F1 to F12), "<name> key" for a usual key name, and the words press, hit,
//	    keystroke, keybinding and shortcut;
//	S2  no step is empty, padded with blanks, or spread over several lines;
//	S3  a step starts with a capital letter, except right after a step that ends in
//	    ", or" (the two-step alternative of a load restrictor failure, written that
//	    way before the convention; its test pins the exact text);
//	S4  a step ends with a period, except one that ends in ", or" and is followed by
//	    its alternative;
//	S5  no step appears twice in the list.
//
// What it does not check, because no reliable check exists: that the first word is an
// imperative verb (a step may open with "If this reference points outside…"), that
// there is one action per step, and that the English is US. A bare "enter", "tab" or
// "return" is not flagged either: each is also an ordinary verb or noun.
var keyNamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:ctrl|alt|shift|cmd|super)\+`),
	regexp.MustCompile(`(?i)\b(?:ctrl|esc)\b`),
	regexp.MustCompile(`<[A-Za-z][A-Za-z0-9]*>`),
	regexp.MustCompile(`\bF(?:[1-9]|1[0-2])\b`),
	regexp.MustCompile(`(?i)\b(?:enter|return|esc|escape|tab|space|backspace|delete|arrow|up|down|left|right)\s+key\b`),
	regexp.MustCompile(`(?i)\b(?:press|presses|pressed|pressing|hit|keystrokes?|keybindings?|shortcuts?)\b`),
}

const alternativeEnd = ", or"

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
		afterAlternative := i > 0 && strings.HasSuffix(steps[i-1], alternativeEnd)
		first, _ := utf8.DecodeRuneInString(s)
		if !unicode.IsUpper(first) && !afterAlternative {
			problems = append(problems, label+": does not start with a capital letter")
		}
		last := i == len(steps)-1
		switch {
		case strings.HasSuffix(s, "."):
		case strings.HasSuffix(s, alternativeEnd) && !last:
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
	}
	for _, c := range cases {
		got := strings.Join(stepProblems(c.steps), "\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: problems = %q, want one mentioning %q", c.name, got, c.want)
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
			renderSteps(SourceHelm, "charts/api", ToolSourceBinary, "boom"),
			renderSteps(SourceKustomize, "overlays/dev", ToolSourceBinary, "boom"),
			renderSteps(SourceKustomize, "overlays/dev", ToolSourceBinary, restrictor),
			renderSteps(SourceKustomize, "overlays/dev", ToolSourceContainer, restrictor),
			renderSteps(SourceKustomize, "overlays/dev", ToolSourceContainer, "no such file or directory"),
			renderSteps(SourceKustomize, "overlays/dev", ToolSourceContainer, "evalsymlink failure"),
		},
		"helmLintSteps": {helmLintSteps("templates/deployment.yaml", "charts/api")},
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
