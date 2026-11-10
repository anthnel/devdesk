package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
