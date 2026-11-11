package ociresources

import (
	"reflect"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthnel/devdesk/internal/shared"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-210 step 7: the periodic refresh is skipped while the terminal is
// blurred, one re-read on the focus edge.

func isolateFocusTest(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir()) // no docker binary: the fetches fail fast
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
}

// collect runs cmd (flattening batches) and returns what arrives in window.
func collect(cmd tea.Cmd, window time.Duration) []tea.Msg {
	var (
		mu  sync.Mutex
		out []tea.Msg
		run func(tea.Cmd)
	)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			msg := c()
			if batch, ok := msg.(tea.BatchMsg); ok {
				for _, sub := range batch {
					run(sub)
				}
				return
			}
			if msg == nil {
				return
			}
			mu.Lock()
			out = append(out, msg)
			mu.Unlock()
		}()
	}
	run(cmd)
	time.Sleep(window)
	mu.Lock()
	defer mu.Unlock()
	return append([]tea.Msg(nil), out...)
}

func hasType[T any](msgs []tea.Msg) bool {
	for _, m := range msgs {
		if _, ok := m.(T); ok {
			return true
		}
	}
	return false
}

func types(msgs []tea.Msg) []string {
	var out []string
	for _, m := range msgs {
		out = append(out, reflect.TypeOf(m).String())
	}
	return out
}

func anyFetch(msgs []tea.Msg) bool {
	return hasType[ImagesListMsg](msgs) || hasType[ScanCacheLoadedMsg](msgs) ||
		hasType[NetworksListMsg](msgs) || hasType[VolumesListMsg](msgs)
}

func TestBlurredRefreshTickSkipsTheFetches(t *testing.T) {
	isolateFocusTest(t)
	testutil.FastTimers(t, &refreshInterval) // a var, not a const, once this lands
	m := feed(t, newTestModel(t), shared.TerminalFocusMsg{Focused: false})

	_, cmd := m.Update(RefreshTickMsg(time.Now()))
	msgs := collect(cmd, 500*time.Millisecond)

	if anyFetch(msgs) {
		t.Errorf("blurred RefreshTickMsg fetched; got %v", types(msgs))
	}
	if !hasType[RefreshTickMsg](msgs) {
		t.Errorf("blurred RefreshTickMsg dropped its timer; got %v", types(msgs))
	}
}

func TestRefreshTickStillFetchesWithoutAFocusMessage(t *testing.T) {
	isolateFocusTest(t)
	testutil.FastTimers(t, &refreshInterval)
	m := newTestModel(t)

	_, cmd := m.Update(RefreshTickMsg(time.Now()))
	msgs := collect(cmd, 500*time.Millisecond)

	for name, ok := range map[string]bool{
		"ImagesListMsg":      hasType[ImagesListMsg](msgs),
		"ScanCacheLoadedMsg": hasType[ScanCacheLoadedMsg](msgs),
		"NetworksListMsg":    hasType[NetworksListMsg](msgs),
		"VolumesListMsg":     hasType[VolumesListMsg](msgs),
	} {
		if !ok {
			t.Errorf("a never-blurred view stopped refreshing: no %s; got %v", name, types(msgs))
		}
	}
	if !hasType[RefreshTickMsg](msgs) {
		t.Errorf("a focused RefreshTickMsg did not re-arm its timer; got %v", types(msgs))
	}
}

func TestRefreshTickAfterResumeFetchesAgain(t *testing.T) {
	isolateFocusTest(t)
	testutil.FastTimers(t, &refreshInterval)
	m := feed(t, newTestModel(t), shared.TerminalFocusMsg{Focused: false})
	m = feed(t, m, shared.TerminalFocusMsg{Focused: true, Resume: true})

	_, cmd := m.Update(RefreshTickMsg(time.Now()))
	msgs := collect(cmd, 500*time.Millisecond)

	if !anyFetch(msgs) {
		t.Errorf("the tick after a resume did not re-read; got %v", types(msgs))
	}
	if !hasType[RefreshTickMsg](msgs) {
		t.Errorf("the tick after a resume did not re-arm; got %v", types(msgs))
	}
}

func TestFocusResumeRefreshesOnceWithoutANewTimer(t *testing.T) {
	isolateFocusTest(t)
	testutil.FastTimers(t, &refreshInterval)
	m := feed(t, newTestModel(t), shared.TerminalFocusMsg{Focused: false})

	_, cmd := m.Update(shared.TerminalFocusMsg{Focused: true, Resume: true})
	if cmd == nil {
		t.Fatal("a resuming TerminalFocusMsg returned no Cmd")
	}
	msgs := collect(cmd, 500*time.Millisecond)

	for name, ok := range map[string]bool{
		"ImagesListMsg":      hasType[ImagesListMsg](msgs),
		"ScanCacheLoadedMsg": hasType[ScanCacheLoadedMsg](msgs),
		"NetworksListMsg":    hasType[NetworksListMsg](msgs),
		"VolumesListMsg":     hasType[VolumesListMsg](msgs),
	} {
		if !ok {
			t.Errorf("resume did not re-read: no %s; got %v", name, types(msgs))
		}
	}
	if hasType[RefreshTickMsg](msgs) {
		t.Errorf("resume started a second tick chain; got %v", types(msgs))
	}
}

func TestFocusResumeRunsNoScan(t *testing.T) {
	// Rule 126: the re-read lists and loads the cache, it never scans.
	isolateFocusTest(t)
	m := feed(t, loadedModel(t), shared.TerminalFocusMsg{Focused: false})

	_, cmd := m.Update(shared.TerminalFocusMsg{Focused: true, Resume: true})
	for _, msg := range collect(cmd, 500*time.Millisecond) {
		if name := reflect.TypeOf(msg).String(); !isReadMsg(msg) {
			t.Errorf("resume produced %s, only listing/cache messages are allowed", name)
		}
	}
}

func isReadMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case ImagesListMsg, ScanCacheLoadedMsg, NetworksListMsg, VolumesListMsg:
		return true
	}
	return false
}

func TestFocusLevelWithoutResumeStartsNothing(t *testing.T) {
	m := newTestModel(t)
	for _, msg := range []shared.TerminalFocusMsg{
		{Focused: false, Resume: false},
		{Focused: true, Resume: false}, // entry level
	} {
		if _, cmd := m.Update(msg); cmd != nil {
			t.Errorf("%+v returned a Cmd, want nil (flag only)", msg)
		}
	}
}
