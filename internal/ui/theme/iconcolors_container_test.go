package theme

import (
	"encoding/json"
	"testing"
)

// DEVDESK-152 (Q2-A): the container icon has its own role. Its default is the
// ordinary text colour (Rule 122: the nominal state is not coloured), and it is
// overridable like the other icon roles.

func TestTheContainerRoleDefaultsToTheTextColour(t *testing.T) {
	t.Cleanup(func() { ApplyTheme(DefaultTheme()) })
	ApplyTheme(DefaultTheme())
	if got := IconColor(IconRoleContainer); got != ColorText {
		t.Errorf("IconColor(IconRoleContainer) = %v, want ColorText", got)
	}
	if got := IconStyle(IconRoleContainer).GetForeground(); got != ColorText {
		t.Errorf("IconStyle foreground = %v, want ColorText", got)
	}
	if IconRoleContainer == IconRoleImage || IconRoleContainer == "" {
		t.Errorf("IconRoleContainer = %q must be its own role", IconRoleContainer)
	}
}

func TestAThemeCanOverrideTheContainerIconColour(t *testing.T) {
	t.Cleanup(func() { ApplyTheme(DefaultTheme()) })

	custom := DefaultTheme()
	custom.IconContainer = "#ff00ff"
	ApplyTheme(custom)

	if got := IconColor(IconRoleContainer); Hex(got) != "#ff00ff" {
		t.Errorf("IconColor(container) = %v, want #ff00ff", got)
	}
	if IconColor(IconRoleImage) == IconColor(IconRoleContainer) {
		t.Error("overriding the container icon moved the image icon")
	}
}

func TestTheContainerIconKeyIsIconContainer(t *testing.T) {
	var th Theme
	if err := json.Unmarshal([]byte(`{"icon_container":"#123456"}`), &th); err != nil {
		t.Fatal(err)
	}
	if th.IconContainer != "#123456" {
		t.Errorf("IconContainer = %q after reading the icon_container key, want #123456", th.IconContainer)
	}
}
