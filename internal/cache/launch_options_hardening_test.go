package cache

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// OC-17 hardening (DEVDESK-815): a file that cannot be read is removed, not left
// on disk with whatever values it held; the migration keeps only names a launch
// could have used; the read is bounded.

func fileGone(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

// A corrupt file (truncated by the old non-atomic write, or a mistyped field) is
// removed on the open: the cache is regenerable, the values it may hold are not.
func TestACorruptCacheFileIsRemovedOnTheOpen(t *testing.T) {
	for name, body := range map[string]string{
		"truncated":        `{"api:v1": {"env": "API_TOKEN=` + canary + `", "detach": tru`,
		"a mistyped field": `{"api:v1": {"env": "API_TOKEN=` + canary + `", "detach": "yes"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, file := isolatedHome(t)
			writeCacheFile(t, file, body, 0o600)
			logged := capturedLog(t)

			c, err := NewLaunchOptionsCache()
			if err != nil {
				t.Fatalf("NewLaunchOptionsCache: %v", err)
			}

			if !fileGone(t, file) {
				t.Errorf("the corrupt file is still on disk:\n%s", readCacheFile(t, file))
			}
			if c.Get("api:v1") != nil {
				t.Error("an entry came out of a corrupt file")
			}
			if strings.Contains(logged.String(), canary) || strings.Contains(logged.String(), "API_TOKEN") {
				t.Errorf("the log holds content of the file: %q", logged.String())
			}
			// The cache still works: the next launch writes a fresh file.
			if err := c.Set("api:v1", LaunchOptionsEntry{Detach: true}); err != nil {
				t.Fatalf("Set after the removal: %v", err)
			}
			if strings.Contains(readCacheFile(t, file), canary) {
				t.Error("the new file holds the old content")
			}
		})
	}
}

// A value written by hand in env_names is a value like any other: the file is
// rewritten without it.
func TestAValueInEnvNamesIsCutAndTheFileRewritten(t *testing.T) {
	_, file := isolatedHome(t)
	writeCacheFile(t, file, `{"api:v1": {"env_names": ["X=`+canary+`", "Y"], "detach": true}}`, 0o600)

	c, err := NewLaunchOptionsCache()
	if err != nil {
		t.Fatal(err)
	}

	if body := readCacheFile(t, file); strings.Contains(body, canary) {
		t.Errorf("the value stayed in the file:\n%s", body)
	}
	if got := c.Get("api:v1").EnvNames; !slices.Equal(got, []string{"X", "Y"}) {
		t.Errorf("EnvNames = %q, want X and Y", got)
	}
}

// The migration keeps what a launch could have named and drops the rest: spaces,
// non-ASCII, wildcards, the prefix of a known token, a name over 128 bytes.
// A reserved name stays (the submit refuses it).
func TestTheMigrationKeepsOnlyNamesTheFormAccepts(t *testing.T) {
	_, file := isolatedHome(t)
	long := strings.Repeat("N", 129)
	legacy := strings.Join([]string{
		"A=1", "TOKEN=a", "b=c", // the piece after a comma of a value is undetectable: "b" stays
		"OK.NAME-1=x", "DOCKER_HOST=tcp://other:2375",
		"BAD NAME=x", "caf\u00e9=1", "zero\u200bwidth=1", "PRE*=x", "-flag=1", "=noname",
		"ghp_" + canary + "=1", "gho_x=1", "ghu_x=1", "ghs_x=1", "github_pat_x=1",
		"glpat-x=1", "xoxb-x=1", "xoxp-x=1", "AKIAIOSFODNN7EXAMPLE=1",
		long + "=1",
	}, ",")
	blob, _ := json.Marshal(map[string]any{"api:v1": map[string]any{"env": legacy}})
	writeCacheFile(t, file, string(blob), 0o600)

	c, err := NewLaunchOptionsCache()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"A", "TOKEN", "b", "OK.NAME-1", "DOCKER_HOST"}
	if got := c.Get("api:v1").EnvNames; !slices.Equal(got, want) {
		t.Errorf("EnvNames = %q, want %q", got, want)
	}
	body := readCacheFile(t, file)
	for _, leaked := range []string{canary, "ghp_", "AKIA", "github_pat_", "glpat-", "xox", "BAD NAME", "width", long} {
		if strings.Contains(body, leaked) {
			t.Errorf("the migrated file holds %q:\n%s", leaked, body)
		}
	}
}

// A name that is on screen at 128 bytes is kept: the limit is a bound, not a cut.
func TestTheMigrationKeepsANameOfExactlyTheMaximumLength(t *testing.T) {
	_, file := isolatedHome(t)
	name := strings.Repeat("N", 128)
	writeCacheFile(t, file, `{"api:v1": {"env": "`+name+`=1"}}`, 0o600)

	c, err := NewLaunchOptionsCache()
	if err != nil {
		t.Fatal(err)
	}

	if got := c.Get("api:v1").EnvNames; !slices.Equal(got, []string{name}) {
		t.Errorf("EnvNames = %q, want the 128-byte name", got)
	}
}

// The read is bounded: a file over 1 MiB is treated as corrupt and removed.
func TestAnOversizedCacheFileIsRemovedUnread(t *testing.T) {
	_, file := isolatedHome(t)
	body := `{"api:v1": {"env": "API_TOKEN=` + canary + `", "entrypoint": "` + strings.Repeat("x", maxLaunchCacheBytes) + `"}}`
	writeCacheFile(t, file, body, 0o600)
	logged := capturedLog(t)

	c, err := NewLaunchOptionsCache()
	if err != nil {
		t.Fatal(err)
	}

	if !fileGone(t, file) {
		t.Error("the oversized file is still on disk")
	}
	if c.Get("api:v1") != nil {
		t.Error("an oversized file was read")
	}
	if strings.Contains(logged.String(), canary) {
		t.Errorf("the log holds content: %q", logged.String())
	}
}

// A file just under the bound is read as usual.
func TestACacheFileUnderTheBoundIsRead(t *testing.T) {
	_, file := isolatedHome(t)
	writeCacheFile(t, file, `{"api:v1": {"entrypoint": "`+strings.Repeat("x", maxLaunchCacheBytes-1024)+`"}}`, 0o600)

	c, err := NewLaunchOptionsCache()
	if err != nil {
		t.Fatal(err)
	}

	if c.Get("api:v1") == nil {
		t.Error("a file under the bound was not read")
	}
}
