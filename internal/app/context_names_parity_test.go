package app

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/contextcmd"
)

// DEVDESK-88: contextcmd.ValidName mirrors config.ValidateContextName without
// importing it; this test fails only if the two rules drift apart.

var parityNames = []string{
	"work", "default", "a1", "0", "-", "a-b", "a--b", "-a", "a-", "new", "list",
	"Work", "WORK", "a/b", "a\\b", "..", ".", "", " ", "a b", "a_b", "é", "café",
	"日本", "a\x00b", "a\nb", "work.yaml", strings.Repeat("a", 300), strings.Repeat("-", 64),
}

func TestContextNameRulesAgree(t *testing.T) {
	for _, n := range parityNames {
		if got, want := contextcmd.ValidName(n), config.ValidateContextName(n) == nil; got != want {
			t.Errorf("name %q: contextcmd.ValidName = %v, config.ValidateContextName ok = %v", n, got, want)
		}
	}
}

func TestReservedWordsAgree(t *testing.T) {
	reserved := map[string]bool{contextcmd.WordNew: true, contextcmd.WordPrevious: true, "list": true}
	for _, n := range append([]string{"default", "work"}, parityNames...) {
		refused := config.ValidateNewContextName(n) != nil && config.ValidateContextName(n) == nil
		if refused != reserved[n] {
			t.Errorf("name %q: ValidateNewContextName refuses as reserved = %v, contextcmd reserves = %v", n, refused, reserved[n])
		}
	}
}
