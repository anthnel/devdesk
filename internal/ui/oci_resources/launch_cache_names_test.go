package ociresources

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
)

// OC-17 (DEVDESK-815, S9): the launch cache keeps the names of the Environment
// variables and never a value. The form reopened from it shows each name with an
// empty value and a hint to type the values again; it flags nothing at opening
// (Rule 140, OC-14 criterion 7), the refusal belongs to the submit.

const cacheCanary = "canary-7f3a91"

// cacheHome isolates HOME (DEVDESK-36) and returns the launch cache file.
func cacheHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return filepath.Join(home, ".devdesk", "cache", "launch-options.json")
}

func writeLegacyLaunchCache(t *testing.T, file, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func modeOf(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

// launchedWith types env into a fresh form and saves what a successful launch
// saves: ToCacheEntry through saveLaunchOptionsCmd.
func launchedWith(t *testing.T, env string) {
	t.Helper()
	f := NewLaunchForm("api:v1", []string{"80/tcp"}, nil, 120)
	setEnv(f, env)
	run(t, saveLaunchOptionsCmd("api:v1", f.ToCacheEntry()))
}

// reopened loads the cache for api:v1 into a new launch form, the way the
// router does when the form opens.
func reopened(t *testing.T) Model {
	t.Helper()
	m := launchingModel(t, "80/tcp")
	return feed(t, m, run(t, loadLaunchOptionsCmd("api:v1")))
}

func formText(f *LaunchForm) string { return ansi.Strip(f.View()) }

// Criterion 1: FOO=bar and API_TOKEN=canary leave the names in the file, no value
// and no trace of the canary, in a 0600 file inside a 0700 directory.
func TestTheLaunchCacheKeepsTheNamesAndNoValue(t *testing.T) {
	file := cacheHome(t)

	launchedWith(t, "FOO=bar, API_TOKEN="+cacheCanary)

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("the cache file was not written: %v", err)
	}
	body := string(data)
	for _, name := range []string{"FOO", "API_TOKEN"} {
		if !strings.Contains(body, name) {
			t.Errorf("the cache file lost the name %s:\n%s", name, body)
		}
	}
	for _, leaked := range []string{cacheCanary, "bar", "FOO=", "API_TOKEN="} {
		if strings.Contains(body, leaked) {
			t.Errorf("the cache file holds %q:\n%s", leaked, body)
		}
	}
	if runtime.GOOS != "windows" {
		if got := modeOf(t, file); got != 0o600 {
			t.Errorf("the cache file has mode %o, want 600", got)
		}
		if got := modeOf(t, filepath.Dir(file)); got != 0o700 {
			t.Errorf("the cache directory has mode %o, want 700", got)
		}
	}
}

// The entry that crosses the saving command holds no value either, whatever the
// field holds when the launch goes out.
func TestTheCacheEntryOfAFormHoldsNoEnvironmentValue(t *testing.T) {
	f := NewLaunchForm("api:v1", nil, nil, 120)
	setEnv(f, "FOO=bar, API_TOKEN="+cacheCanary+", DSN=a=b")

	blob, err := json.Marshal(f.ToCacheEntry())
	if err != nil {
		t.Fatal(err)
	}

	for _, leaked := range []string{cacheCanary, "bar", "a=b"} {
		if strings.Contains(string(blob), leaked) {
			t.Errorf("the cache entry holds %q: %s", leaked, blob)
		}
	}
}

// Criterion 3: reopened, the form lists the names with empty values, hints that
// the values are to be typed again, and flags nothing.
func TestAFormReopenedFromTheCacheHasTheNamesWithEmptyValuesAndAHint(t *testing.T) {
	cacheHome(t)
	launchedWith(t, "FOO=bar, API_TOKEN="+cacheCanary)

	m := reopened(t)

	f := m.launchForm
	if got, want := f.envEntries(), []string{"FOO=", "API_TOKEN="}; !slices.Equal(got, want) {
		t.Fatalf("Environment entries = %q, want %q", got, want)
	}
	text := strings.ToLower(formText(f))
	if !strings.Contains(text, "values are not saved") {
		t.Errorf("the form does not say the values are not saved:\n%s", formText(f))
	}
	if strings.Contains(text, strings.ToLower(cacheCanary)) {
		t.Error("the canary is on screen")
	}
	if st := m.formStatus(); st.Text != "" {
		t.Errorf("footer = {%q, %v} at opening, want nothing (Rule 140)", st.Text, st.Level)
	}
	if input, rendered := envUnderlined(f); input || rendered {
		t.Error("Environment is underlined at opening")
	}
}

// The hint is not an error and goes once nothing is left to type again.
func TestTheRetypeHintGoesOnceEveryValueIsTyped(t *testing.T) {
	cacheHome(t)
	launchedWith(t, "FOO=bar, API_TOKEN=x")
	f := reopened(t).launchForm

	setEnv(f, "FOO=bar, API_TOKEN=y")

	if strings.Contains(strings.ToLower(formText(f)), "values are not saved") {
		t.Error("the hint stays after every value was typed")
	}
	if _, ok := submitOpts(f); !ok {
		t.Errorf("the retyped form was refused: %q", footerStatus(f).Text)
	}
}

// The refusal belongs to the submit: while a value is still empty the launch is
// refused, underlined, with an Error sentence that repeats nothing.
func TestTheSubmitRefusesAnEnvironmentNameRestoredWithoutItsValue(t *testing.T) {
	cacheHome(t)
	launchedWith(t, "FOO=bar")
	m := reopened(t)

	if _, ok := submitOpts(m.launchForm); ok {
		t.Fatal("the submit went through with an empty value")
	}

	if input, _ := envUnderlined(m.launchForm); !input {
		t.Error("the field is not underlined after the refused submit")
	}
	if st := m.formStatus(); st.Level != sharedcomponents.LevelError || st.Text == "" {
		t.Errorf("footer = {%q, %v}, want an Error sentence", st.Text, st.Level)
	}
}

// Criterion 3, reserved and invalid names: kept as they are in the field, refused
// at the submit as before, flagged at no other time.
func TestAReservedOrInvalidNameRestoredFromTheCacheIsKeptAndRefusedAtTheSubmit(t *testing.T) {
	file := cacheHome(t)
	writeLegacyLaunchCache(t, file, `{"api:v1": {"env": "DOCKER_HOST=tcp://other:2375, BAD NAME=x", "detach": true}}`)

	m := reopened(t)

	f := m.launchForm
	if got, want := f.envEntries(), []string{"DOCKER_HOST=", "BAD NAME="}; !slices.Equal(got, want) {
		t.Fatalf("Environment entries = %q, want %q", got, want)
	}
	if st := m.formStatus(); st.Text != "" {
		t.Errorf("footer = %q at opening, want nothing", st.Text)
	}
	if input, rendered := envUnderlined(f); input || rendered {
		t.Error("a restored name is underlined at opening")
	}
	if _, ok := submitOpts(f); ok {
		t.Error("the submit went through with a reserved name")
	}
	if st := footerStatus(f); st.Level != sharedcomponents.LevelError {
		t.Errorf("footer level = %v after the refused submit, want Error", st.Level)
	}
}

// Criterion 2, through the form's own load command: the values of an older cache
// are gone from the form, the names are in, and the file is rewritten without
// them in 0600.
func TestAnOlderLaunchCacheIsMigratedWhenTheFormOpens(t *testing.T) {
	file := cacheHome(t)
	writeLegacyLaunchCache(t, file, `{"api:v1": {"entrypoint": "/bin/sh", "env": "FOO=bar, API_TOKEN=`+cacheCanary+`", "detach": true}}`)

	m := reopened(t)

	if got, want := m.launchForm.envEntries(), []string{"FOO=", "API_TOKEN="}; !slices.Equal(got, want) {
		t.Fatalf("Environment entries = %q, want %q", got, want)
	}
	if m.launchForm.entrypointInput.Value() != "/bin/sh" {
		t.Error("the rest of the cached options was lost")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), cacheCanary) || strings.Contains(string(data), "bar") {
		t.Errorf("the file still holds a value after the form opened:\n%s", data)
	}
	if runtime.GOOS != "windows" {
		if got := modeOf(t, file); got != 0o600 {
			t.Errorf("the rewritten file has mode %o, want 600", got)
		}
	}
}

// Criterion 4: a corrupt cache leaves an empty form, and the log says nothing of
// what the file held.
func TestACorruptLaunchCacheOpensAnEmptyFormAndLogsNoContent(t *testing.T) {
	file := cacheHome(t)
	writeLegacyLaunchCache(t, file, `{"api:v1": {"env": "API_TOKEN=`+cacheCanary+`", "detach": tr`)
	var logged bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(previous) })

	m := reopened(t)

	if got := m.launchForm.envInput.Value(); got != "" {
		t.Errorf("Environment = %q, want an empty form", got)
	}
	if st := m.formStatus(); st.Text != "" {
		t.Errorf("footer = %q, want nothing", st.Text)
	}
	for _, leaked := range []string{cacheCanary, "API_TOKEN", "invalid character"} {
		if strings.Contains(logged.String(), leaked) {
			t.Errorf("the log holds %q: %q", leaked, logged.String())
		}
	}
}
