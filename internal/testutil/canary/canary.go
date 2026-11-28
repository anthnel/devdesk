// Package canary is the end-to-end proof of requirement S8/S9 of the OC plan
// (OC-23, DEVDESK-821): a value typed where a secret could be typed
// (SECRET=canary) must be nowhere it must not be. A test injects Entry at every
// door a value could come through, then hands the places it must NOT be to
// AssertAbsent. Each place is a named Surface, so a failure says which one held
// the value (the log, the cache file, the clipboard, an argv, a rendered
// screen, a file under HOME ...) and where in it.
//
// The package is a set of small parts, not a framework:
//
//   - the constants Name, Value and Entry (SECRET, canary, SECRET=canary);
//   - Surface and the constructors that cover the usual places: Text and Static
//     (a string), Lines (a list), Files (every file name and content under a
//     directory), Environ (the process environment), Recorder (what a seam such
//     as the clipboard was handed), CaptureLog (what the log package printed);
//   - Surfaces, a list a test builds up with Add: the story that adds a new
//     place that can hold a value (OC-41 layers, OC-66 kubeconfig, OC-50
//     Compose ...) adds one Surface to the list its test already has and the
//     assertion covers it;
//   - AssertAbsent for the places that must not hold the value, and
//     AssertPresent for the proof that the test CAN see it (the child's
//     environment, the revealed inspect, a deliberately leaky surface): a
//     canary test that never finds the canary anywhere proves nothing.
//
// What counts as the value: the text Value, in any letter case, its hex encoding
// in any letter case, and its base64 (standard and URL alphabets, padded or not)
// at every alignment, that is whatever bytes come before and after it in the
// encoded text (see Variants). The entry NAME=VALUE is looked for the same way.
// A value split over two writes, encrypted, or reversed is not seen; that is a
// limit, not a feature.
//
// A failure names the surface and the place in it (a file path, a line number)
// and shows the surrounding text with the value replaced by "<canary>". The
// value is a fake here; the helper keeps that habit so that reusing it with a
// real-looking secret never prints one.
//
// This package imports "testing" and belongs to _test.go files only: the guard
// TestOnlyTestsImportCanary fails on a non-test file that imports it. It
// changes the process only through t.Setenv, t.Cleanup and the log writer, so
// it does not combine with t.Parallel.
package canary

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
)

const (
	// Name is the variable name the canary is typed under. It matches the
	// default patterns of internal/secretenv (SECRET), so the inspect masks it.
	Name = "SECRET"
	// Value is the fake secret.
	Value = "canary"
	// Entry is what a user types in the launch form: NAME=VALUE.
	Entry = Name + "=" + Value
)

// excerptRunes is how much text a failure shows on each side of a hit.
const excerptRunes = 24

// Part is one piece of a surface: a file, a line, one recorded write. Label says
// which one ("" for a surface that has only one).
type Part struct {
	Label string
	Text  string
}

// Surface is a named place a value could end up. Read is called each time the
// surface is asserted, so a surface built before the work under test reads the
// state after it. It returns what it could read AND the error of what it could
// not: AssertAbsent and AssertPresent report that error as a failure of its own,
// whatever the parts say, since a surface that cannot be read must not pass for a
// clean one.
type Surface struct {
	Name string
	Read func() ([]Part, error)
}

// Static is a surface holding text that is already known.
func Static(name, text string) Surface {
	return Surface{Name: name, Read: func() ([]Part, error) { return []Part{{Text: text}}, nil }}
}

// Text is a surface read from a function: a rendered view, a footer, a string
// field of a model.
func Text(name string, read func() string) Surface {
	return Surface{Name: name, Read: func() ([]Part, error) { return []Part{{Text: read()}}, nil }}
}

// Lines is a surface made of several texts, each reported by its position: the
// words of an argv, the entries of an environment, the writes a seam received.
func Lines(name string, read func() []string) Surface {
	return Surface{Name: name, Read: func() ([]Part, error) {
		lines := read()
		parts := make([]Part, len(lines))
		for i, l := range lines {
			parts[i] = Part{Label: fmt.Sprintf("entry %d", i), Text: l}
		}
		return parts, nil
	}}
}

// Environ is the environment of this very process as a test sees it, restricted
// to what changed since the surface was built: an entry added or modified (by
// t.Setenv, os.Setenv, or code under test that does either) is searched, key and
// value, for the value. The launch hands the child the typed entries without
// setting them in DevDesk's own environment; this surface proves that stays
// true.
//
// Two things are deliberately not looked at. What was already in the environment
// when the surface was built (the machine's own variables, whatever they hold),
// and, in a changed entry, every path under os.TempDir(): a test's temporary
// directories (HOME, PATH of the fake CLI, XDG_*) are named after the test, and a
// directory called TestCanary... is not a leak. A secret that happens to be a
// path under the temporary directory is therefore not seen here; the other
// surfaces would see it where it is written.
func Environ(name string) Surface {
	before := make(map[string]bool)
	for _, entry := range os.Environ() {
		before[entry] = true
	}
	return Lines(name, func() []string {
		var out []string
		for _, entry := range os.Environ() {
			if before[entry] {
				continue
			}
			key, value, _ := strings.Cut(entry, "=")
			out = append(out, key+"="+withoutTempPaths(value))
		}
		return out
	})
}

// withoutTempPaths replaces the elements of a path list that are under the
// temporary directory by a marker.
func withoutTempPaths(value string) string {
	tmp := filepath.Clean(os.TempDir())
	elements := filepath.SplitList(value)
	if len(elements) == 0 {
		return value
	}
	for i, e := range elements {
		if filepath.IsAbs(e) && strings.HasPrefix(filepath.Clean(e), tmp+string(filepath.Separator)) {
			elements[i] = "<tmp>"
		}
	}
	return strings.Join(elements, string(filepath.ListSeparator))
}

// Files is every file under root, name AND content: a name that holds the value
// (a cache key, a temporary file named after an environment entry) leaks as
// much as a content that does. A root that does not exist holds nothing, which
// is the answer of a test that wrote nothing. A symbolic link is reported by its
// name only, never followed: the walk must not leave root. A directory or a file
// that cannot be read is an error of the surface (see Surface), reported on its
// own and naming the path; what could be read is still searched.
func Files(name, root string) Surface {
	return Surface{Name: name, Read: func() ([]Part, error) {
		var (
			parts []Part
			errs  []error
		)
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if !os.IsNotExist(err) {
					errs = append(errs, err)
				}
				return nil // keep walking: the rest of the tree is still worth reading
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				errs = append(errs, relErr)
				return nil
			}
			if rel == "." {
				return nil
			}
			rel = filepath.ToSlash(rel)
			parts = append(parts, Part{Label: "name " + rel, Text: rel})
			if !d.Type().IsRegular() {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				errs = append(errs, readErr)
				return nil
			}
			parts = append(parts, Part{Label: "content of " + rel, Text: string(body)})
			return nil
		})
		return parts, errors.Join(errs...)
	}}
}

// Recorder collects what a seam was handed: the text written to the clipboard,
// an argv word. It is safe to call from the goroutines a tea.Cmd runs in.
type Recorder struct {
	mu    sync.Mutex
	items []string
}

// Record keeps text.
func (r *Recorder) Record(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, text)
}

// Items returns what was recorded, oldest first.
func (r *Recorder) Items() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.items)
}

// Surface is the recorder as a surface.
func (r *Recorder) Surface(name string) Surface { return Lines(name, r.Items) }

// CaptureLog redirects the log package to a buffer until the test ends, then
// puts back the writer it found, and returns what was printed as a surface. The
// log is where DevDesk writes its own account of an error (a launch the engine
// refused, a cache that could not be opened).
func CaptureLog(t testing.TB, name string) Surface {
	t.Helper()
	var (
		mu  sync.Mutex
		buf bytes.Buffer
	)
	previous := log.Writer()
	log.SetOutput(lockedWriter{mu: &mu, w: &buf})
	t.Cleanup(func() { log.SetOutput(previous) })
	return Text(name, func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	})
}

type lockedWriter struct {
	mu *sync.Mutex
	w  *bytes.Buffer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// Home gives the test a home directory of its own and returns it with the
// surface that reads every file under it. HOME and USERPROFILE point at it, and
// so do the XDG base directories and the Windows profile variables, so that no
// code finds a path outside it: what DevDesk keeps under ~/.devdesk, and what
// a library keeps under the XDG directories, are all in the walk.
//
// The directory is returned too, for a test that reads one file on purpose
// (the launch cache, to prove it holds the NAME), and for AssertWired.
//
// TMPDIR, TEMP and TMP are NOT isolated: a temporary file DevDesk writes outside
// HOME is not seen by this surface. Isolating them would put the fake CLI's own
// scenario and journal (which hold the value on purpose: the engine's stderr and
// the child's environment) and every t.TempDir() of the test under a directory the
// walk reads. A story that makes DevDesk write a temporary file holding a
// value-bearing document (a kubeconfig, a Compose file) adds a Files surface on
// the directory it passes the code under test.
func Home(t testing.TB) (string, Surface) {
	t.Helper()
	// Not t.TempDir: its name holds the test's name, and a file that records its
	// own path would then hold the value for nothing.
	home, err := os.MkdirTemp("", "dd-home-")
	if err != nil {
		t.Fatalf("canary: creating a home directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	for name, value := range map[string]string{
		"HOME":            home,
		"USERPROFILE":     home,
		"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
		"XDG_CACHE_HOME":  filepath.Join(home, ".cache"),
		"XDG_DATA_HOME":   filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME":  filepath.Join(home, ".local", "state"),
		"APPDATA":         filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA":    filepath.Join(home, "AppData", "Local"),
	} {
		t.Setenv(name, value)
	}
	return home, Files("files under HOME", home)
}

// witnessName is the file AssertWired writes.
const witnessName = ".canary-witness"

// AssertWired proves that a Files surface really reads root: it writes a witness
// file holding the value under root, checks that the surface sees it, and removes
// it. A canary test whose "files under HOME" surface was built on another
// directory than the one the code under test writes to would pass for nothing.
func AssertWired(t testing.TB, root string, surface Surface) {
	t.Helper()
	path := filepath.Join(root, witnessName)
	if err := os.WriteFile(path, []byte(Entry), 0o600); err != nil {
		t.Fatalf("canary: writing the witness file: %v", err)
	}
	defer func() { _ = os.Remove(path) }()
	AssertPresent(t, surface)
}

// Surfaces is the list a test builds up. The zero value is an empty list.
type Surfaces []Surface

// Add appends surfaces to the list.
func (s *Surfaces) Add(surfaces ...Surface) { *s = append(*s, surfaces...) }

// Names lists the names of the surfaces, in order, for a test that checks which
// ones it covers.
func (s Surfaces) Names() []string {
	names := make([]string, len(s))
	for i, surface := range s {
		names[i] = surface.Name
	}
	return names
}

// AssertAbsent fails the test, once per surface, for every surface that holds
// the value or cannot be read.
func (s Surfaces) AssertAbsent(t testing.TB) {
	t.Helper()
	AssertAbsent(t, s...)
}

// matcher finds one spelling of the value.
type matcher struct {
	variant string
	re      *regexp.Regexp
}

var (
	matchersOnce sync.Once
	matcherList  []matcher
	variantList  []string
)

// initMatchers builds the spellings once.
func initMatchers() {
	add := func(variant, pattern string, fold bool) {
		if fold {
			pattern = "(?i)" + pattern
		}
		matcherList = append(matcherList, matcher{variant: variant, re: regexp.MustCompile(pattern)})
		variantList = append(variantList, variant+": "+pattern)
	}
	for _, plain := range []string{Value, Entry} {
		add("plain", regexp.QuoteMeta(plain), true)
		add("hex", hexPattern(plain), true)
		// base64 is case sensitive: the value as written and in capitals.
		seen := map[string]bool{}
		for _, written := range []string{plain, strings.ToUpper(plain)} {
			for _, spelling := range base64Alignments(written) {
				if !seen[spelling] {
					seen[spelling] = true
					add("base64", regexp.QuoteMeta(spelling), false)
				}
			}
		}
	}
}

// hexPattern matches the hex encoding of plain whatever the case of its letters
// (each letter is its lower or its upper case byte) and of the hex digits.
func hexPattern(plain string) string {
	var b strings.Builder
	for _, c := range []byte(plain) {
		lower, upper := strings.ToLower(string(c)), strings.ToUpper(string(c))
		if lower == upper {
			fmt.Fprintf(&b, "%02x", c)
			continue
		}
		fmt.Fprintf(&b, "(?:%02x|%02x)", lower[0], upper[0])
	}
	return b.String()
}

// base64Alignments returns the text that every base64 encoding of any bytes
// holding plain has in common, for the three alignments of plain in the encoded
// stream (0, 1 or 2 bytes before it) and both alphabets, without padding (a
// padded encoding holds the unpadded one). The characters of the group that
// carry bits of the bytes before or after plain are cut: they depend on those
// bytes, and the rest does not.
func base64Alignments(plain string) []string {
	var out []string
	for before := 0; before < 3; before++ {
		raw := []byte(strings.Repeat("x", before) + plain)
		lead := [3]int{0, 2, 3}[before] // characters that carry bits of the bytes before
		for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
			text := enc.EncodeToString(raw)
			if len(raw)%3 != 0 {
				text = text[:len(text)-1] // the last character carries bits of the bytes after
			}
			if spelling := text[lead:]; spelling != "" && !slices.Contains(out, spelling) {
				out = append(out, spelling)
			}
		}
	}
	return out
}

// Variants lists the spellings a surface is searched for, as "kind: pattern" (the
// hex ones and the plain ones match in any letter case).
func Variants() []string {
	matchersOnce.Do(initMatchers)
	return slices.Clone(variantList)
}

// Hit is one place a surface holds the value.
type Hit struct {
	Label   string // the Part's label
	Variant string // "plain", "hex" or "base64"
	Excerpt string // the text around it, the value replaced by <canary>
}

// Hits returns every place surface holds the value, in order. The error of a
// surface that could not be read is not a hit: see ReadError.
func Hits(surface Surface) []Hit {
	parts, _ := surface.Read()
	return partsHits(parts)
}

// ReadError returns the error of a surface that could not be read in full.
func ReadError(surface Surface) error {
	_, err := surface.Read()
	return err
}

func partsHits(parts []Part) []Hit {
	matchersOnce.Do(initMatchers)
	var hits []Hit
	for _, part := range parts {
		for _, m := range matcherList {
			for _, at := range m.re.FindAllStringIndex(part.Text, -1) {
				hits = append(hits, Hit{Label: part.Label, Variant: m.variant, Excerpt: excerpt(part.Text, at[0], at[1])})
			}
		}
	}
	return hits
}

// excerpt cuts the text around [start, end) and replaces the match by a marker.
// The offsets are byte offsets in text itself, never in a transformed copy, so a
// letter whose lower case has another length (U+0130) cannot shift them. It cuts
// on rune boundaries and flattens line breaks.
func excerpt(text string, start, end int) string {
	before, after := []rune(text[:start]), []rune(text[end:])
	if len(before) > excerptRunes {
		before = append([]rune("..."), before[len(before)-excerptRunes:]...)
	}
	if len(after) > excerptRunes {
		after = append(after[:excerptRunes:excerptRunes], []rune("...")...)
	}
	flat := strings.NewReplacer("\n", `\n`, "\r", `\r`, "\x1b", `\e`)
	return flat.Replace(string(before)) + "<canary>" + flat.Replace(string(after))
}

// Holds reports whether the surface holds the value.
func Holds(surface Surface) bool { return len(Hits(surface)) > 0 }

// AssertAbsent fails the test, once per surface, for every surface that holds
// the value, and once more for every surface that could not be read in full. The
// message starts with the surface's name.
func AssertAbsent(t testing.TB, surfaces ...Surface) {
	t.Helper()
	for _, surface := range surfaces {
		parts, readErr := surface.Read()
		if readErr != nil {
			t.Errorf("canary: surface %q could not be read in full, so it proves nothing: %v", surface.Name, readErr)
		}
		hits := partsHits(parts)
		if len(hits) == 0 {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "canary: surface %q holds the value (%d place(s)):", surface.Name, len(hits))
		for i, h := range hits {
			if i == 5 {
				fmt.Fprintf(&b, "\n  ... and %d more", len(hits)-i)
				break
			}
			where := h.Label
			if where == "" {
				where = "text"
			}
			fmt.Fprintf(&b, "\n  - in %s (%s): %s", where, h.Variant, h.Excerpt)
		}
		t.Error(b.String())
	}
}

// AssertPresent fails the test when the surface does NOT hold the value, or
// cannot be read in full. It is the proof that the test can see the value where it
// is: the environment of the child the launch started, the inspect after `m`.
func AssertPresent(t testing.TB, surface Surface) {
	t.Helper()
	parts, readErr := surface.Read()
	if readErr != nil {
		t.Errorf("canary: surface %q could not be read in full: %v", surface.Name, readErr)
	}
	if len(partsHits(parts)) == 0 {
		t.Errorf("canary: surface %q does not hold the value, so a test of its absence elsewhere proves nothing", surface.Name)
	}
}
