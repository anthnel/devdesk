package ociresources

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/ui/indicator"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// DEVDESK-146 step 3: a scan or pull failure of an image comes from the
// router's memory (m.failures) and from nowhere else. The Scanned cell no
// longer has an error branch, and a successful removal tells the router to
// forget the tag.

// A failed ImageScanFinishedMsg alone leaves no trace in the view: without the
// router's snapshot the row neither fails nor loses its last date.
func TestAScanFinishedWithAnErrorSetsNoLocalFailure(t *testing.T) {
	m := loadedModel(t)
	before := scannedCell(imageRowByName(t, m, imgRef))
	m = feed(t, m, ImageScanFinishedMsg{ImageName: imgRef, Err: errors.New("trivy: exit status 1")})

	row := imageRowByName(t, m, imgRef)
	if got := m.rowActivity(row).State; got == indicator.Failed {
		t.Errorf("the row failed with nothing in the router's memory: %v", got)
	}
	if got := scannedCell(row); got != before || strings.Contains(got, "error") {
		t.Errorf("Scanned cell = %q after a failed scan, want the last date %q", got, before)
	}
	if want := timeAgo(row.Entry.ScannedAt); before != want || scannedCell(row) != want {
		t.Errorf("Scanned cell = %q, want exactly the last date %q", scannedCell(row), want)
	}
	if len(m.failures) != 0 {
		t.Errorf("m.failures = %+v, want empty: the only source of a failure is the router's snapshot", m.failures)
	}
}

// With the failure in the snapshot the dot is red and the Scanned cell still
// reads the last successful date.
func TestAFailedImageKeepsItsLastScanDate(t *testing.T) {
	f := []jobs.Failure{ociFailure(jobs.KindScan, ociCtx, imgRef, time.Now())}
	m := ociSnapshot(t, ociCtx, nil, f)
	row := imageRowByName(t, m, imgRef)

	if got := row.Activity.State; got != indicator.Failed {
		t.Fatalf("activity = %v, want Failed", got)
	}
	got := scannedCell(row)
	if strings.Contains(got, "error") || got != timeAgo(row.Entry.ScannedAt) {
		t.Errorf("Scanned cell = %q, want %q", got, timeAgo(row.Entry.ScannedAt))
	}
	if style := scannedStyle(row); style.GetForeground() == theme.StatusErrorStyle.GetForeground() {
		t.Errorf("Scanned cell still painted as an error")
	}
}

func TestScannedCellHasNoErrorBranch(t *testing.T) {
	at := time.Now().Add(-2 * time.Hour)
	row := imageRow{Scanned: true, Entry: scanCacheFixture()[imgRef]}
	row.Entry.ScannedAt = at
	row.Activity.State = indicator.Failed
	if got := scannedCell(row); got != timeAgo(at) {
		t.Errorf("scannedCell = %q, want %q", got, timeAgo(at))
	}
	row.Scanned = false
	if got := scannedCell(row); got != "-" {
		t.Errorf("unscanned failed row = %q, want -", got)
	}
}

func forgotten(msgs []tea.Msg) []jobs.ForgetTargetMsg {
	var out []jobs.ForgetTargetMsg
	for _, msg := range msgs {
		if f, ok := msg.(jobs.ForgetTargetMsg); ok {
			out = append(out, f)
		}
	}
	return out
}

// A removal that succeeded asks the router to forget the row's raw tag, once.
func TestASuccessfulRemovalForgetsTheTag(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // the reload that follows must not reach docker
	m := loadedModel(t)
	img := imageRowByName(t, m, imgRef)

	_, cmd := step(t, m, ImageActionMsg{Action: "remove", ID: img.Image.ID, Name: img.RawName})
	got := forgotten(testutil.Msgs(cmd))
	if len(got) != 1 || got[0].Target != imgRef {
		t.Errorf("forget messages = %+v, want one for %q", got, imgRef)
	}
}

// A failed removal keeps the memory and says why at the footer.
func TestAFailedRemovalDoesNotForgetTheTag(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	m := loadedModel(t)
	img := imageRowByName(t, m, imgRef)

	m, cmd := step(t, m, ImageActionMsg{Action: "remove", ID: img.Image.ID, Name: img.RawName, Err: errors.New("in use")})
	if got := forgotten(testutil.Msgs(cmd)); len(got) != 0 {
		t.Errorf("a failed removal asked to forget: %+v", got)
	}
	if !m.footer.IsSet() {
		t.Error("the removal error is not at the footer")
	}
}
