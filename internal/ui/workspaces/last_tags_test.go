package workspaces

import "testing"

// Several tags on one commit are all shown, ", "-separated, three at most —
// the highest versions, since internal/git lists them highest first.
func TestLastTagsTextListsAtMostThreeTags(t *testing.T) {
	tests := []struct {
		tags []string
		want string
	}{
		{nil, ""},
		{[]string{"v1.0.0"}, "v1.0.0"},
		{[]string{"2.4.2", "2.4", "2"}, "2.4.2, 2.4, 2"},
		{[]string{"2.4.2", "2.4", "2", "latest"}, "2.4.2, 2.4, 2"},
	}
	for _, tc := range tests {
		if got := lastTagsText(tc.tags); got != tc.want {
			t.Errorf("lastTagsText(%q) = %q, want %q", tc.tags, got, tc.want)
		}
	}
}
