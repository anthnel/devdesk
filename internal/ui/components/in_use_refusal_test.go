package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// OC-25 (DEVDESK-823), criteria 2 to 4: the footer line of `D` on an image, a volume or a
// network that containers use. Step 3 of the plan, written with the docker layer because
// the names come from it (docker.ContainerUsage sorts them, without docker's "/").
//
// What the code is expected to offer (removal_footer.go, today a stub):
//
//	// InUseRefusal is the footer line of `D` on an item containers use. Names keep the
//	// order given (the caller sorted them), go through Printable, and are cut to 40
//	// cells with "…". Beyond three, "and N more". The caller never asks for an empty list.
//	func InUseRefusal(kind RefusalKind, item string, users []string) string
//
// The exact texts of the criteria are pinned; what they leave open is marked "proposed"
// (plan section 9, question 2: the network sentence, the singular "delete it first",
// the cut at 40 cells) and is to be validated with the techwriter.
func TestInUseRefusalSentences(t *testing.T) {
	tests := []struct {
		name  string
		kind  RefusalKind
		item  string
		users []string
		want  string
	}{
		// Criterion 2, verbatim.
		{"image, two containers", RefusalImage, "nginx:1.27", []string{"web", "api"},
			"Cannot delete nginx:1.27: used by web, api — delete them first"},
		{"image, three containers", RefusalImage, "nginx:1.27", []string{"a", "b", "c"},
			"Cannot delete nginx:1.27: used by a, b, c — delete them first"},
		{"image, four containers: and 1 more", RefusalImage, "nginx:1.27", []string{"a", "b", "c", "d"},
			"Cannot delete nginx:1.27: used by a, b, c and 1 more — delete them first"},
		{"image, five containers: and 2 more", RefusalImage, "nginx:1.27", []string{"a", "b", "c", "d", "e"},
			"Cannot delete nginx:1.27: used by a, b, c and 2 more — delete them first"},
		// Criterion 3, verbatim: no suffix.
		{"volume, one container", RefusalVolume, "pgdata", []string{"db"},
			"Cannot delete volume pgdata: used by db"},
		{"volume, four containers", RefusalVolume, "pgdata", []string{"a", "b", "c", "d"},
			"Cannot delete volume pgdata: used by a, b, c and 1 more"},
		// Criterion 4 names the containers and fixes no text: proposed.
		{"network, two containers (proposed)", RefusalNetwork, "appnet", []string{"web", "api"},
			"Cannot delete network appnet: used by web, api"},
		{"network, five containers (proposed)", RefusalNetwork, "appnet", []string{"a", "b", "c", "d", "e"},
			"Cannot delete network appnet: used by a, b, c and 2 more"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InUseRefusal(tt.kind, tt.item, tt.users); got != tt.want {
				t.Errorf("InUseRefusal() = %q, want %q", got, tt.want)
			}
		})
	}
}

// One container on an image: the sentence still names it and still ends on what to do.
// "delete it first" or "delete them first" is the techwriter's to fix (proposed: the
// singular), so only the shared parts are pinned.
func TestInUseRefusalWithOneContainerOnAnImage(t *testing.T) {
	got := InUseRefusal(RefusalImage, "nginx:1.27", []string{"web"})

	if !strings.HasPrefix(got, "Cannot delete nginx:1.27: used by web — delete ") || !strings.HasSuffix(got, " first") {
		t.Errorf("InUseRefusal() = %q, want `Cannot delete nginx:1.27: used by web — delete <it|them> first`", got)
	}
	if strings.Contains(got, "more") {
		t.Errorf("InUseRefusal() = %q, want no \"more\" for one name", got)
	}
}

// Names come from the engine: what they hold is data, never terminal control (Printable,
// as the removal confirmation does). An escape sequence is stripped, a direction override
// becomes `?`, and the item is treated the same; nothing raw reaches the footer.
func TestInUseRefusalDrawsNothingTheEngineSent(t *testing.T) {
	got := InUseRefusal(RefusalImage, "ng\u202einx:1", []string{"web\x1b[31m", "we\u202eb", "a\x07pi"})

	want := "Cannot delete ng?inx:1: used by web, we?b, a?pi — delete them first"
	if got != want {
		t.Errorf("InUseRefusal() = %q, want %q", got, want)
	}
	for _, r := range got {
		if r == 0x1b || r == 0x07 || r == 0x202e {
			t.Errorf("InUseRefusal() = %q, holds the control character %U", got, r)
		}
	}
}

// A very long name is cut, with an ellipsis, so one container cannot push the rest of
// the sentence (what to do, the other names) off the footer. Proposed: 40 cells a name.
func TestInUseRefusalCutsALongName(t *testing.T) {
	long := strings.Repeat("x", 200)

	got := InUseRefusal(RefusalImage, "nginx:1.27", []string{long, "api"})

	if strings.Contains(got, strings.Repeat("x", 41)) {
		t.Errorf("InUseRefusal() = %q, want the name cut to 40 cells", got)
	}
	if !strings.Contains(got, "…") || !strings.HasSuffix(got, ", api — delete them first") {
		t.Errorf("InUseRefusal() = %q, want an ellipsis on the long name and the rest of the sentence intact", got)
	}
	name := strings.TrimPrefix(got, "Cannot delete nginx:1.27: used by ")
	name, _, _ = strings.Cut(name, ", api")
	if w := ansi.StringWidth(name); w > 40 {
		t.Errorf("the cut name %q is %d cells wide, want at most 40", name, w)
	}
}

// No user, no refusal: the sentence would say "used by" nobody. It is empty, and the
// caller shows nothing.
func TestInUseRefusalWithNoUserIsEmpty(t *testing.T) {
	for _, kind := range []RefusalKind{RefusalImage, RefusalVolume, RefusalNetwork} {
		if got := InUseRefusal(kind, "x", nil); got != "" {
			t.Errorf("InUseRefusal(%v, nil users) = %q, want empty", kind, got)
		}
		if got := InUseRefusal(kind, "x", []string{}); got != "" {
			t.Errorf("InUseRefusal(%v, no users) = %q, want empty", kind, got)
		}
	}
}

// An anonymous volume is named by 64 hex characters: the item is cut like a name, so the
// names after it and what to do stay on the footer.
func TestInUseRefusalCutsALongItem(t *testing.T) {
	anon := strings.Repeat("ab", 32)

	got := InUseRefusal(RefusalVolume, anon, []string{"db"})

	if strings.Contains(got, strings.Repeat("ab", 21)) || !strings.Contains(got, "…") || !strings.HasSuffix(got, ": used by db") {
		t.Errorf("InUseRefusal() = %q, want the 64-character name cut with an ellipsis and `: used by db` intact", got)
	}
	item := strings.TrimSuffix(strings.TrimPrefix(got, "Cannot delete volume "), ": used by db")
	if w := ansi.StringWidth(item); w > 40 {
		t.Errorf("the cut item %q is %d cells wide, want at most 40", item, w)
	}
}
