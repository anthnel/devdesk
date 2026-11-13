package config

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var activePricesLine = regexp.MustCompile(`(?m)^\s*(agent_usage|prices):`)

// assertFreshBlockFile checks criterion 1 on a file written by a creation.
func assertFreshBlockFile(t *testing.T, path, name string) {
	t.Helper()
	got := readFile(t, path)
	if n := strings.Count(got, examplePricesMarker); n != 1 {
		t.Errorf("marker appears %d times, want 1", n)
	}
	for _, line := range blockLines() {
		if !strings.Contains(got, line+"\n") && !strings.HasSuffix(got, line) {
			t.Errorf("block line missing: %q", line)
		}
	}
	if activePricesLine.MatchString(got) {
		t.Errorf("an agent_usage:/prices: line is active:\n%s", got)
	}
	cfg, err := LoadContext(name)
	if err != nil {
		t.Fatalf("LoadContext(%q): %v", name, err)
	}
	if len(cfg.AgentUsage.Prices) != 0 {
		t.Errorf("prices = %v, want empty", cfg.AgentUsage.Prices)
	}
}

func pastTime(t *testing.T, path string) time.Time {
	t.Helper()
	past := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	return past
}

// ── criterion 1 ──────────────────────────────────────────────────────────────

func TestCreateContext_WritesExampleBlock(t *testing.T) {
	setupTmpHome(t)
	if err := CreateContext("dev"); err != nil {
		t.Fatal(err)
	}
	path, _ := GetContextPath("dev")
	assertFreshBlockFile(t, path, "dev")
	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("mode = %o, want 600", st.Mode().Perm())
		}
	}
}

func TestWriteNewContext_AbsentFile(t *testing.T) {
	setupTmpHome(t)
	if err := WriteNewContext(newContextConfig(), "dev"); err != nil {
		t.Fatal(err)
	}
	path, _ := GetContextPath("dev")
	assertFreshBlockFile(t, path, "dev")
}

func TestWriteNewContext_SameBytesAsCreateContext(t *testing.T) {
	setupTmpHome(t)
	if err := CreateContext("a"); err != nil {
		t.Fatal(err)
	}
	if err := WriteNewContext(newContextConfig(), "b"); err != nil {
		t.Fatal(err)
	}
	pa, _ := GetContextPath("a")
	pb, _ := GetContextPath("b")
	if readFile(t, pa) != readFile(t, pb) {
		t.Error("CreateContext and WriteNewContext wrote different bytes")
	}
}

// ── criterion 7 ──────────────────────────────────────────────────────────────

func TestWriteNewContext_ReplacesExistingContext(t *testing.T) {
	setupTmpHome(t)
	path := seedContextFile(t, "dev", plainContext(t))
	if err := WriteNewContext(newContextConfig(), "dev"); err != nil {
		t.Fatal(err)
	}
	assertFreshBlockFile(t, path, "dev")
}

func TestCreateContext_LeavesOtherContextUntouched(t *testing.T) {
	setupTmpHome(t)
	path := seedContextFile(t, "a", plainContext(t))
	past := pastTime(t, path)
	before := readFile(t, path)
	if err := CreateContext("b"); err != nil {
		t.Fatal(err)
	}
	if readFile(t, path) != before {
		t.Error("context a bytes changed")
	}
	st, _ := os.Stat(path)
	if !st.ModTime().Equal(past) {
		t.Errorf("context a mtime changed: %v", st.ModTime())
	}
}

func TestCreateContext_ExistingKeepsBytesAndMtime(t *testing.T) {
	setupTmpHome(t)
	path := seedContextFile(t, "a", []byte("not: [valid yaml\n"))
	past := pastTime(t, path)
	err := CreateContext("a")
	if err == nil || !strings.Contains(err.Error(), "already exists") || !isCtxExists(err) {
		t.Fatalf("err = %v, want ErrContextExists", err)
	}
	if readFile(t, path) != "not: [valid yaml\n" {
		t.Error("existing file modified")
	}
	st, _ := os.Stat(path)
	if !st.ModTime().Equal(past) {
		t.Error("existing file mtime changed")
	}
}

func isCtxExists(err error) bool {
	for e := err; e != nil; {
		if e == ErrContextExists {
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// ── criterion 2 ──────────────────────────────────────────────────────────────

func TestCreateDefaultContextIfMissing_Creates(t *testing.T) {
	setupTmpHome(t)
	created, err := CreateDefaultContextIfMissing()
	if err != nil || !created {
		t.Fatalf("= (%v, %v), want (true, nil)", created, err)
	}
	path, _ := GetContextPath("default")
	assertFreshBlockFile(t, path, "default")
}

func TestCreateDefaultContextIfMissing_ExistingUntouched(t *testing.T) {
	cases := map[string][]byte{
		"valid":   nil,
		"invalid": []byte("::: not yaml [\n"),
		"empty":   {},
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			setupTmpHome(t)
			if content == nil {
				content = plainContext(t)
			}
			path := seedContextFile(t, "default", content)
			past := pastTime(t, path)
			created, err := CreateDefaultContextIfMissing()
			if err != nil || created {
				t.Fatalf("= (%v, %v), want (false, nil)", created, err)
			}
			if readFile(t, path) != string(content) {
				t.Error("config.yaml bytes changed")
			}
			st, _ := os.Stat(path)
			if !st.ModTime().Equal(past) {
				t.Error("config.yaml mtime changed")
			}
		})
	}
}

func TestCreateDefaultContextIfMissing_ReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions are not enforced here")
	}
	home := setupTmpHome(t)
	dir := filepath.Join(home, ".devdesk")
	if err := os.MkdirAll(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	created, err := CreateDefaultContextIfMissing()
	if err == nil || created {
		t.Fatalf("= (%v, %v), want (false, error)", created, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("directory not empty: %v", entries)
	}
}

func TestCreateDefaultContextIfMissing_CurrentContextElsewhere(t *testing.T) {
	cases := map[string]bool{
		"other-with-file":    true,
		"other-without-file": false,
	}
	for name, withFile := range cases {
		t.Run(name, func(t *testing.T) {
			home := setupTmpHome(t)
			if withFile {
				seedContextFile(t, "other", plainContext(t))
			}
			if err := EnsureConfigDir(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, ".devdesk", ".current-context"), []byte("other\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			created, err := CreateDefaultContextIfMissing()
			if err != nil || created {
				t.Fatalf("= (%v, %v), want (false, nil)", created, err)
			}
			path, _ := GetContextPath("default")
			if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
				t.Errorf("config.yaml was created (stat err = %v)", statErr)
			}
		})
	}
}

// ── race ─────────────────────────────────────────────────────────────────────

func TestCreateDefaultContextIfMissing_Concurrent(t *testing.T) {
	setupTmpHome(t)
	const n = 8
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		trues int
		errs  []error
		start = make(chan struct{})
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			created, err := CreateDefaultContextIfMissing()
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
			}
			if created {
				trues++
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if trues != 1 {
		t.Fatalf("%d callers reported created=true, want 1", trues)
	}
	path, _ := GetContextPath("default")
	assertFreshBlockFile(t, path, "default")
}

// Not covered: removal of the file after a failed write following the
// exclusive create. It needs an injectable writer in createContextFile.
