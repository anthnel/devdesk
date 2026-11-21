package main

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthnel/devdesk/internal/config"
)

type safeBuf struct {
	mu sync.Mutex
	sb strings.Builder
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sb.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sb.String()
}

// colorModel renders one coloured word, enough to see which profile the
// program writes.
type colorModel struct{}

func (colorModel) Init() tea.Cmd                         { return nil }
func (m colorModel) Update(tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (colorModel) View() tea.View {
	return tea.NewView(lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8800")).Render("hello"))
}

// runAt runs a real tea.Program with opts until its first frame shows, and
// returns what it wrote.
func runAt(t *testing.T, opts ...tea.ProgramOption) string {
	t.Helper()
	out := &safeBuf{}
	opts = append([]tea.ProgramOption{
		tea.WithInput(nil), tea.WithOutput(out), tea.WithWindowSize(40, 5), tea.WithoutSignalHandler(),
	}, opts...)
	p := tea.NewProgram(colorModel{}, opts...)
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(out.String(), "hello") && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		p.Quit()
	}()
	if _, err := p.Run(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// Windows Terminal under WSL does not propagate COLORTERM, so main forces
// TrueColor there; a forced profile bypasses the detection that honours
// NO_COLOR, so NO_COLOR must switch the forcing off (DEVDESK-201, H4).
// Each forcing is applied to a real program and the output is read.
func TestForcedColorProfile(t *testing.T) {
	for _, tc := range []struct {
		name      string
		env       map[string]string
		wantForce bool
	}{
		{"windows terminal", map[string]string{"WT_SESSION": "1"}, true},
		{"windows terminal with COLORTERM already set", map[string]string{"WT_SESSION": "1", "COLORTERM": "truecolor"}, true},
		{"windows terminal with NO_COLOR", map[string]string{"WT_SESSION": "1", "NO_COLOR": "1"}, false},
		{"plain terminal", map[string]string{}, false},
		{"COLORTERM alone is not forced", map[string]string{"COLORTERM": "truecolor"}, false},
		{"NO_COLOR alone", map[string]string{"NO_COLOR": "1"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, forced := forcedColorProfile(func(k string) string { return tc.env[k] })
			if forced != tc.wantForce {
				t.Fatalf("forced = %v, want %v", forced, tc.wantForce)
			}
			if !forced {
				return
			}
			if got := runAt(t, tea.WithColorProfile(p)); !strings.Contains(got, "38;2;255;136;0") {
				t.Errorf("the forced profile did not give TrueColor output: %q", got)
			}
		})
	}
}

// DEVDESK-314: ensureDefaultContext creates config.yaml with the price example
// on a first launch, and reports a failure in the log instead of stopping.
func TestEnsureDefaultContextCreatesTheFileWithTheBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ensureDefaultContext()
	path, err := config.GetContextPath("default")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config.yaml was not created: %v", err)
	}
	if !strings.Contains(string(data), config.ExamplePricesBlock) {
		t.Errorf("config.yaml lacks the price example block:\n%s", data)
	}
}

func TestEnsureDefaultContextLogsAWarnWhenItCannotWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// A file where the config directory should be makes the creation fail.
	if err := os.WriteFile(filepath.Join(home, ".devdesk"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	ensureDefaultContext()
	if !strings.Contains(buf.String(), "WARN [config] could not create the default context file") {
		t.Errorf("log = %q, want the WARN line", buf.String())
	}
}

// DEVDESK-663 (S-01) criterion 3. main loads the config before
// ensureDefaultContext, so on a first launch the session would run on
// Default() (no registry) and its first config.Save would write config.yaml
// without the Docker Hub entry. ensureDefaultContext reports `created`, and
// reloadOnFirstLaunch rereads the config only in that case.

func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestEnsureDefaultContextReportsWhetherItCreatedTheFile(t *testing.T) {
	isolatedHome(t)
	if !ensureDefaultContext() {
		t.Fatal("first launch: ensureDefaultContext() = false, want true (the file was created)")
	}
	if ensureDefaultContext() {
		t.Error("second launch: ensureDefaultContext() = true, want false (the file was already there)")
	}
}

func TestEnsureDefaultContextReportsFalseWhenItCannotWrite(t *testing.T) {
	home := isolatedHome(t)
	// A file where the config directory should be makes the creation fail.
	if err := os.WriteFile(filepath.Join(home, ".devdesk"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	log.SetOutput(&bytes.Buffer{})
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	if ensureDefaultContext() {
		t.Error("ensureDefaultContext() = true after a failed write, want false")
	}
}

func TestReloadOnFirstLaunch(t *testing.T) {
	before := config.Default()
	reloaded := config.Default()
	reloaded.Registry.Registries = []config.RegistryItem{config.DefaultHubRegistry()}
	boom := errors.New("read failed")

	cases := []struct {
		name      string
		created   bool
		load      func() (*config.Config, error)
		want      *config.Config
		wantCalls int
	}{
		{"created: the config is read again", true, func() (*config.Config, error) { return reloaded, nil }, reloaded, 1},
		{"context already there: nothing is read", false, func() (*config.Config, error) { return reloaded, nil }, before, 0},
		{"created but the reread fails: the session keeps what it had", true, func() (*config.Config, error) { return nil, boom }, before, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			load := func() (*config.Config, error) { calls++; return tc.load() }
			if got := reloadOnFirstLaunch(before, tc.created, load); got != tc.want {
				t.Errorf("returned %p, want %p", got, tc.want)
			}
			if calls != tc.wantCalls {
				t.Errorf("load called %d times, want %d", calls, tc.wantCalls)
			}
		})
	}
}

// The startup sequence of runTUI, end to end on a throwaway HOME: Load, then
// sessionConfig (ensureDefaultContext and the reload, the very call runTUI
// makes), then a first Save.
func TestFirstLaunchSessionSeesTheHubEntryAndSavesIt(t *testing.T) {
	isolatedHome(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Registry.Registries) != 0 {
		t.Fatalf("precondition: Load before the file exists gave %+v, want the Default() fallback", cfg.Registry.Registries)
	}

	cfg = sessionConfig(cfg, ensureDefaultContext, config.Load)

	if got := cfg.Registry.Registries; len(got) != 1 || got[0] != config.DefaultHubRegistry() {
		t.Fatalf("the session config registries = %+v, want exactly [%+v] (:oci must show Docker Hub without a restart)",
			got, config.DefaultHubRegistry())
	}

	// The first save of the session must not drop the entry.
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := saved.Registry.Registries; len(got) != 1 || got[0] != config.DefaultHubRegistry() {
		t.Errorf("config.yaml registries after the first Save = %+v, want exactly [%+v]", got, config.DefaultHubRegistry())
	}
}

func TestLaunchOnAnExistingContextReadsAndAddsNothing(t *testing.T) {
	home := isolatedHome(t)
	dir := filepath.Join(home, ".devdesk")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path, err := config.GetContextPath("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("app:\n  theme: dark\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}

	created := false
	ensure := func() bool { created = ensureDefaultContext(); return created }
	calls := 0
	got := sessionConfig(cfg, ensure, func() (*config.Config, error) { calls++; return config.Load() })

	if created {
		t.Error("ensureDefaultContext() = true on an existing config.yaml")
	}
	if calls != 0 || got != cfg {
		t.Errorf("an existing context was reread (%d loads) or replaced", calls)
	}
	if len(got.Registry.Registries) != 0 {
		t.Errorf("registries = %+v, want none: an existing context is never given the Docker Hub entry", got.Registry.Registries)
	}
	if err := config.Save(got); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "docker.io") {
		t.Errorf("a Save on an existing context wrote docker.io:\n%s", data)
	}
}
