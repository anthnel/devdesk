package theme

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// ── TimeAgo ──────────────────────────────────────────────────────────────────

func TestTimeAgo_Zero(t *testing.T) {
	if got := TimeAgo(time.Time{}); got != "" {
		t.Errorf("TimeAgo(zero) = %q, want %q", got, "")
	}
}

func TestTimeAgo_Now(t *testing.T) {
	got := TimeAgo(time.Now().Add(-10 * time.Second))
	if got != "now" {
		t.Errorf("TimeAgo(10s ago) = %q, want %q", got, "now")
	}
}

func TestTimeAgo_Minutes(t *testing.T) {
	got := TimeAgo(time.Now().Add(-30 * time.Minute))
	if got != "30 min ago" {
		t.Errorf("TimeAgo(30m ago) = %q, want %q", got, "30 min ago")
	}
}

func TestTimeAgo_Hours(t *testing.T) {
	got := TimeAgo(time.Now().Add(-5 * time.Hour))
	if got != "5 hr ago" {
		t.Errorf("TimeAgo(5h ago) = %q, want %q", got, "5 hr ago")
	}
}

func TestTimeAgo_OneDay(t *testing.T) {
	got := TimeAgo(time.Now().Add(-25 * time.Hour))
	if got != "1 day ago" {
		t.Errorf("TimeAgo(25h ago) = %q, want %q", got, "1 day ago")
	}
}

func TestTimeAgo_MultiplesDays(t *testing.T) {
	got := TimeAgo(time.Now().Add(-72 * time.Hour))
	if got != "3 days ago" {
		t.Errorf("TimeAgo(72h ago) = %q, want %q", got, "3 days ago")
	}
}

func TestTimeAgo_Months(t *testing.T) {
	got := TimeAgo(time.Now().Add(-60 * 24 * time.Hour))
	if !strings.HasSuffix(got, "mo ago") {
		t.Errorf("TimeAgo(60 days ago) = %q, want 'X mo ago'", got)
	}
}

func TestTimeAgo_Years(t *testing.T) {
	got := TimeAgo(time.Now().Add(-400 * 24 * time.Hour))
	if !strings.HasSuffix(got, "yr ago") {
		t.Errorf("TimeAgo(400 days ago) = %q, want 'X yr ago'", got)
	}
}

// ── Bg / PadWithBg / BgLine / EmptyLineBg / BgWrap ──────────────────────────

func TestBg_ReturnsString(t *testing.T) {
	result := Bg("hello")
	if result == "" {
		t.Error("Bg() returned empty string")
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("Bg('hello') should contain 'hello', got %q", result)
	}
}

func TestPadWithBg_WidthRespected(t *testing.T) {
	result := PadWithBg("hi", 20)
	// lipgloss includes ANSI codes; the visible content should fit in width
	if result == "" {
		t.Error("PadWithBg() returned empty string")
	}
}

func TestBgLine_ContainsContent(t *testing.T) {
	result := BgLine("test content", 40)
	if !strings.Contains(result, "test content") {
		t.Errorf("BgLine should contain 'test content', got %q", result)
	}
}

func TestEmptyLineBg_NotEmpty(t *testing.T) {
	result := EmptyLineBg(30)
	if result == "" {
		t.Error("EmptyLineBg() returned empty string")
	}
}

func TestBgWrap_MultiLine(t *testing.T) {
	input := "line one\nline two\nline three"
	result := BgWrap(input, 40)
	if result == "" {
		t.Error("BgWrap() returned empty string")
	}
	// Should produce multiple lines
	lines := strings.Split(result, "\n")
	if len(lines) < 3 {
		t.Errorf("BgWrap() expected at least 3 lines, got %d", len(lines))
	}
}

// ── RenderCheckbox / RenderCheckboxDisabled / RenderRadioButton ──────────────

func TestRenderCheckbox_Checked(t *testing.T) {
	result := RenderCheckbox(true, "Option A", true)
	if result == "" {
		t.Error("RenderCheckbox() returned empty string")
	}
}

func TestRenderCheckbox_Unchecked(t *testing.T) {
	result := RenderCheckbox(false, "Option B", false)
	if result == "" {
		t.Error("RenderCheckbox(unchecked) returned empty string")
	}
}

func TestRenderCheckboxDisabled(t *testing.T) {
	result := RenderCheckboxDisabled("Disabled option")
	if result == "" {
		t.Error("RenderCheckboxDisabled() returned empty string")
	}
}

func TestRenderRadioButton_Selected(t *testing.T) {
	result := RenderRadioButton(true, "Choice 1", true)
	if result == "" {
		t.Error("RenderRadioButton() returned empty string")
	}
}

func TestRenderRadioButton_Unselected(t *testing.T) {
	result := RenderRadioButton(false, "Choice 2", false)
	if result == "" {
		t.Error("RenderRadioButton(unselected) returned empty string")
	}
}

// ── RenderTabs ────────────────────────────────────────────────────────────────

func TestRenderTabs_Basic(t *testing.T) {
	tabs := []TabItem{{Label: "Tab1"}, {Label: "Tab2"}, {Label: "Tab3"}}
	result := RenderTabs(tabs, 1)
	if result == "" {
		t.Error("RenderTabs() returned empty string")
	}
	if !strings.Contains(result, "Tab1") || !strings.Contains(result, "Tab2") {
		t.Errorf("RenderTabs() should contain tab labels, got %q", result)
	}
}

func TestRenderTabs_EmptyList(t *testing.T) {
	result := RenderTabs([]TabItem{}, 0)
	// Should not panic — empty is fine
	_ = result
}

func TestRenderTabs_ActiveBeyondLen(t *testing.T) {
	// Should not panic when activeIdx >= len(tabs)
	result := RenderTabs([]TabItem{{Label: "A"}}, 5)
	_ = result
}

// ── DefaultTableStyles / TableStylesForState / TableStylesForSeverity ────────

func TestTableStylesForState_ErrorDiffersFromNormal(t *testing.T) {
	normal := TableStylesForState("normal")
	errStyle := TableStylesForState("error")
	// The selection background must differ between states
	if normal.Selected.GetBackground() == errStyle.Selected.GetBackground() {
		t.Error("TableStylesForState('error') and 'normal' should have different selection backgrounds")
	}
}

func TestTableStylesForState_UnknownFallsBack(t *testing.T) {
	// Should not panic and should return a usable style
	s := TableStylesForState("bogus")
	_ = s.Selected.Render("test")
}

func TestTableStylesForSeverity(t *testing.T) {
	severities := []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "UNKNOWN", "bogus"}
	for _, sev := range severities {
		t.Run(sev, func(t *testing.T) {
			s := TableStylesForSeverity(sev)
			_ = s.Selected.Render("test") // must not panic
		})
	}
}

func TestTableStylesForSeverity_CriticalDiffersFromLow(t *testing.T) {
	crit := TableStylesForSeverity("CRITICAL")
	low := TableStylesForSeverity("LOW")
	if crit.Selected.GetBackground() == low.Selected.GetBackground() {
		t.Error("CRITICAL and LOW severities should have different selection backgrounds")
	}
}

// ── RenderButton ─────────────────────────────────────────────────────────────

func TestRenderButton_ContainsLabel(t *testing.T) {
	for _, focused := range []bool{true, false} {
		result := RenderButton("Confirm", focused, "primary")
		if !strings.Contains(result, "Confirm") {
			t.Errorf("RenderButton(focused=%v) should contain label text, got %q", focused, result)
		}
	}
}

// ── ResponseTimeStyle ─────────────────────────────────────────────────────────

func TestResponseTimeStyle_DifferentRanges(t *testing.T) {
	fast := ResponseTimeStyle(50)   // < 100ms → OK icon
	med := ResponseTimeStyle(300)   // 100–499ms → warning icon
	slow := ResponseTimeStyle(1000) // >= 500ms → error icon

	// Each range should render to distinct output
	rf := fast.Render("")
	rm := med.Render("")
	rs := slow.Render("")
	if rf == rm {
		t.Error("fast and medium response time styles should render differently")
	}
	if rm == rs {
		t.Error("medium and slow response time styles should render differently")
	}
}

// ── StatusStyle ──────────────────────────────────────────────────────────────

func TestStatusStyle_OKDiffersFromErrorAndWarning(t *testing.T) {
	// StatusDownStyle and StatusErrorStyle intentionally share ColorError.
	// Verify the meaningful distinctions: OK vs ERROR, OK vs WARNING.
	okFg := fmt.Sprintf("%v", StatusStyle("OK").GetForeground())
	errFg := fmt.Sprintf("%v", StatusStyle("ERROR").GetForeground())
	warnFg := fmt.Sprintf("%v", StatusStyle("WARNING").GetForeground())

	if okFg == errFg {
		t.Error("StatusStyle('OK') and StatusStyle('ERROR') should have different foreground colors")
	}
	if okFg == warnFg {
		t.Error("StatusStyle('OK') and StatusStyle('WARNING') should have different foreground colors")
	}
	if errFg == warnFg {
		t.Error("StatusStyle('ERROR') and StatusStyle('WARNING') should have different foreground colors")
	}
}

// ── SpinnerMessage ────────────────────────────────────────────────────────────

func TestSpinnerMessage(t *testing.T) {
	result := SpinnerMessage("⠋", "Loading...")
	if result == "" {
		t.Error("SpinnerMessage() returned empty string")
	}
	if !strings.Contains(result, "Loading...") {
		t.Errorf("SpinnerMessage() should contain message, got %q", result)
	}
}

// ── RenderBorderTitle ─────────────────────────────────────────────────────────

func TestRenderBorderTitle(t *testing.T) {
	result := RenderBorderTitle("My Title", 40)
	if result == "" {
		t.Error("RenderBorderTitle() returned empty string")
	}
}
