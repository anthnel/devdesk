package configuration

import (
	"strings"
	"testing"
)

// DEVDESK-88 step 9 (Rule 114): the Contexts help section documents the whole
// :ctx vocabulary — switch, previous, explicit creation, completion.

func contextsSection(t *testing.T) string {
	t.Helper()
	for _, s := range newModel(t).GetHelpContent().Sections {
		if s.Title == "Contexts" {
			return s.Body
		}
	}
	t.Fatal("help has no Contexts section")
	return ""
}

func TestHelpContextsDocumentsTheCtxCommand(t *testing.T) {
	body := contextsSection(t)
	for _, want := range []string{
		":ctx <name>",
		":ctx -",
		"previous",
		":ctx new <name>",
		"tab",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Contexts help lacks %q:\n%s", want, body)
		}
	}
}

func TestHelpContextsNoLongerSuggestsImplicitCreation(t *testing.T) {
	body := contextsSection(t)
	if strings.Contains(body, "Switch with :context <name>") {
		t.Errorf("Contexts help still has the old hint:\n%s", body)
	}
	low := strings.ToLower(body)
	if !strings.Contains(low, "no longer creat") && !strings.Contains(low, "does not create") &&
		!strings.Contains(low, "never creates") && !strings.Contains(low, "doesn't create") {
		t.Errorf("Contexts help must say :ctx <unknown> does not create a context:\n%s", body)
	}
}
