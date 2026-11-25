package scan

import (
	"reflect"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/anthnel/devdesk/internal/testutil/stepshell"
)

// DEVDESK-772: a step that tells the user to type a command is read by a shell,
// and the folder in it comes from a repository DevDesk does not trust. DEVDESK-768
// only put "./" in front of a name that starts with "-". The decision:
//
//   - a name that is not made of plain path characters is wrapped in POSIX single
//     quotes, an apostrophe inside it written '\'' ;
//   - a name no form protects safely (a line break, any control character) makes
//     the step a text on one line saying the folder name needs quoting, not a
//     command to copy;
//   - an ordinary folder keeps its plain name, with no apostrophe;
//   - the limit is documented, not coded: PowerShell and cmd read these quotes
//     differently, and there is no form per platform.
//
// The behavior tests go through the *Steps functions as they are today, so they
// fail on what a user would copy. The helper they expect is shellQuote (the
// guard in steps_quoting_guard_test.go looks for it by name).

// hostileFolder is a folder name that makes a shell run or split something when it
// is pasted bare, with the form the steps must show instead: single-quoted.
type hostileFolder struct {
	name   string
	dir    string
	quoted string
}

// listed by the story
var storyFolders = []hostileFolder{
	{"command substitution", "$(touch pwned)", `'$(touch pwned)'`},
	{"semicolon", "a;touch pwned", `'a;touch pwned'`},
	{"backticks", "`id`", "'`id`'"},
	{"space", "a b", `'a b'`},
	{"apostrophe", "a'b", `'a'\''b'`},
	{"ampersand", "a&b", `'a&b'`},
	{"pipe", "a|b", `'a|b'`},
	{"glob", "a*", `'a*'`},
}

// More ways a bare word is read wrongly. The step must show a quoted form too,
// but only what the shell does with the pasted command is held here.
var moreFolders = []hostileFolder{
	{"redirection", "a>pwned", `'a>pwned'`},
	{"comment", "#c", `'#c'`},
	{"tilde", "~root", `'~root'`},
	{"backslash", `a\b`, `'a\b'`},
	{"double quote", `a"b`, `'a"b'`},
	{"history", "!x", `'!x'`},
	{"brace", "{a,b}", `'{a,b}'`},
	{"parenthesis", "a(b)", `'a(b)'`},
	{"dollar variable", "$HOME", `'$HOME'`},
	{"two spaces", "a  b", `'a  b'`},
	// "=" anywhere, at the head included: zsh reads x==ls as x=/usr/bin/ls and =ls
	// as the path of ls.
	{"equal at the head", "=x", `'=x'`},
	{"equal in the middle", "a=b", `'a=b'`},
	{"double equal", "x==ls", `'x==ls'`},
	// What no quote is needed for stays bare, and the empty name is one empty word.
	{"empty", "", `''`},
	{"dot", ".", `.`},
}

// A step's command, as the four sentences DevDesk writes today; these are the
// frozen wordings of steps.go.
type commandStep struct {
	program string // what Command looks for
	stubs   string // the program the shell runs
	argv    []string
	steps   func(dir string) []string
	index   int
	text    func(word string) string
}

var commandSteps = []commandStep{
	{
		"helm template", "helm", []string{"template"},
		func(dir string) []string { return renderSteps(SourceHelm, dir, ToolSourceBinary, "boom", false) },
		0, func(w string) string { return "Run helm template " + w + " and fix the first error it reports." },
	},
	{
		"helm dependency build", "helm", []string{"dependency", "build"},
		func(dir string) []string { return renderSteps(SourceHelm, dir, ToolSourceBinary, "boom", false) },
		1, func(w string) string { return "If a dependency is missing, run helm dependency build " + w + "." },
	},
	{
		"helm lint", "helm", []string{"lint"},
		func(dir string) []string { return helmLintSteps(dir+"/templates/a.yaml", dir) },
		1, func(w string) string { return "Run helm lint " + w + " again to confirm." },
	},
	{
		"kustomize build", "kustomize", []string{"build"},
		func(dir string) []string { return renderSteps(SourceKustomize, dir, ToolSourceBinary, "boom", false) },
		0, func(w string) string { return "Run kustomize build " + w + " and fix the first error it reports." },
	},
}

func TestTheStepsShowAHostileFolderNameInSingleQuotes(t *testing.T) {
	for _, f := range append(append([]hostileFolder{}, storyFolders...), moreFolders...) {
		for _, c := range commandSteps {
			t.Run(f.name+"/"+c.program, func(t *testing.T) {
				steps := c.steps(f.dir)
				if c.index >= len(steps) {
					t.Fatalf("steps = %q, want at least %d", steps, c.index+1)
				}
				if got, want := steps[c.index], c.text(f.quoted); got != want {
					t.Errorf("step = %q, want %q", got, want)
				}
			})
		}
	}
}

// What a pasted step does is the whole point: nothing runs but the tool, and the
// tool receives the folder name, whole, as one argument.
func TestAPastedStepRunsNothingElseAndGivesTheFolderAsOneArgument(t *testing.T) {
	stepshell.Available(t)
	for _, f := range append(append([]hostileFolder{}, storyFolders...), moreFolders...) {
		for _, c := range commandSteps {
			t.Run(f.name+"/"+c.program, func(t *testing.T) {
				steps := c.steps(f.dir)
				if c.index >= len(steps) {
					t.Fatalf("steps = %q, want at least %d", steps, c.index+1)
				}
				cmd, ok := stepshell.Command(steps[c.index], c.program)
				if !ok {
					t.Fatalf("step %q holds no %q command", steps[c.index], c.program)
				}
				res := stepshell.Run(t, cmd, c.stubs)
				if res.Pwned {
					t.Errorf("pasting %q created the file pwned: the folder name ran as code", cmd)
				}
				if !res.Called {
					t.Fatalf("pasting %q never reached %s (stderr %q)", cmd, c.stubs, res.Stderr)
				}
				want := append(append([]string{}, c.argv...), f.dir)
				if !reflect.DeepEqual(res.Args, want) {
					t.Errorf("pasting %q gave %s the arguments %q, want %q", cmd, c.stubs, res.Args, want)
				}
			})
		}
	}
}

// What is written for a folder a shell reads as it is stays as it is, and a
// dash still gets its "./": a quote is added only where a shell needs one.
func TestAnOrdinaryFolderKeepsItsPlainNameAndADashGetsOnlyItsDotSlash(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		word string // as helm and kustomize are told to read it
		kust string // kustomize, which also reads a dotted first segment as a remote base
	}{
		{"chart", "charts/app", "charts/app", "charts/app"},
		{"overlay", "kustomizations/dev", "kustomizations/dev", "kustomizations/dev"},
		{"underscore, plus, at, colon and comma", "charts/a_b+c@d:e,f", "charts/a_b+c@d:e,f", "charts/a_b+c@d:e,f"},
		{"dot", ".", ".", "."},
		{"dash", "-x", "./-x", "./-x"},
		// "=" is not a plain character since DEVDESK-772: with zsh's
		// magic_equal_subst a bare x==ls is read as x=/usr/bin/ls.
		{"equal", "charts/a_b+c=d", `'charts/a_b+c=d'`, `'charts/a_b+c=d'`},
		{"option-looking", "--load-restrictor=LoadRestrictionsNone", `'./--load-restrictor=LoadRestrictionsNone'`, `'./--load-restrictor=LoadRestrictionsNone'`},
		{"empty", "", "''", "''"},
		{"dash below the first segment", "charts/-x", "charts/-x", "charts/-x"},
		{"dotted host", "github.com/org/repo", "github.com/org/repo", "./github.com/org/repo"},
		{"scp-like", "git@host:o/r", "git@host:o/r", "./git@host:o/r"},
		// Both protections at once: the dot-slash first, then the quotes around the whole.
		{"dash and a command", "-x $(id)", `'./-x $(id)'`, `'./-x $(id)'`},
		{"dash and a quote", "-x'y", `'./-x'\''y'`, `'./-x'\''y'`},
		{"remote-looking and a blank", "github.com/o r", `'github.com/o r'`, `'./github.com/o r'`},
		{"dotted first segment and a pipe", "a.b/c|d", `'a.b/c|d'`, `'./a.b/c|d'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, c := range commandSteps {
				word := tt.word
				if c.stubs == "kustomize" {
					word = tt.kust
				}
				steps := c.steps(tt.dir)
				if got, want := steps[c.index], c.text(word); got != want {
					t.Errorf("%s: step = %q, want %q", c.program, got, want)
				}
				if !strings.ContainsAny(tt.dir, " $'|=") && tt.dir != "" && strings.Contains(strings.Join(steps, "\n"), "'") {
					t.Errorf("%s: an ordinary folder got an apostrophe: %q", c.program, steps)
				}
				// Whatever the form, a shell must read it as the one folder, with its ./
				// when the tool needs it.
				cmd, ok := stepshell.Command(steps[c.index], c.program)
				if !ok {
					t.Fatalf("%s: no command in %q", c.program, steps)
				}
				res := stepshell.Run(t, cmd, c.stubs)
				wantArgs := append(append([]string{}, c.argv...), unquoteWord(word))
				if res.Pwned || !reflect.DeepEqual(res.Args, wantArgs) {
					t.Errorf("%s: pasting %q gave %q (pwned %v), want %q", c.program, cmd, res.Args, res.Pwned, wantArgs)
				}
			}
		})
	}
}

// unquoteWord reads back a word shellQuote wrote: the outer quotes off, an
// apostrophe written as quote, backslash, quote, quote back to one.
func unquoteWord(word string) string {
	if len(word) >= 2 && strings.HasPrefix(word, "'") && strings.HasSuffix(word, "'") {
		return strings.ReplaceAll(word[1:len(word)-1], `'\''`, "'")
	}
	return word
}

func TestADashFolderWithAnInjectionIsDotSlashedAndQuotedWhenPasted(t *testing.T) {
	stepshell.Available(t)
	const dir = "-x $(touch pwned)"
	for _, c := range commandSteps {
		t.Run(c.program, func(t *testing.T) {
			cmd, ok := stepshell.Command(c.steps(dir)[c.index], c.program)
			if !ok {
				t.Fatalf("no %q command in %q", c.program, c.steps(dir))
			}
			res := stepshell.Run(t, cmd, c.stubs)
			if res.Pwned {
				t.Errorf("pasting %q created the file pwned", cmd)
			}
			want := append(append([]string{}, c.argv...), "./"+dir)
			if !reflect.DeepEqual(res.Args, want) {
				t.Errorf("pasting %q gave %q, want %q", cmd, res.Args, want)
			}
		})
	}
}

// controlFolders are names no quote protects once pasted: a line break ends the
// command, and a control character is read by the terminal before the shell sees
// it (an escape sequence rewrites the screen, a tab completes). The payload
// "touch pwned" is what a second command would be.
var controlFolders = []struct{ name, dir string }{
	{"line feed", "x\ntouch pwned"},
	{"carriage return", "x\rtouch pwned"},
	{"CRLF", "x\r\ntouch pwned"},
	{"escape sequence", "x\x1b[2Jtouch pwned"},
	{"tab", "x\ttouch pwned"},
	{"delete", "x\x7ftouch pwned"},
	{"next line", "x\u0085touch pwned"},
	{"vertical tab", "x\vtouch pwned"},
	{"bell", "x\atouch pwned"},
	{"line feed after a quote", "a'\ntouch pwned"},
	{"dash and a line feed", "-x\ntouch pwned"},
	// Not control characters for unicode.IsControl, but none of them prints: a
	// bidirectional override rewrites what the line reads as, U+2028 breaks a line
	// in some terminals and editors, U+00A0 looks like a blank the shell does not
	// split on. And bytes that are not text at all.
	{"right-to-left override", "x\u202etouch pwned"},
	{"line separator", "x\u2028touch pwned"},
	{"paragraph separator", "x\u2029touch pwned"},
	{"no-break space", "x\u00a0touch pwned"},
	{"zero-width space", "x\u200btouch pwned"},
	{"invalid UTF-8", "x\xfftouch pwned"},
	{"NUL", "x\x00touch pwned"},
}

// notShown says a step holds something a terminal could act on or a reader could
// misread: a rune that does not print (a control character, a line or paragraph
// separator, a format character such as U+202E, a no-break space) or a byte that
// is not UTF-8.
func notShown(s string) bool {
	return !utf8.ValidString(s) || strings.ContainsFunc(s, func(r rune) bool { return !unicode.IsPrint(r) })
}

// Every step of every list, whatever the branch: none spans two lines or holds a
// control character, so none can start a second command or move the cursor. The
// steps that are commands are texts then, which say the name needs quoting and do
// not repeat it.
func TestAFolderNameWithALineBreakOrAControlCharacterGivesOneLineAndNoCommand(t *testing.T) {
	for _, f := range controlFolders {
		t.Run(f.name, func(t *testing.T) {
			lists := map[string][]string{
				"helm render":       renderSteps(SourceHelm, f.dir, ToolSourceBinary, "boom", false),
				"kustomize build":   renderSteps(SourceKustomize, f.dir, ToolSourceBinary, "boom", false),
				"kustomize, binary": renderSteps(SourceKustomize, f.dir, ToolSourceBinary, loadRestrictorStderr, false),
				"kustomize, mount":  renderSteps(SourceKustomize, f.dir, ToolSourceContainer, "no such file or directory", false),
				"kustomize blocked": renderSteps(SourceKustomize, f.dir, ToolSourceBinary, sshBlockedStderr, true),
				"helm lint":         helmLintSteps(f.dir+"/templates/a.yaml", f.dir),
				"helm lint, chart":  helmLintSteps(f.dir, f.dir),
			}
			for list, steps := range lists {
				for _, s := range steps {
					if notShown(s) {
						t.Errorf("%s: step %q holds a line break, a control character or something that does not print", list, s)
					}
				}
				for _, p := range stepProblems(steps) {
					t.Errorf("%s: %s", list, p)
				}
			}

			commands := map[string]string{}
			for _, c := range commandSteps {
				var steps []string
				switch c.program {
				case "helm template", "helm dependency build":
					steps = lists["helm render"]
				case "helm lint":
					steps = lists["helm lint"]
				default:
					steps = lists["kustomize build"]
				}
				if c.index >= len(steps) {
					t.Fatalf("%s: steps = %q, want at least %d", c.program, steps, c.index+1)
				}
				commands[c.program] = steps[c.index]
			}
			for program, step := range commands {
				if !strings.Contains(strings.ToLower(step), "needs quoting") {
					t.Errorf("%s: step %q is not the text saying the folder name needs quoting", program, step)
				}
				if strings.Contains(step, "touch pwned") {
					t.Errorf("%s: step %q repeats the name", program, step)
				}
				if cmd, ok := stepshell.Command(step, program); ok {
					t.Errorf("%s: step %q still holds a command to copy (%q)", program, step, cmd)
				}
			}
		})
	}
}

// localRepro (DEVDESK-772, checked): a Repro is built by reproLine, which refuses a
// token that is not PlainWords, holds a blank or starts with "-". It never quotes,
// so it has no such defect: a hostile name gives no Repro, and a name it accepts
// gives a command a shell reads as it is. This pins that, so a change to
// PlainWords cannot open the same hole without a test failing.
func TestAReproNeverCarriesAFolderNameAShellWouldReadWrongly(t *testing.T) {
	stepshell.Available(t)
	tool := ToolSpec{Source: ToolSourceBinary}
	var names []string
	for _, f := range append(append([]hostileFolder{}, storyFolders...), moreFolders...) {
		names = append(names, f.dir)
	}
	for _, f := range controlFolders {
		names = append(names, f.dir)
	}
	names = append(names, "-x", "-x $(id)", "charts/app", "charts/a_b+c=d", "git@host:o/r", "a=b", "a^b", "a%b", "a^", "^a", "%a", "=a", "a~")
	for _, name := range names {
		repro, _ := renderRepro(tool, "helm", "template", name)
		if repro == "" {
			continue
		}
		res := stepshell.Run(t, repro, "helm")
		want := []string{"template", name}
		if res.Pwned || !reflect.DeepEqual(res.Args, want) {
			t.Errorf("Repro %q for the folder %q: arguments %q (pwned %v), want %q", repro, name, res.Args, res.Pwned, want)
		}
	}
	if repro, _ := renderRepro(tool, "helm", "template", "charts/app"); repro != "helm template charts/app" {
		t.Errorf("Repro for an ordinary folder = %q, want %q", repro, "helm template charts/app")
	}
	for _, name := range []string{"$(touch pwned)", "a b", "a'b", "x\ntouch pwned", "-x"} {
		if repro, _ := renderRepro(tool, "helm", "template", name); repro != "" {
			t.Errorf("Repro for the folder %q = %q, want none", name, repro)
		}
	}
}

// DEVDESK-772: schemaSteps names the field, the kind and the version a manifest
// holds, in a sentence that is not a command. The manifest is the repository's, so
// none of them may break the line or carry something a terminal acts on.
func TestSchemaStepsStayOnOneLineForAHostileFieldKindOrVersion(t *testing.T) {
	hostile := []string{"a\ntouch pwned", "a\r\ntouch pwned", "a\x1b[2Jb", "a\u202eb", "a\u2028b", "a\xffb", "a\x00b", "a\tb"}
	for _, h := range hostile {
		cases := map[string]struct {
			r kubeconformResource
			p kubeconformFieldProblem
		}{
			"path": {kubeconformResource{Kind: "Deployment", Version: "apps/v1"}, kubeconformFieldProblem{Path: "/spec/" + h, Msg: "invalid"}},
			"additional property": {kubeconformResource{Kind: "Deployment", Version: "apps/v1"},
				kubeconformFieldProblem{Path: "/spec/", Msg: "additional properties '" + h + "' not allowed"}},
			"kind":    {kubeconformResource{Kind: h, Version: "apps/v1"}, kubeconformFieldProblem{Path: "/spec/replicas", Msg: "invalid"}},
			"version": {kubeconformResource{Kind: "Deployment", Version: h}, kubeconformFieldProblem{Path: "/spec/replicas", Msg: "invalid"}},
		}
		for name, c := range cases {
			for _, rendered := range []string{"", "helm", "kustomize"} {
				steps := schemaSteps(c.r, c.p, "1.36.0", rendered)
				for _, s := range steps {
					if notShown(s) {
						t.Errorf("%s %q: step %q holds a line break, a control character or something that does not print", name, h, s)
					}
				}
				for _, p := range stepProblems(steps) {
					t.Errorf("%s %q: %s", name, h, p)
				}
			}
		}
	}
	// An ordinary manifest keeps its text.
	got := schemaSteps(kubeconformResource{Kind: "Deployment", Version: "apps/v1"}, kubeconformFieldProblem{Path: "/spec/replicas", Msg: "invalid"}, "1.36.0", "")
	if want := "Correct /spec/replicas so it matches the apps/v1 Deployment schema of Kubernetes 1.36.0."; len(got) != 1 || got[0] != want {
		t.Errorf("steps = %q, want %q", got, want)
	}
}
