package scan

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The numbered steps DevDesk writes for the findings it builds itself
// (DEVDESK-752). Every function here is pure: the caller says what it ran and
// what the tool printed. A step is one imperative sentence in English US with
// one action; alternatives are separate steps. The Resolution of the same
// finding is untouched.

// kustomizeMountStep is added for a kustomize image that could not find a path
// (see mountResolution): the container sees only what DevDesk mounts.
const kustomizeMountStep = "If this reference points outside the directory DevDesk mounts into the container, " +
	"scan the enclosing directory or set scan.tools.kustomize.source: binary."

// isLoadRestrictor reports whether kustomize refused a file outside the overlay
// directory.
func isLoadRestrictor(detail string) bool {
	return strings.Contains(detail, "security; file") && strings.Contains(detail, "is not in or below")
}

// shellQuote is word as one argument of a POSIX shell (DEVDESK-772). A word made
// only of plain path characters stays as it is, so an ordinary folder is shown
// the way it is written; any other word is wrapped in single quotes (the
// apostrophe rule is below). An "=" is not plain: with zsh's magic_equal_subst a
// bare x==ls is read as x=/usr/bin/ls. The quotes are the POSIX ones: PowerShell
// and cmd read them differently, and no form is written per platform.
//
// It does not make a line break or a control character safe, since a terminal
// reads those before the shell does: a caller asks quotable first and writes a
// text step (needsQuotingStep) when the answer is no.
func shellQuote(word string) string {
	if word == "" {
		return "''"
	}
	plain := true
	for _, r := range word {
		if !plainPathRune(r) {
			plain = false
			break
		}
	}
	if plain {
		return word
	}
	// An apostrophe inside the word closes the quote, is escaped, and reopens it:
	// a'b becomes 'a'\''b'.
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// plainPathRune is a character a shell reads as itself in the middle of a word.
func plainPathRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return strings.ContainsRune("-_.+@:,/", r)
}

// quotable says a folder name can be shown inside quotes: it is valid UTF-8 and
// every character prints (unicode.IsPrint, the test logWord applies). A line
// break, a control character, a format character such as U+202E or U+2028 and
// U+0085 are not: no quote protects them, so the step is a text instead.
func quotable(name string) bool {
	return utf8.ValidString(name) && !strings.ContainsFunc(name, func(r rune) bool { return !unicode.IsPrint(r) })
}

// needsQuotingStep is the step written in place of a command whose folder name
// is not quotable. It says what to do, on one line, without the name (which
// could hold anything) and without a command to copy.
func needsQuotingStep(action string) string {
	return "Folder name needs quoting: rename the folder, then " + action + "."
}

// typedDir is a folder as the user is told to type it after helm
// (DEVDESK-768, DEVDESK-772). A name that starts with "-" would be read as an
// option, so it gets the "./" the scan itself uses (asPathArg), before the
// quoting; a name a shell would split or expand is quoted (shellQuote). Any
// other folder keeps its plain name, which the same command accepts. Only the
// command's argument goes through it: a title or a file names the folder as it
// is, and a sentence through logWord.
func typedDir(dir string) string {
	if strings.HasPrefix(dir, "-") {
		dir = "./" + dir
	}
	return shellQuote(dir)
}

// typedKustomizeDir is typedDir for kustomize, which also reads a relative name
// as a remote git base when its first segment holds a ".", a ":" or an "@"
// (github.com/org/repo, git@host:o/r, DEVDESK-763). Those get the "./" of
// asPathArg too; an ordinary folder stays bare.
func typedKustomizeDir(dir string) string {
	first, _, _ := strings.Cut(dir, "/")
	if (first != "." && first != ".." && strings.ContainsAny(first, ".:@")) || strings.HasPrefix(dir, "-") {
		dir = asPathArg(dir)
	}
	return shellQuote(dir)
}

// renderSteps are the steps for a chart or an overlay that did not render.
// target is the overlay directory or the chart, relative to the scanned
// directory; detail is what the tool printed. blocked says the failure is a
// remote base the cut network refused (the caller's remoteBlocked, asked only of
// an offline run): it gets steps of its own.
func renderSteps(source, target string, from ToolSource, detail string, blocked bool) []string {
	if source != SourceKustomize {
		if !quotable(target) {
			return []string{
				needsQuotingStep("render the chart with helm template"),
				needsQuotingStep("build its dependencies with helm dependency build"),
			}
		}
		return []string{
			fmt.Sprintf("Run helm template %s and fix the first error it reports.", typedDir(target)),
			fmt.Sprintf("If a dependency is missing, run helm dependency build %s.", typedDir(target)),
		}
	}
	var steps []string
	switch {
	case blocked:
		return []string{
			fmt.Sprintf("Copy the remote base that %s names into the repository and reference it by a relative path.", logWord(target)),
			"Or set scan.tools.kustomize.allow_remote_bases: true if you trust this repository.",
		}
	case isLoadRestrictor(detail):
		steps = []string{
			"Reference a directory that has its own kustomization.yaml, or",
			fmt.Sprintf("move the file below %s.", logWord(target)),
		}
	case !quotable(target):
		steps = []string{needsQuotingStep("build the overlay with kustomize build")}
	default:
		steps = []string{fmt.Sprintf("Run kustomize build %s and fix the first error it reports.", typedKustomizeDir(target))}
	}
	if from == ToolSourceContainer &&
		(strings.Contains(detail, "evalsymlink failure") || strings.Contains(detail, "no such file or directory")) {
		steps = append(steps, kustomizeMountStep)
	}
	return steps
}

// uncheckedOverlaySteps are the steps for an overlay that was not built because
// DevDesk could not check its references (DEVDESK-771): dir is the overlay
// directory and reason what stopped the check.
func uncheckedOverlaySteps(dir, reason string) []string {
	return []string{
		fmt.Sprintf("Remove what stops DevDesk from checking the references of %s: %s.", dir, reason),
		"Or set scan.tools.kustomize.source: image to build the overlay in a container without a network.",
		"Or set scan.tools.kustomize.allow_remote_bases: true if you trust this repository and the services of this machine.",
	}
}

// helmLintSteps are the steps for one message of helm lint: file is the finding's
// file and chart the chart directory.
func helmLintSteps(file, chart string) []string {
	again := needsQuotingStep("lint the chart again with helm lint")
	if quotable(chart) {
		again = fmt.Sprintf("Run helm lint %s again to confirm.", typedDir(chart))
	}
	return []string{
		fmt.Sprintf("Fix what helm lint reports in %s.", logWord(file)),
		again,
	}
}

// schemaSteps are the steps for a field the schema rejected. rendered is "" for
// a plain manifest and the renderer's dialect (helm, kustomize) for a rendered
// one.
func schemaSteps(r kubeconformResource, p kubeconformFieldProblem, version, rendered string) []string {
	// field, Kind and Version come from the manifest, so they go through logWord.
	field := p.Path
	if m := additionalProperty.FindStringSubmatch(p.Msg); m != nil {
		field = strings.TrimSuffix(p.Path, "/") + "/" + m[1]
	}
	if field == "" {
		field = "the resource"
	}
	release := "Kubernetes"
	if version != "" {
		release += " " + version
	}
	steps := []string{fmt.Sprintf("Correct %s so it matches the %s %s schema of %s.", logWord(field), logWord(r.Version), logWord(r.Kind), release)}
	if rendered != "" {
		steps = append(steps, fmt.Sprintf("Render it with %s and validate the output against %s.", rendered, release))
	}
	return steps
}

// secretSteps are the steps for a secret Gitleaks or Trivy found. It returns a
// new slice on every call: a shared one would let a caller change every finding.
func secretSteps() []string {
	return []string{
		"Revoke or rotate this secret where it was issued: treat it as exposed.",
		"Remove it from the file and read it from a secret store or an environment variable.",
	}
}
