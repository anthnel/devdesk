package cache

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// OC-17 (DEVDESK-815, S9): launch-options.json keeps the names of the
// environment variables of a launch and never a value. A file written by an
// earlier build holds values: reading it drops them and writes the file back
// without them.

// canary is the value no byte of the cache file may ever hold.
const canary = "canary-7f3a91"

// isolatedHome points HOME and USERPROFILE at a directory of the test's own
// (the package-wide HOME is shared, DEVDESK-36) and returns the cache file path.
func isolatedHome(t *testing.T) (home, file string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home, filepath.Join(home, ".devdesk", "cache", "launch-options.json")
}

func writeCacheFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating the cache directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("writing the cache file: %v", err)
	}
	// WriteFile keeps the mode of an existing file and applies the umask to a new one.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod: %v", err)
	}
}

func readCacheFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the cache file: %v", err)
	}
	return string(data)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return // Unix permission bits do not apply
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s has mode %o, want %o", path, got, want)
	}
}

// capturedLog redirects the standard logger for one test.
func capturedLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &buf
}

// legacyFile is what a build before OC-17 wrote: the whole Environment field,
// values included, under "env".
const legacyFile = `{
  "api:v1": {
    "entrypoint": "/bin/sh",
    "extra_ports": "",
    "env": "FOO=bar, API_TOKEN=` + canary + `",
    "volumes": "",
    "user": "",
    "network": "devdesk",
    "remove": false,
    "detach": true,
    "interactive": false,
    "port_mappings": {"80/tcp": {"host_port": "8080", "enabled": true}}
  },
  "web:v2": {"env": "REGION=eu"}
}`

// Criterion 2: a cache from an earlier version loses its values on the read,
// the file is rewritten without them in 0600, and the names stay.
func TestAnOlderCacheLosesItsValuesOnTheReadAndIsRewritten(t *testing.T) {
	_, file := isolatedHome(t)
	writeCacheFile(t, file, legacyFile, 0o644)

	c, err := NewLaunchOptionsCache()
	if err != nil {
		t.Fatalf("NewLaunchOptionsCache: %v", err)
	}

	body := readCacheFile(t, file)
	for _, leaked := range []string{canary, "bar", "eu"} {
		if strings.Contains(body, leaked) {
			t.Errorf("the rewritten file still holds %q:\n%s", leaked, body)
		}
	}
	for _, kept := range []string{"FOO", "API_TOKEN", "REGION", "/bin/sh", "devdesk", "8080"} {
		if !strings.Contains(body, kept) {
			t.Errorf("the rewritten file lost %q:\n%s", kept, body)
		}
	}
	assertMode(t, file, 0o600)

	got := c.Get("api:v1")
	if got == nil {
		t.Fatal("api:v1 is gone from the cache")
	}
	if got.Entrypoint != "/bin/sh" || got.Network != "devdesk" || !got.Detach {
		t.Errorf("the other fields were not kept: %+v", got)
	}
	if pm := got.PortMappings["80/tcp"]; pm.HostPort != "8080" || !pm.Enabled {
		t.Errorf("the port mapping was not kept: %+v", got.PortMappings)
	}
	if blob, _ := json.Marshal(got); strings.Contains(string(blob), canary) {
		t.Errorf("the entry in memory still holds the value: %s", blob)
	}
}

// The rewrite is permanent: a second open reads the same names from the new
// file and has nothing left to migrate.
func TestAMigratedCacheIsStableOnTheNextOpen(t *testing.T) {
	_, file := isolatedHome(t)
	writeCacheFile(t, file, legacyFile, 0o600)
	if _, err := NewLaunchOptionsCache(); err != nil {
		t.Fatalf("first open: %v", err)
	}
	first := readCacheFile(t, file)

	if _, err := NewLaunchOptionsCache(); err != nil {
		t.Fatalf("second open: %v", err)
	}

	if second := readCacheFile(t, file); second != first {
		t.Errorf("the second open changed the file:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// A legacy "env" that is not NAME=VALUE (a token pasted with no name, or with
// only padding after the equals sign) has no name to keep: nothing of it stays,
// because its first characters would be the secret itself.
func TestALegacyEntryWithNoNameKeepsNothingOfIt(t *testing.T) {
	_, file := isolatedHome(t)
	writeCacheFile(t, file, `{"api:v1": {"env": "ghp_`+canary+`, abcd`+canary+`==, OK=1"}}`, 0o600)

	if _, err := NewLaunchOptionsCache(); err != nil {
		t.Fatalf("NewLaunchOptionsCache: %v", err)
	}

	body := readCacheFile(t, file)
	if strings.Contains(body, canary) {
		t.Errorf("a token with no name survived the migration:\n%s", body)
	}
	if !strings.Contains(body, "OK") {
		t.Errorf("the well-formed name was lost:\n%s", body)
	}
}

// Criterion 4: a corrupt file is ignored, the cache opens empty, and the log
// holds a fixed sentence: neither a byte of the file nor the parser's own error
// (it quotes the byte it stopped on).
func TestACorruptCacheIsIgnoredAndItsContentIsNeverLogged(t *testing.T) {
	_, file := isolatedHome(t)
	corrupt := `{"api:v1": {"env": "API_TOKEN=` + canary + `", "detach": tru`
	writeCacheFile(t, file, corrupt, 0o600)
	logged := capturedLog(t)

	c, err := NewLaunchOptionsCache()
	if err != nil {
		t.Fatalf("a corrupt file must not fail the open: %v", err)
	}

	if got := c.Get("api:v1"); got != nil {
		t.Errorf("Get = %+v, want nothing from a corrupt file", got)
	}
	out := logged.String()
	if out == "" {
		t.Error("nothing was logged about the corrupt file")
	}
	for _, leaked := range []string{canary, "API_TOKEN", "invalid character", "tru", file} {
		if strings.Contains(out, leaked) {
			t.Errorf("the log holds %q: %q", leaked, out)
		}
	}
}

// A launch saved after a corrupt read replaces the file with a valid one.
func TestALaunchSavedAfterACorruptReadReplacesTheFile(t *testing.T) {
	_, file := isolatedHome(t)
	writeCacheFile(t, file, `{"api:v1": {"env": "API_TOKEN=`+canary+`"`, 0o600)
	capturedLog(t)
	c, err := NewLaunchOptionsCache()
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Set("api:v1", LaunchOptionsEntry{Detach: true}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	body := readCacheFile(t, file)
	if strings.Contains(body, canary) {
		t.Errorf("the new file still holds the corrupt content:\n%s", body)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Errorf("the new file is not valid JSON: %v", err)
	}
}

// Criterion 1, directory half: DevDesk creates the directory, so it is 0700; the
// file is 0600. Nothing is created by a cache that is only opened and never saved
// beyond the directory itself.
func TestTheCacheDirectoryDevDeskCreatesIsPrivate(t *testing.T) {
	home, file := isolatedHome(t)

	c, err := NewLaunchOptionsCache()
	if err != nil {
		t.Fatalf("NewLaunchOptionsCache: %v", err)
	}
	if err := c.Set("api:v1", LaunchOptionsEntry{Detach: true}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	assertMode(t, filepath.Dir(file), 0o700)
	assertMode(t, filepath.Join(home, ".devdesk"), 0o700)
	assertMode(t, file, 0o600)
}

// A file that already exists with a wider mode comes back as 0600 after a save.
func TestASaveTightensTheModeOfAnExistingFile(t *testing.T) {
	_, file := isolatedHome(t)
	writeCacheFile(t, file, `{}`, 0o644)
	c, err := NewLaunchOptionsCache()
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Set("api:v1", LaunchOptionsEntry{Detach: true}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	assertMode(t, file, 0o600)
}

func TestEnvNamesKeepsTheNameOfEachNameValueEntryOnly(t *testing.T) {
	tests := []struct {
		name    string
		entries []string
		want    []string
	}{
		{"nothing", nil, nil},
		{"two entries", []string{"FOO=bar", "API_TOKEN=" + canary}, []string{"FOO", "API_TOKEN"}},
		{"the first equals sign ends the name", []string{"DSN=a=b"}, []string{"DSN"}},
		{"an empty value keeps its name", []string{"EMPTY="}, []string{"EMPTY"}},
		{"blank entries and spaces", []string{"  ", " A=1 "}, []string{"A"}},
		{"duplicates once", []string{"A=1", "A=2"}, []string{"A"}},
		{"no equals sign: nothing to keep", []string{"ghp_" + canary}, nil},
		{"only equals signs after the first: a token", []string{"abcd=="}, nil},
		{"no name", []string{"=" + canary}, nil},
		{"an invalid name is kept as typed", []string{"BAD NAME=x"}, []string{"BAD NAME"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EnvNames(tt.entries)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("EnvNames(%q) = %q, want %q", tt.entries, got, tt.want)
			}
		})
	}
}

// Whatever a caller puts in EnvNames, a value cannot reach the file.
func TestSetWritesNoValueEvenFromAMalformedEntry(t *testing.T) {
	_, file := isolatedHome(t)
	c, err := NewLaunchOptionsCache()
	if err != nil {
		t.Fatal(err)
	}

	if err := c.Set("api:v1", LaunchOptionsEntry{EnvNames: []string{"FOO=" + canary, "BAR"}}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	body := readCacheFile(t, file)
	if strings.Contains(body, canary) {
		t.Errorf("a value reached the file:\n%s", body)
	}
	if got := c.Get("api:v1").EnvNames; strings.Join(got, "|") != "FOO|BAR" {
		t.Errorf("EnvNames = %q, want FOO and BAR", got)
	}
}
