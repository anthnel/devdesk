package viewer

import "testing"

func TestLevelsAreReadFromJSONLogfmtAndBareText(t *testing.T) {
	cases := []struct {
		name string
		line string
		want Level
	}{
		{"json level", `{"ts":"2026-01-01","level":"error","msg":"boom"}`, LevelError},
		{"json severity", `{"severity":"WARNING","msg":"slow"}`, LevelWarn},
		{"json lvl", `{"lvl":"debug"}`, LevelDebug},
		{"logfmt", `ts=2026-01-01 level=warn msg="disk almost full"`, LevelWarn},
		{"logfmt quoted", `level="error" msg=nope`, LevelError},
		{"bracketed", `2026-01-01 [ERROR] connection refused`, LevelError},
		{"logrus", `ERRO[0001] could not connect`, LevelError},
		{"bare info", `2026-01-01T10:00:00Z INFO  started`, LevelInfo},
		{"fatal folds into error", `[FATAL] giving up`, LevelError},
		{"panic folds into error", `PANIC: nil map`, LevelError},
		{"nothing", `just a line of output`, LevelUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectLevel(tc.line); got != tc.want {
				t.Errorf("detectLevel(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

// Lowercase is refused for bare text on purpose: "error" is a word people write
// in sentences, and treating every mention of it as a level would filter out
// lines that merely talk about one.
func TestALowercaseWordInProseIsNotALevel(t *testing.T) {
	if got := detectLevel("the request failed with an error, retrying"); got != LevelUnknown {
		t.Errorf("detectLevel = %v, want unknown — a word in prose is not a level", got)
	}
}

// A level far into the line is prose, not a field.
func TestALevelWordPastTheWindowIsIgnored(t *testing.T) {
	padding := "0123456789012345678901234567890123456789012345678901234567890123456789 "
	if got := detectLevel(padding + "ERROR"); got != LevelUnknown {
		t.Errorf("detectLevel = %v, want unknown past the credible window", got)
	}
}

// The decision the whole filter rests on: a stack trace has no level on any of
// its lines, and a filter that swallowed it would destroy exactly what the user
// opened the log to read.
func TestAnUnlevelledLineInheritsTheLineAbove(t *testing.T) {
	text := `2026-01-01 [ERROR] request failed
  at handler.go:42
  at server.go:10
  at main.go:7`

	lines := ParseLog(text)
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(lines))
	}
	for i, line := range lines {
		if line.Level != LevelError {
			t.Errorf("line %d (%q) is %v, want error by inheritance", i, line.Text, line.Level)
		}
		if !line.Level.Passes(LevelWarn) {
			t.Errorf("line %d does not survive a ≥ warn filter; the trace would be lost", i)
		}
	}
}

// Inheritance has to stop somewhere, or one ERROR colours the rest of the file.
func TestABlankLineBreaksTheInheritanceChain(t *testing.T) {
	lines := ParseLog("[ERROR] boom\n  at x.go:1\n\nunrelated output")

	if lines[1].Level != LevelError {
		t.Errorf("the trace line is %v, want error", lines[1].Level)
	}
	if lines[3].Level != LevelUnknown {
		t.Errorf("the line after the blank is %v, want unknown", lines[3].Level)
	}
}

// A line nobody could classify is never hidden. Inheritance covers the lines
// that belong to an entry above them; this covers the ones that belong to
// nothing — a banner, a bare command echo, the first lines of a file.
func TestAnUnknownLineSurvivesEveryFilter(t *testing.T) {
	for _, min := range Levels {
		if !LevelUnknown.Passes(min) {
			t.Errorf("an unlevelled line is hidden at ≥ %v", min)
		}
	}
}

func TestVerbosityIsMonotone(t *testing.T) {
	cases := []struct {
		line Level
		min  Level
		want bool
	}{
		{LevelError, LevelWarn, true},
		{LevelWarn, LevelWarn, true},
		{LevelInfo, LevelWarn, false},
		{LevelDebug, LevelWarn, false},
		{LevelInfo, LevelUnknown, true}, // no filter at all
		{LevelTrace, LevelUnknown, true},
	}
	for _, tc := range cases {
		if got := tc.line.Passes(tc.min); got != tc.want {
			t.Errorf("%v.Passes(%v) = %v, want %v", tc.line, tc.min, got, tc.want)
		}
	}
}

func TestParseLogKeepsTheTextItWasGiven(t *testing.T) {
	text := "first\nsecond\nthird"
	lines := ParseLog(text)
	for i, want := range []string{"first", "second", "third"} {
		if lines[i].Text != want {
			t.Errorf("line %d = %q, want %q", i, lines[i].Text, want)
		}
	}
}
