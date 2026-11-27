package docker

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/engine"
)

// OC-06 (DEVDESK-803), guard G3 at run time, requirements S1 and S2: a function
// that builds a destructive argv (stop, rm, kill, rmi, prune, volume rm, network
// rm) from an identifier validates it first. A malformed one returns an error
// before anything is built or run; a valid one is one element of the argv, and
// nothing else.
//
// The static half, "these verbs are built only in internal/docker and
// internal/kind", is TestDestructiveVerbsStayInTheirPackage in
// internal/engineguard.
//
// Every test goes through stubStrict with no declared argv for the refusals: the
// stub fails the test on any Build or Run, and the real engine is never reached.
// The seams are global, so none of them calls t.Parallel.
//
// What the code offers (identifier.go and containers.go):
//
//	var ErrInvalidContainerID = errors.New("invalid container ID")
//	var ErrInvalidImageID     = errors.New("invalid image ID")
//	var ErrInvalidVolumeName  = errors.New("invalid volume name")
//	var ErrInvalidNetworkID   = errors.New("invalid network ID") // an ID or a name
//
//	func IsContainerID(s string) bool
//	func IsImageID(s string) bool // an optional "sha256:" prefix, then hex
//	func IsVolumeName(s string) bool
//	func IsNetworkRef(s string) bool
//
// StopContainer, RemoveContainer, RemoveImage, RemoveVolume and RemoveNetwork
// keep their signatures; each returns the error wrapped with %w, before it
// builds its argv.

// idKind is the sort of identifier a destructive function takes.
type idKind string

const (
	kindContainer idKind = "container"
	kindImage     idKind = "image"
	kindVolume    idKind = "volume"
	kindNetwork   idKind = "network"
)

// badID is an identifier that must be refused, and why it is in the table.
type badID struct {
	label string
	id    string
}

// The strings every identifier kind refuses: the five of the story, then the
// other ways an option or a second argument gets into an argv.
var commonBadIDs = []badID{
	{"shell metacharacters", "abc; rm -rf ~"},
	{"a short option", "-f"},
	{"empty", ""},
	{"a glob", "*"},
	{"an embedded space", "has space"},
	{"a long option", "--all"},
	{"command substitution", "$(id)"},
	{"a trailing newline", "0123456789ab\n"},
	{"a leading dash on an otherwise valid ID", "-0123456789ab"},
	{"a pipe", "0123456789ab | sh"},
	{"Arabic-Indic digits, not ASCII", "٣٣٣٣٣٣٣٣٣٣٣٣"},
	{"fullwidth digits and a letter, not ASCII", "０１２３４５６７８９ab"},
	{"a non-ASCII byte after a valid ID", "0123456789ab\u00e9"},
}

// badIDs are the identifiers each kind refuses: the common ones, and what only
// that kind refuses.
var badIDs = map[idKind][]badID{
	kindContainer: append(slices.Clone(commonBadIDs),
		badID{"uppercase hex", "0123456789AB"},
		badID{"too short", "0123456789a"},
		badID{"too long", strings.Repeat("a", 65)},
		badID{"a name, not an ID", "web"},
	),
	kindImage: append(slices.Clone(commonBadIDs),
		badID{"the prefix alone", "sha256:"},
		badID{"the prefix and an option", "sha256:-f"},
		badID{"the prefix twice", "sha256:sha256:0123456789ab"},
		badID{"a reference, not an ID", "nginx:latest"},
		badID{"uppercase hex", "0123456789AB"},
		badID{"too short", "0123456789a"},
		badID{"too long", strings.Repeat("a", 65)},
	),
	kindVolume: append(slices.Clone(commonBadIDs),
		badID{"a path", "../data"},
		badID{"a leading dash", "-pgdata"},
		badID{"a slash", "a/b"},
		badID{"a colon", "a:b"},
	),
	kindNetwork: append(slices.Clone(commonBadIDs),
		badID{"a path", "../data"},
		badID{"a leading dash", "-backend"},
		badID{"a slash", "a/b"},
		badID{"a colon", "a:b"},
	),
}

// goodIDs are valid identifiers: each is one argv element.
var goodIDs = map[idKind][]string{
	kindContainer: {"0123456789ab", strings.Repeat("ab", 32)},
	kindImage:     {"0123456789ab", strings.Repeat("ab", 32), "sha256:0123456789ab"},
	kindVolume:    {"pgdata", "my-vol_1.x", "a1", strings.Repeat("ab", 32)},
	kindNetwork: {"backend", "my-net_1.x", "0123456789ab", strings.Repeat("ab", 32),
		"proj_default", "proj-default", "podman", "bridge"},
}

// destructiveByIDCase is one destructive function, called one way.
type destructiveByIDCase struct {
	name string // the exported function, as TestEveryDestructiveFunctionIsCovered reads it
	kind idKind
	// argv is the argv the function builds for id, engine-independent.
	argv func(id string) []string
	call func(id string) error
	// wantIs is the sentinel the refusal wraps, always set: a refusal that is an
	// error but not this one (a failure from further down, say) fails the test.
	wantIs error
}

// label names the case in a subtest: the function and the argv words before the
// identifier. There is one case per function since OC-22: RemoveContainer and
// RemoveImage lost their force option, so `rm -f` and `rmi -f` are no longer argv
// any function builds.
func (c destructiveByIDCase) label() string {
	argv := c.argv("ID")
	return c.name + "_" + strings.Join(argv[:len(argv)-1], "_")
}

// destructiveByID is every function of the package that builds a destructive argv
// from an identifier, as the tests call it.
var destructiveByID = []destructiveByIDCase{
	{"StopContainer", kindContainer,
		func(id string) []string { return []string{"stop", id} },
		StopContainer, ErrInvalidContainerID},
	{"RemoveContainer", kindContainer,
		func(id string) []string { return []string{"rm", id} },
		RemoveContainer, ErrInvalidContainerID},
	{"RemoveImage", kindImage,
		func(id string) []string { return []string{"rmi", id} },
		RemoveImage, ErrInvalidImageID},
	{"RemoveVolume", kindVolume,
		func(id string) []string { return []string{"volume", "rm", id} },
		RemoveVolume, ErrInvalidVolumeName},
	{"RemoveNetwork", kindNetwork,
		func(id string) []string { return []string{"network", "rm", id} },
		RemoveNetwork, ErrInvalidNetworkID},
}

// destructiveWithoutID are the destructive functions that take no identifier:
// nothing to validate, and the argv is fixed. Pinned here so that none grows an
// argument unnoticed.
var destructiveWithoutID = []struct {
	name string
	argv []string
	call func() (string, error)
}{
	{"PruneNetworks", []string{"network", "prune", "-f"}, PruneNetworks},
}

// Criterion 2: a malformed identifier is refused before any argv is built, by
// every destructive function, under both engines. The strict stub has no declared
// argv, so any Build or Run fails the test and names the argv; the journal is
// checked as well, so that a call that slipped through reads as such.
func TestDestructiveFunctionsRefuseAMalformedID(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, fn := range destructiveByID {
			for _, bad := range badIDs[fn.kind] {
				t.Run(fn.label()+"/"+bad.label, func(t *testing.T) {
					s := stubStrict(t)

					err := fn.call(bad.id)

					if err == nil {
						t.Errorf("%s(%q) = nil, want an error: %q is not a valid %s identifier", fn.name, bad.id, bad.id, fn.kind)
					}
					if fn.wantIs != nil && !errors.Is(err, fn.wantIs) {
						t.Errorf("%s(%q) = %v, want it to wrap %v", fn.name, bad.id, err, fn.wantIs)
					}
					if len(s.calls) != 0 {
						t.Errorf("%s(%q) reached the runner with %q, want no argv built for a malformed identifier",
							fn.name, bad.id, s.lastArgs())
					}
				})
			}
		}
	})
}

// Criterion 3: a valid identifier is one element of the argv, in its place, with
// nothing glued to it, under both engines. The strict stub declares the whole argv
// and nothing else, so the words, their order and their number are all checked;
// the element holding the identifier is read back from the journal too.
func TestDestructiveFunctionsPassAValidIDAsOneArgvElement(t *testing.T) {
	forEachEngine(t, func(t *testing.T, shape engine.Shape) {
		for _, fn := range destructiveByID {
			for _, id := range goodIDs[fn.kind] {
				t.Run(fn.label()+"/"+id[:min(len(id), 14)], func(t *testing.T) {
					want := fn.argv(id)
					s := stubStrict(t, reply{argv: want})

					if err := fn.call(id); err != nil {
						t.Fatalf("%s(%q) = %v, want nil on the %s engine", fn.name, id, err, shape.Name)
					}

					if len(s.calls) != 1 {
						t.Fatalf("%s(%q) ran %d invocations, want exactly one", fn.name, id, len(s.calls))
					}
					got := s.calls[0].Args
					if !slices.Equal(got, want) {
						t.Errorf("%s(%q) argv = %q, want %q", fn.name, id, got, want)
					}
					if n := countElement(got, id); n != 1 {
						t.Errorf("%s(%q): the identifier is %d element(s) of %q, want one", fn.name, id, n, got)
					}
					if got[len(got)-1] != id {
						t.Errorf("%s(%q): the identifier is not the last element of %q", fn.name, id, got)
					}
					if !s.calls[0].Combined {
						t.Errorf("%s(%q) did not ask for combined output, so the engine's diagnostic would be lost", fn.name, id)
					}
				})
			}
		}
	})
}

func countElement(argv []string, s string) int {
	n := 0
	for _, a := range argv {
		if a == s {
			n++
		}
	}
	return n
}

// The prune functions take no identifier: their argv is fixed, and they refuse
// nothing, so what is pinned is the argv under both engines.
func TestPruneFunctionsBuildAFixedArgv(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, fn := range destructiveWithoutID {
			t.Run(fn.name, func(t *testing.T) {
				stubStrict(t, reply{argv: fn.argv, out: "Total reclaimed space: 0B\n"})

				out, err := fn.call()

				if err != nil {
					t.Fatalf("%s() = %v", fn.name, err)
				}
				if out != "Total reclaimed space: 0B" {
					t.Errorf("%s() output = %q, want the engine's report trimmed", fn.name, out)
				}
			})
		}
	})
}

// A refusal wraps the sentinel with %w and names the offending value, so the
// footer says what was refused (Rule 128) and a caller can match it with errors.Is.
func TestARefusedIDIsNamedInTheError(t *testing.T) {
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		stubStrict(t)

		err := StopContainer("abc; rm -rf ~")

		if err == nil {
			t.Fatal("StopContainer accepted a malformed ID")
		}
		if !errors.Is(err, ErrInvalidContainerID) {
			t.Errorf("error = %v, want it to wrap ErrInvalidContainerID", err)
		}
		if !strings.Contains(err.Error(), `"abc; rm -rf ~"`) {
			t.Errorf("error = %q, want it to quote the refused value", err)
		}
	})
}

// The value a refusal quotes is escaped: a terminal control sequence or a
// direction override in an identifier reaches the log as text, never as bytes
// that the terminal would act on (%q).
func TestARefusedIDIsEscapedInTheError(t *testing.T) {
	const raw = "\x1b[31m\u202eabc"
	forEachEngine(t, func(t *testing.T, _ engine.Shape) {
		for _, fn := range destructiveByID {
			t.Run(fn.label(), func(t *testing.T) {
				stubStrict(t)

				err := fn.call(raw)

				if err == nil {
					t.Fatalf("%s accepted %q", fn.name, raw)
				}
				msg := err.Error()
				if strings.ContainsRune(msg, 0x1b) || strings.ContainsRune(msg, 0x202e) {
					t.Errorf("error = %q, want the ESC and U+202E bytes escaped", msg)
				}
				if !strings.Contains(msg, `\x1b[31m\u202eabc`) {
					t.Errorf("error = %q, want the escaped value", msg)
				}
			})
		}
	})
}

// "All" in criterion 2 has to be checked, not hoped for: the exported functions
// of the package whose body spells a destructive subcommand are read from the
// source, and each one must be in a table above. A new KillContainer, or a
// RemoveX that nobody listed, fails here by name instead of escaping the
// invariant.
//
// The check reads words, not types: a destructive subcommand built in an unexported
// helper, from a variable or by appending word by word is not seen (the static
// guard in internal/engineguard has the same limit).
func TestEveryDestructiveFunctionIsCovered(t *testing.T) {
	got := exportedDestructiveFunctions(t)

	want := map[string]bool{}
	for _, fn := range destructiveByID {
		want[fn.name] = true
	}
	for _, fn := range destructiveWithoutID {
		want[fn.name] = true
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("%s builds a destructive subcommand but is in neither destructiveByID nor destructiveWithoutID: "+
				"add it, with the invalid identifiers it must refuse", name)
		}
	}
	for name := range want {
		if !slices.Contains(got, name) {
			t.Errorf("%s is in the destructive tables but no longer spells a destructive subcommand: delete the entry", name)
		}
	}
}

// destructiveWords are the literals that spell a destructive subcommand, alone
// or after a group word.
var destructiveWords = []string{"kill", "rm", "remove", "rmi", "stop", "prune"}

// exportedDestructiveFunctions returns the sorted names of the exported functions
// of this package (the files in the working directory, tests excluded) whose body
// holds one of destructiveWords as a string literal.
func exportedDestructiveFunctions(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package: %v", err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, e.Name(), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", e.Name(), err)
		}
		files = append(files, f)
	}
	if len(files) < 10 {
		t.Fatalf("read %d files of package docker, want the package", len(files))
	}
	seen := map[string]bool{}
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if s, err := strconv.Unquote(lit.Value); err == nil && slices.Contains(destructiveWords, s) {
					seen[fn.Name.Name] = true
				}
				return true
			})
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
