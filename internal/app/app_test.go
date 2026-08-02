package app

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

func TestBuildShortcutLines(t *testing.T) {
	tests := []struct {
		name  string
		input shortcut.Shortcuts
	}{
		{"empty", nil},
		{"single column", []shortcut.Shortcut{
			{Key: "q", Description: "quit"},
			{Key: "n", Description: "new monitor"},
			{Key: "d", Description: "delete monitor"},
		}},
		{"two columns", []shortcut.Shortcut{
			{Key: "q", Description: "quit"},
			{Key: "n", Description: "new monitor"},
			{Key: "d", Description: "delete monitor"},
			{Key: "e", Description: "edit monitor"},
			{Key: "r", Description: "refresh"},
			{Key: "+/-", Description: "adjust refresh interval"},
			{Key: "space", Description: "pause/resume"},
			{Key: "tab", Description: "switch tab"},
			{Key: "↑↓", Description: "navigate"},
			{Key: ":", Description: "command mode"},
			{Key: "?", Description: "help"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildShortcutLines(tt.input, 60)
			if len(result) != headerMinHeight {
				t.Errorf("buildShortcutLines() returned %d lines, want %d", len(result), headerMinHeight)
			}
		})
	}
}
