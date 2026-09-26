package fuzzy

import "testing"

func TestMatchSubsequence(t *testing.T) {
	tests := []struct {
		candidate string
		query     string
		want      bool
	}{
		{"devdesk", "dvd", true},
		{"devdesk", "ddk", true},
		{"devdesk", "devdesk", true},
		{"devdesk", "xyz", false},
		{"devdesk", "kdd", false}, // out of order
		{"workspaces/devdesk", "wsd", true},
		{"devdesk", "", false},
	}
	for _, tt := range tests {
		_, got := Match(tt.candidate, tt.query)
		if got != tt.want {
			t.Errorf("Match(%q, %q) matched = %v, want %v", tt.candidate, tt.query, got, tt.want)
		}
	}
}

func TestMatchCaseInsensitive(t *testing.T) {
	if _, ok := Match("DevDesk", "dvd"); !ok {
		t.Error("Match should ignore case")
	}
}

// A run of consecutive matched characters is a tighter match than the same
// letters scattered across the candidate, and should score higher.
func TestMatchScoresConsecutiveRunsHigher(t *testing.T) {
	consecutive, ok := Match("abcxyz", "abc")
	if !ok {
		t.Fatal("expected a match")
	}
	scattered, ok := Match("axbxcx", "abc")
	if !ok {
		t.Fatal("expected a match")
	}
	if consecutive <= scattered {
		t.Errorf("consecutive score = %d, scattered score = %d, want consecutive higher", consecutive, scattered)
	}
}

// A match starting right after a path separator reads as "this segment",
// which is what a directory jump is usually after — it should outscore the
// same letters matched mid-segment.
func TestMatchScoresSegmentBoundaryHigher(t *testing.T) {
	boundary, ok := Match("x/bar", "bar")
	if !ok {
		t.Fatal("expected a match")
	}
	midword, ok := Match("xxbar", "bar")
	if !ok {
		t.Fatal("expected a match")
	}
	if boundary <= midword {
		t.Errorf("boundary score = %d, mid-word score = %d, want boundary higher", boundary, midword)
	}
}
