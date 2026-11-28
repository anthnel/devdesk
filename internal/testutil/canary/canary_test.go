package canary_test

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/testutil/canary"
)

// recordingT captures what AssertAbsent reports, so the helper's own failures
// can be read without failing the test that reads them.
type recordingT struct {
	testing.TB
	errors []string
}

func (r *recordingT) Helper() {}
func (r *recordingT) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}
func (r *recordingT) Error(args ...any) { r.errors = append(r.errors, fmt.Sprint(args...)) }

func TestAssertAbsentNamesTheSurfaceAndHidesTheValueFromTheExcerpt(t *testing.T) {
	rec := &recordingT{TB: t}

	canary.AssertAbsent(rec,
		canary.Static("a clean one", "nothing to see"),
		canary.Static("the log", "level=error msg=\"launch failed\" env=SECRET=canary done"),
	)

	if len(rec.errors) != 1 {
		t.Fatalf("%d failures, want exactly one (the log): %q", len(rec.errors), rec.errors)
	}
	msg := rec.errors[0]
	if !strings.HasPrefix(msg, `canary: surface "the log"`) {
		t.Errorf("the failure does not start with the surface name: %q", msg)
	}
	_, body, _ := strings.Cut(msg, "\n") // the first line is the prefix "canary: surface ..."
	if strings.Contains(strings.ReplaceAll(body, "<canary>", ""), canary.Value) {
		t.Errorf("the failure prints the value: %q", msg)
	}
	if !strings.Contains(msg, "<canary>") {
		t.Errorf("the failure shows no excerpt: %q", msg)
	}
}

func TestTheValueIsFoundInEveryVariant(t *testing.T) {
	for name, text := range map[string]string{
		"plain":            "x canary y",
		"upper case":       "x CANARY y",
		"mixed case":       "x CaNaRy y",
		"the entry":        "SECRET=canary",
		"hex":              "id 63616e617279",
		"hex upper case":   "id 63616E617279",
		"hex mixed case":   "id 63616E617279",
		"hex of capitals":  "id 43414E415259",
		"hex of mixed":     "id 43614E617259",
		"the entry in hex": "id " + strings.ToUpper(fmt.Sprintf("%x", canary.Entry)),
		"two on one line":  "canary and canary",
		"a multi-line one": "first\nsecond canary\nthird",
	} {
		if !canary.Holds(canary.Static(name, text)) {
			t.Errorf("%s: %q is not seen as holding the value", name, text)
		}
	}
	for name, text := range map[string]string{
		"empty":           "",
		"the name alone":  "SECRET",
		"a mask":          "SECRET=********",
		"a near miss":     "canar",
		"a hex near miss": "6361 6e617279",
	} {
		if canary.Holds(canary.Static(name, text)) {
			t.Errorf("%s: %q is taken for the value", name, text)
		}
	}
}

// base64 puts the value anywhere in the stream: 0, 1 or 2 bytes before it, and
// 0, 1 or 2 after it, in both alphabets, padded or not.
func TestTheValueIsFoundInBase64AtEveryAlignment(t *testing.T) {
	encodings := map[string]*base64.Encoding{
		"std": base64.StdEncoding, "raw std": base64.RawStdEncoding,
		"url": base64.URLEncoding, "raw url": base64.RawURLEncoding,
	}
	for _, plain := range []string{canary.Value, canary.Entry} {
		for _, before := range []string{"", "x", "xy", "xyz", "\xff\xfe"} {
			for _, after := range []string{"", "a", "ab", "abc", "\xfb\xff"} {
				for name, enc := range encodings {
					text := "token=" + enc.EncodeToString([]byte(before+plain+after)) + ";"
					if !canary.Holds(canary.Static(name, text)) {
						t.Errorf("%s, %q before, %q after, %q: base64 %q is not seen", name, before, after, plain, text)
					}
				}
			}
		}
	}
	for name, text := range map[string]string{
		"base64 of something else": base64.StdEncoding.EncodeToString([]byte("a secret that is not it")),
		"base64 of the name":       base64.StdEncoding.EncodeToString([]byte(canary.Name)),
	} {
		if canary.Holds(canary.Static(name, text)) {
			t.Errorf("%s: %q is taken for the value", name, text)
		}
	}
}

// strings.ToLower changes the byte length of U+0130; the excerpt is cut in the
// original text, so it neither panics nor lands off the match.
func TestAnExcerptStaysOnTheMatchWhenLowerCasingChangesTheLength(t *testing.T) {
	hits := canary.Hits(canary.Static("s", "\u0130\u0130\u0130 canary \u0130"))
	if len(hits) != 1 || hits[0].Excerpt != "\u0130\u0130\u0130 <canary> \u0130" {
		t.Errorf("hits = %+v, want one with the excerpt around the match", hits)
	}
}

func TestFilesReadsNamesAndContentsRecursively(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(nested, "clean.json"), `{"env_names":["SECRET"]}`)
	surface := canary.Files("files", root)
	if canary.Holds(surface) {
		t.Fatal("a tree with no value reports one")
	}

	mustWrite(t, filepath.Join(nested, "leak.json"), `{"env":"SECRET=canary"}`)
	hits := canary.Hits(surface)
	if len(hits) == 0 || !strings.Contains(hits[0].Label, "a/b/leak.json") {
		t.Fatalf("a leaking content is not located: %+v", hits)
	}

	mustWrite(t, filepath.Join(root, "SECRET=canary.tmp"), "harmless")
	labels := ""
	for _, h := range canary.Hits(surface) {
		labels += h.Label + ";"
	}
	if !strings.Contains(labels, "name SECRET=canary.tmp") {
		t.Errorf("a file NAMED after the value is not found: %s", labels)
	}
}

func TestAMissingRootHoldsNothing(t *testing.T) {
	surface := canary.Files("missing", filepath.Join(t.TempDir(), "absent"))
	if canary.Holds(surface) || canary.ReadError(surface) != nil {
		t.Error("a root that does not exist is reported as holding the value or as unreadable")
	}
}

// A tree that cannot be read is an error of the surface, not a pass: reported by
// AssertAbsent and AssertPresent whatever the text of the files that could be read.
func TestAnUnreadableTreeIsAnExplicitFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions do not stop this user from reading")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(locked, "inside"), "harmless")
	mustWrite(t, filepath.Join(root, "readable"), "harmless")
	mustWrite(t, filepath.Join(root, "sealed"), "harmless")
	if err := os.Chmod(filepath.Join(root, "sealed"), 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	surface := canary.Files("files", root)

	err := canary.ReadError(surface)
	if err == nil || !strings.Contains(err.Error(), "locked") || !strings.Contains(err.Error(), "sealed") {
		t.Fatalf("ReadError = %v, want both unreadable paths named", err)
	}
	probe := &recordingT{TB: t}
	canary.AssertAbsent(probe, surface)
	if len(probe.errors) != 1 || !strings.Contains(probe.errors[0], `surface "files" could not be read`) {
		t.Errorf("AssertAbsent said %q, want one explicit read failure", probe.errors)
	}
	probe = &recordingT{TB: t}
	canary.AssertPresent(probe, surface)
	if len(probe.errors) != 2 {
		t.Errorf("AssertPresent said %q, want the read failure and the absence", probe.errors)
	}

	// What could still be read is still searched.
	mustWrite(t, filepath.Join(root, "leak"), canary.Entry)
	probe = &recordingT{TB: t}
	canary.AssertAbsent(probe, surface)
	if len(probe.errors) != 2 {
		t.Errorf("AssertAbsent said %q, want the read failure and the leak", probe.errors)
	}
}

func TestAssertWiredSeesTheRootItWasGivenAndOnlyThat(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	probe := &recordingT{TB: t}
	canary.AssertWired(probe, root, canary.Files("files", root))
	if len(probe.errors) != 0 {
		t.Errorf("a surface on the right root failed: %q", probe.errors)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("the witness file was left behind: %v", entries)
	}
	probe = &recordingT{TB: t}
	canary.AssertWired(probe, root, canary.Files("files", other))
	if len(probe.errors) != 1 {
		t.Errorf("a surface on another root passed: %q", probe.errors)
	}
}

func TestSurfacesAreReadWhenAsserted(t *testing.T) {
	var s canary.Surfaces
	var rec canary.Recorder
	value := "before"
	s.Add(canary.Text("a field", func() string { return value }), rec.Surface("clipboard"))
	if got := strings.Join(s.Names(), ","); got != "a field,clipboard" {
		t.Fatalf("names = %q", got)
	}

	probe := &recordingT{TB: t}
	s.AssertAbsent(probe)
	if len(probe.errors) != 0 {
		t.Fatalf("clean surfaces failed: %q", probe.errors)
	}

	value = canary.Entry
	rec.Record("docker run -e " + canary.Name)
	rec.Record("late " + canary.Value)
	probe = &recordingT{TB: t}
	s.AssertAbsent(probe)
	if len(probe.errors) != 2 {
		t.Fatalf("%d failures after the surfaces changed, want 2 (field, clipboard): %q", len(probe.errors), probe.errors)
	}
	if !strings.Contains(probe.errors[1], "entry 1") {
		t.Errorf("the recorder does not say which write leaked: %q", probe.errors[1])
	}
}

func TestAssertPresentFailsWhenTheValueCannotBeSeen(t *testing.T) {
	probe := &recordingT{TB: t}
	canary.AssertPresent(probe, canary.Static("the child environment", "PATH=/bin"))
	if len(probe.errors) != 1 || !strings.Contains(probe.errors[0], "the child environment") {
		t.Errorf("AssertPresent said %q", probe.errors)
	}
	probe = &recordingT{TB: t}
	canary.AssertPresent(probe, canary.Static("the child environment", canary.Entry))
	if len(probe.errors) != 0 {
		t.Errorf("AssertPresent failed on a surface that holds the value: %q", probe.errors)
	}
}

func TestHomeIsolatesEveryRoot(t *testing.T) {
	home, surface := canary.Home(t)
	for _, name := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME"} {
		if got := os.Getenv(name); got != home && !strings.HasPrefix(got, home+string(filepath.Separator)) {
			t.Errorf("%s = %q, outside the test home %q", name, got, home)
		}
	}
	if canary.Holds(surface) {
		t.Error("an empty home holds the value")
	}
	mustWrite(t, filepath.Join(home, "x"), canary.Entry)
	if !canary.Holds(surface) {
		t.Error("the home surface does not see a file written under it")
	}
}

// The environment surface is a comparison with the environment when it was built.
func TestEnvironSeesWhatWasAddedOrChangedAfterItWasBuilt(t *testing.T) {
	t.Setenv("DD_TEST_AMBIENT", "SECRET=canary") // already there when the surface is built
	t.Setenv("DD_TEST_CHANGED", "harmless")
	surface := canary.Environ("the process environment")
	canary.AssertAbsent(t, surface) // an ambient entry is the machine's, not a leak

	for name, set := range map[string]func(){
		"the entry in a new variable":     func() { t.Setenv("DD_TEST_X", canary.Entry) },
		"the value alone in a new one":    func() { t.Setenv("DD_TEST_X", "canary-1") },
		"the value in a changed variable": func() { t.Setenv("DD_TEST_CHANGED", "now canary") },
		"the name SECRET":                 func() { t.Setenv(canary.Name, canary.Value) },
	} {
		t.Run(name, func(t *testing.T) {
			fresh := canary.Environ("the process environment")
			canary.AssertAbsent(t, fresh)
			set()
			if !canary.Holds(fresh) {
				t.Errorf("%s: the environment surface does not see it", name)
			}
		})
	}
}

// A test's own temporary directories carry its name, and the test is called Canary.
func TestEnvironIgnoresPathsUnderTheTemporaryDirectory(t *testing.T) {
	surface := canary.Environ("the process environment")
	t.Setenv("DD_TEST_DIR", filepath.Join(os.TempDir(), "TestCanaryTypedInTheForm", "001"))
	t.Setenv("DD_TEST_PATHS", strings.Join([]string{
		filepath.Join(os.TempDir(), "TestCanary1"), filepath.Join(os.TempDir(), "TestCanary2"),
	}, string(filepath.ListSeparator)))
	canary.AssertAbsent(t, surface)

	// A path list that also carries the value is still caught.
	t.Setenv("DD_TEST_PATHS", filepath.Join(os.TempDir(), "TestCanary1")+string(filepath.ListSeparator)+"canary")
	if !canary.Holds(surface) {
		t.Error("a value next to a temporary path is not seen")
	}
}

func TestCaptureLogSeesTheLogAndPutsTheWriterBack(t *testing.T) {
	before := log.Writer()
	var surface canary.Surface
	t.Run("capture", func(t *testing.T) {
		surface = canary.CaptureLog(t, "the log")
		log.Printf("ERROR launch failed: %s", canary.Entry)
		canary.AssertPresent(t, surface)
	})
	if log.Writer() != before {
		t.Error("CaptureLog did not put the log writer back")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
