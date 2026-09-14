package workspaces

import "testing"

func TestMatchFuzzySubsequence(t *testing.T) {
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
		_, got := matchFuzzy(tt.candidate, tt.query)
		if got != tt.want {
			t.Errorf("matchFuzzy(%q, %q) matched = %v, want %v", tt.candidate, tt.query, got, tt.want)
		}
	}
}

func TestMatchFuzzyCaseInsensitive(t *testing.T) {
	if _, ok := matchFuzzy("DevDesk", "dvd"); !ok {
		t.Error("matchFuzzy should ignore case")
	}
}

// A run of consecutive matched characters is a tighter match than the same
// letters scattered across the candidate, and should score higher.
func TestMatchFuzzyScoresConsecutiveRunsHigher(t *testing.T) {
	consecutive, ok := matchFuzzy("abcxyz", "abc")
	if !ok {
		t.Fatal("expected a match")
	}
	scattered, ok := matchFuzzy("axbxcx", "abc")
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
func TestMatchFuzzyScoresSegmentBoundaryHigher(t *testing.T) {
	boundary, ok := matchFuzzy("x/bar", "bar")
	if !ok {
		t.Fatal("expected a match")
	}
	midword, ok := matchFuzzy("xxbar", "bar")
	if !ok {
		t.Fatal("expected a match")
	}
	if boundary <= midword {
		t.Errorf("boundary score = %d, mid-word score = %d, want boundary higher", boundary, midword)
	}
}
