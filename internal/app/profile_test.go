package app

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

// lockedBuffer is written by the program's renderer goroutine and read by the
// test while it waits for the first frame.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// The colour profile is applied at the output by the program, not by Render()
// (DEVDESK-201 step F): the same router frame is downsampled to the profile
// the program was given, and the router still asks for the alternate screen.
func TestProgramDownsamplesToItsProfile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile colorprofile.Profile
		want    string
		notWant string
	}{
		{"truecolor", colorprofile.TrueColor, "38;2;", ""},
		{"ansi256", colorprofile.ANSI256, "38;5;", "38;2;"},
		{"ascii", colorprofile.Ascii, "", "\x1b[38;"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := router(t, &fakeView{title: "view", body: "hello"})
			out := &lockedBuffer{}
			p := tea.NewProgram(a, tea.WithInput(nil), tea.WithOutput(out),
				tea.WithWindowSize(100, 30), tea.WithoutSignalHandler(), tea.WithColorProfile(tc.profile))
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
			got := out.String()
			if !strings.Contains(got, "hello") {
				t.Fatalf("no content rendered: %q", got[:min(len(got), 200)])
			}
			if !strings.Contains(got, "\x1b[?1049h") {
				t.Error("the router did not ask for the alternate screen")
			}
			if tc.want != "" && !strings.Contains(got, tc.want) {
				t.Errorf("want %q in the output", tc.want)
			}
			if tc.notWant != "" && strings.Contains(got, tc.notWant) {
				t.Errorf("did not want %q in the output", tc.notWant)
			}
		})
	}
}
