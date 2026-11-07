package app

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/jobs"
)

// Measurement of the renderer's output cost (DEVDESK-139, criterion 8). Opt-in:
// it takes about a minute and measures wall-clock rates, so it never runs in CI.
//
//	DEVDESK_MEASURE_REDRAW=1 go test ./internal/app/ -run TestMeasureRedrawCost -v -count=1

const (
	measureWidth    = 120
	measureHeight   = 40
	measureWarmUp   = 2 * time.Second
	measureWindow   = 6 * time.Second
	measureRowWidth = measureWidth - 4
)

// countingWriter is the program's output: it counts what the renderer writes
// and keeps nothing. There is no TTY behind it.
type countingWriter struct{ n atomic.Int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n.Add(int64(len(p)))
	return len(p), nil
}

// measuredView draws `rows` lines whose first glyph depends on the animation:
// the blink phase (mode "blink") or the spinner frame (mode "spinner"). The
// state is only written from Update, which the program calls from one goroutine.
type measuredView struct {
	fakeView
	rows    int
	blink   bool // WantsBlink
	spinner bool // rows show the spinner frame, not the phase
	frame   string
	phase   bool
}

func (v *measuredView) WantsBlink() bool { return v.blink }

func (v *measuredView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if f, ok := msg.(jobs.FrameMsg); ok {
		v.frame, v.phase = f.Frame, f.Blink
	}
	return v, nil
}

func (v *measuredView) View() string {
	glyph := "○"
	if v.phase {
		glyph = "●"
	}
	if v.spinner {
		glyph = v.frame
	}
	lines := make([]string, 0, measureHeight)
	for i := 0; i < measureHeight-14; i++ {
		text := fmt.Sprintf("row %02d  some name that stays the same", i)
		if i < v.rows {
			text = glyph + " " + text
		} else {
			text = "  " + text
		}
		lines = append(lines, text+strings.Repeat(" ", max(measureRowWidth-len(text), 0)))
	}
	return strings.Join(lines, "\n")
}

// measureBytesPerSecond runs the real router under a tea.Program for the
// warm-up, then counts the bytes written during the window.
func measureBytesPerSecond(t *testing.T, view *measuredView, runningJob bool) (float64, time.Duration) {
	t.Helper()
	a := routerAt(t, view, measureWidth, measureHeight)
	out := &countingWriter{}
	p := tea.NewProgram(programModel{app: a},
		tea.WithInput(nil), tea.WithOutput(out), tea.WithoutSignalHandler())

	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	// No TTY, so the program learns the size from a message, as the renderer does.
	p.Send(tea.WindowSizeMsg{Width: measureWidth, Height: measureHeight})
	// A key press makes the router ask the view whether it wants a blink.
	p.Send(tea.KeyMsg{Type: tea.KeyDown})
	if runningJob {
		p.Send(jobs.StartMsg{Run: scanRun("default", "a")})
	}

	time.Sleep(measureWarmUp)
	start, from := out.n.Load(), time.Now()
	time.Sleep(measureWindow)
	bytes, elapsed := out.n.Load()-start, time.Since(from)

	p.Quit()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the program did not exit")
	}
	return float64(bytes) / elapsed.Seconds(), elapsed
}

// TestMeasureRedrawCost logs the bytes per second the renderer writes for N
// animated rows: 0, 1 and 20 blinking at 1 Hz, and 1 and 20 carrying the
// spinner at 10 Hz as the reference. Opt-in: DEVDESK_MEASURE_REDRAW=1.
func TestMeasureRedrawCost(t *testing.T) {
	if os.Getenv("DEVDESK_MEASURE_REDRAW") != "1" {
		t.Skip("set DEVDESK_MEASURE_REDRAW=1 to measure the renderer's output")
	}
	cases := []struct {
		mode    string
		rows    int
		hz      int
		blink   bool
		spinner bool
	}{
		{"blink", 0, 0, false, false},
		{"blink", 1, 1, true, false},
		{"blink", 20, 1, true, false},
		{"spinner", 1, 10, false, true},
		{"spinner", 20, 10, false, true},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s-%d", c.mode, c.rows), func(t *testing.T) {
			v := &measuredView{rows: c.rows, blink: c.blink, spinner: c.spinner}
			rate, elapsed := measureBytesPerSecond(t, v, c.spinner)
			t.Logf("rows=%d mode=%s rate=%dHz terminal=%dx%d window=%s bytes/s=%.0f",
				c.rows, c.mode, c.hz, measureWidth, measureHeight, elapsed.Round(time.Millisecond), rate)
		})
	}
}
