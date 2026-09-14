package workspaces

import (
	"path/filepath"
	"reflect"
	"testing"
)

// jumpToPath is pure state synthesis (loadEntries' Cmd is not exercised
// here): these tests check what navigateIn would have built one step at a
// time, computed in one shot instead.
func TestJumpToPathSynthesizesDrillDownState(t *testing.T) {
	root := "/tmp/workspaces"

	tests := []struct {
		name            string
		target          string
		wantCurrentPath string
		wantStack       []string
	}{
		{
			name:            "directly under the root",
			target:          filepath.Join(root, "a"),
			wantCurrentPath: "",
			wantStack:       nil,
		},
		{
			name:            "one level nested",
			target:          filepath.Join(root, "a", "b"),
			wantCurrentPath: filepath.Join(root, "a"),
			wantStack:       []string{},
		},
		{
			name:            "two levels nested",
			target:          filepath.Join(root, "a", "b", "c"),
			wantCurrentPath: filepath.Join(root, "a", "b"),
			wantStack:       []string{filepath.Join(root, "a")},
		},
		{
			name:            "three levels nested",
			target:          filepath.Join(root, "a", "b", "c", "d"),
			wantCurrentPath: filepath.Join(root, "a", "b", "c"),
			wantStack:       []string{filepath.Join(root, "a"), filepath.Join(root, "a", "b")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t)
			next, _ := m.jumpToPath(tt.target)
			got, ok := next.(Model)
			if !ok {
				t.Fatalf("jumpToPath returned %T, want workspaces.Model", next)
			}
			if got.currentPath != tt.wantCurrentPath {
				t.Errorf("currentPath = %q, want %q", got.currentPath, tt.wantCurrentPath)
			}
			if !reflect.DeepEqual(got.navigationStack, tt.wantStack) {
				t.Errorf("navigationStack = %v, want %v", got.navigationStack, tt.wantStack)
			}
			if got.pendingSelectPath != tt.target {
				t.Errorf("pendingSelectPath = %q, want %q", got.pendingSelectPath, tt.target)
			}
			if len(got.cursorStack) != len(tt.wantStack) {
				t.Errorf("cursorStack = %v, want %d zero-valued entries", got.cursorStack, len(tt.wantStack))
			}
		})
	}
}

// A target outside the workspaces root has no ancestor chain leading back to
// it — the jump is refused rather than climbing to the filesystem root.
func TestJumpToPathRefusesATargetOutsideTheRoot(t *testing.T) {
	m := newTestModel(t)
	before := m

	next, cmd := m.jumpToPath(filepath.Join(string(filepath.Separator), "definitely-outside-the-fixture-root", "target"))
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("jumpToPath returned %T, want workspaces.Model", next)
	}
	if got.currentPath != before.currentPath || !reflect.DeepEqual(got.navigationStack, before.navigationStack) {
		t.Errorf("jumpToPath changed navigation state for a target outside the root: currentPath=%q navigationStack=%v", got.currentPath, got.navigationStack)
	}
	if cmd != nil {
		t.Error("jumpToPath returned a Cmd for a target outside the root, want nil")
	}
}
