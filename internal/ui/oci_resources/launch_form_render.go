package ociresources

import (
	"strings"

	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// shellQuote wraps value in single quotes if it contains characters that would
// be interpreted by a shell, making the clipboard-copied command paste-safe.
func shellQuote(v string) string {
	if strings.ContainsAny(v, " \t\"'`$\\!;|&<>(){}") {
		return "'" + strings.ReplaceAll(v, "'", "'\\''") + "'"
	}
	return v
}

// envRestoredHint replaces the Environment hint while a name restored from the
// cache still has no value: the cache keeps names only (OC-17).
const envRestoredHint = "Values are not saved: type them again"

// invalidEnvMarker stands in the preview for an Environment entry that is not
// NAME=VALUE with a valid name (see envEntryName). The entry may be a secret typed
// in the name position, so it is never repeated. In a POSIX shell the marker is a
// redirection and PowerShell refuses `<`, so a pasted command fails instead of
// running without the variable.
const invalidEnvMarker = "<invalid>"

// previewEnvName returns what the preview and the ctrl+y copy show for one
// Environment entry: the name alone, as the launcher passes it (`-e NAME`, the
// value travels in the child's environment, OC-14), whatever the variable is.
// Only a well-formed entry (envEntryName: NAME=VALUE, a valid name, a value) is
// named. Anything else is shown as invalidEnvMarker, because it may be a secret
// pasted into the field (`ghp_...`, `abc==`) and the launch refuses it anyway.
func previewEnvName(entry string) string {
	if name, ok := envEntryName(entry); ok {
		return name
	}
	return invalidEnvMarker
}

// buildCommandLines returns the docker run command split into continuation lines.
// Environment values are never part of it: see previewEnvName.
func (f *LaunchForm) buildCommandLines() []string {
	var args []string

	if f.optionRemove {
		args = append(args, "--rm")
	}
	if f.optionDetach {
		args = append(args, "-d")
	}
	if f.optionInteractiveTTY {
		args = append(args, "-it")
	}
	if name := strings.TrimSpace(f.nameInput.Value()); name != "" {
		args = append(args, "--name "+shellQuote(name))
	}
	for i := range f.ports {
		if f.ports[i].enabled {
			args = append(args, "-p "+docker.LoopbackPort(f.ports[i].mappingArg()))
		}
	}
	for p := range strings.SplitSeq(f.extraPortsInput.Value(), ",") {
		if p = strings.TrimSpace(p); p != "" {
			args = append(args, "-p "+docker.LoopbackPort(p))
		}
	}
	if ep := strings.TrimSpace(f.entrypointInput.Value()); ep != "" {
		args = append(args, "--entrypoint "+shellQuote(ep))
	}
	for _, e := range f.envEntries() {
		args = append(args, "-e "+previewEnvName(e))
	}
	volumes, mounts := splitVolumeField(f.volInput.Value())
	for _, v := range volumes {
		args = append(args, "-v "+shellQuote(v))
	}
	for _, v := range mounts {
		args = append(args, "--mount "+shellQuote(v))
	}
	if len(f.networks) > 0 {
		args = append(args, "--network "+shellQuote(f.networks[f.networkIdx]))
	}
	if user := strings.TrimSpace(f.userInput.Value()); user != "" {
		args = append(args, "--user "+shellQuote(user))
	}

	lines := make([]string, 0, len(args)+2)
	lines = append(lines, "docker run \\")
	for _, a := range args {
		lines = append(lines, "  "+a+" \\")
	}
	// Last continuation line: remove trailing " \" and append image
	if len(lines) > 1 {
		last := lines[len(lines)-1]
		lines[len(lines)-1] = strings.TrimSuffix(last, " \\")
	}
	lines = append(lines, "  "+f.image)
	return lines
}

// buildCommandString returns the full docker run command as a single line for clipboard copy.
func (f *LaunchForm) buildCommandString() string {
	lines := f.buildCommandLines()
	// Strip continuation backslashes and join with spaces
	var parts []string
	for _, line := range lines {
		trimmed := strings.TrimSuffix(strings.TrimSpace(line), "\\")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return strings.Join(parts, " ")
}

// commandPreviewLines returns the dim-styled command lines for the right column
func (f *LaunchForm) commandPreviewLines() []string {
	cmdLines := f.buildCommandLines()
	result := make([]string, 0, len(cmdLines))
	for _, line := range cmdLines {
		result = append(result, theme.DimStyle.Render(line))
	}
	return result
}

// View renders the launch form, with a live command preview on the right when wide enough
func (f *LaunchForm) View() string {
	formContent := f.renderFormContent()
	// Show two-column layout only when there is enough horizontal space
	if f.width < 100 {
		return formContent
	}
	return f.renderTwoColumn(formContent)
}

// renderTwoColumn builds a side-by-side view: form on the left, command preview on the right
func (f *LaunchForm) renderTwoColumn(formContent string) string {
	leftWidth := f.width * 55 / 100

	formLines := strings.Split(formContent, "\n")
	previewLines := f.commandPreviewLines()

	// Offset preview by 1 to align with the title (line 0 is the top padding from Rule 131)
	const previewOffset = 1
	maxLines := max(len(formLines), len(previewLines)+previewOffset)

	var b strings.Builder
	for i := range maxLines {
		var left string
		if i < len(formLines) {
			left = formLines[i]
		}
		// Pad left column to fixed width with the app background color
		paddedLeft := theme.PadWithBg(left, leftWidth)

		previewIdx := i - previewOffset
		if previewIdx >= 0 && previewIdx < len(previewLines) {
			b.WriteString(paddedLeft + previewLines[previewIdx])
		} else {
			b.WriteString(paddedLeft)
		}
		if i < maxLines-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// renderFormContent renders the form fields without the two-column wrapper
func (f *LaunchForm) renderFormContent() string {
	var b strings.Builder

	b.WriteString(theme.EmptyLineBg(f.width) + "\n")

	b.WriteString(theme.Bg("  Image" + theme.IconChevronRight + " "))
	b.WriteString(theme.DimStyle.Render(f.image))
	b.WriteString("\n\n")

	// Name
	b.WriteString(f.renderTextField("Name", f.nameInput.View(), 0))
	b.WriteString("\n\n")

	// Entrypoint (with verify indicator)
	b.WriteString(f.renderEntrypointField())
	b.WriteString("\n\n")

	// Environment variables
	envHint := "NAME=VALUE pairs separated by commas (no comma in a value)"
	if f.envNeedsValues() {
		envHint = envRestoredHint
	}
	b.WriteString(f.renderTextFieldWithHint(
		"Environment", f.envInput.View(), f.fieldEnv(), envHint,
	))
	b.WriteString("\n\n")

	// Volume mounts
	b.WriteString(f.renderTextFieldWithHint(
		"Volumes", f.volInput.View(), f.fieldVolumes(),
		"vol:/path pairs or --mount values (type=bind,source=,target=)",
	))
	b.WriteString("\n\n")

	// User (--user UID:GID or username)
	b.WriteString(f.renderTextFieldWithHint(
		"User", f.userInput.View(), f.fieldUser(),
		"UID:GID or username for --user flag",
	))
	b.WriteString("\n\n")

	// Network selector
	b.WriteString(f.renderNetworkSelector())
	b.WriteString("\n\n")

	// Ports section
	b.WriteString(theme.SubTitleStyle.Render("  Ports to expose"))
	b.WriteString("\n")
	if len(f.ports) > 0 {
		for i := range f.ports {
			b.WriteString(f.renderPortEntry(i))
			b.WriteString("\n")
		}
	} else {
		b.WriteString(theme.DimStyle.Render("  No EXPOSE ports declared by this image"))
		b.WriteString("\n")
	}
	b.WriteString(f.renderTextFieldWithHint(
		"Additional ports", f.extraPortsInput.View(), f.fieldExtraPorts(),
		"host:container pairs separated by commas",
	))
	b.WriteString("\n\n")

	// Options section
	b.WriteString(theme.SubTitleStyle.Render("  Options"))
	b.WriteString("\n")
	b.WriteString(theme.RenderCheckbox(f.optionRemove, "--rm  remove container on exit", f.focusedField == f.fieldOptionRemove()))
	b.WriteString("\n")
	b.WriteString(theme.RenderCheckbox(f.optionDetach, "-d   run in background (detach)", f.focusedField == f.fieldOptionDetach()))
	b.WriteString("\n")
	b.WriteString(theme.RenderCheckbox(f.optionInteractiveTTY, "-it  interactive + TTY (auto-set for shells)", f.focusedField == f.fieldOptionInteractive()))
	b.WriteString("\n\n")

	b.WriteString("  ")
	b.WriteString(theme.RenderButton("Launch", f.focusedField == f.fieldSubmit(), "primary"))

	return b.String()
}

// renderPortEntry renders a single port row: checkbox toggle + editable host port + fixed container port.
func (f *LaunchForm) renderPortEntry(i int) string {
	p := &f.ports[i]
	focused := f.focusedField == f.firstPortField()+i

	checkIcon := theme.IconCheckbox
	if p.enabled {
		checkIcon = theme.IconChecked
	}

	// Container port label (e.g. ":80/tcp")
	containerLabel := theme.DimStyle.Render(":" + p.containerPort)

	if focused {
		prefix := theme.KeyStyle.Render(theme.IconCircleSmall + " " + checkIcon + " ")
		return prefix + p.hostPortInput.View() + containerLabel
	}
	return theme.Bg("  "+checkIcon+" ") + p.hostPortInput.View() + containerLabel
}

// renderEntrypointField renders the Entrypoint field with the verification state icon
func (f *LaunchForm) renderEntrypointField() string {
	icon := f.renderVerifyIcon()
	iconSuffix := ""
	if icon != "" {
		iconSuffix = " " + icon
	}

	var labelStr string
	if f.focusedField == f.fieldEntrypoint() {
		labelStr = theme.KeyStyle.Render(theme.IconCircleSmall+" Entrypoint "+theme.IconChevronRight) +
			theme.DimStyle.Render("  override default entrypoint") + iconSuffix
	} else {
		labelStr = theme.Bg("  Entrypoint "+theme.IconChevronRight) +
			theme.DimStyle.Render("  override default entrypoint") + iconSuffix
	}

	return labelStr + "\n" + theme.Bg("  ") + f.entrypointInput.View()
}

// renderVerifyIcon returns the styled icon for the current verification state
func (f *LaunchForm) renderVerifyIcon() string {
	switch f.verify {
	case verifyChecking:
		return theme.DimStyle.Render("…")
	case verifyOK:
		return theme.StatusOKStyle.Render(theme.IconOK)
	case verifyWarning:
		return theme.StatusWarningStyle.Render(theme.IconWarning)
	default:
		return ""
	}
}

// renderTextField renders a mono-line label + textinput (multi-line layout, Rule 113)
func (f *LaunchForm) renderTextField(label, value string, fieldIdx int) string {
	var labelStr string
	if f.focusedField == fieldIdx {
		labelStr = theme.KeyStyle.Render(theme.IconCircleSmall + " " + label + " " + theme.IconChevronRight)
	} else {
		labelStr = theme.Bg("  " + label + " " + theme.IconChevronRight)
	}
	return labelStr + "\n  " + value
}

// renderTextFieldWithHint renders a label + hint on the label line + textinput below
func (f *LaunchForm) renderTextFieldWithHint(label, value string, fieldIdx int, hint string) string {
	var labelStr string
	if f.focusedField == fieldIdx {
		labelStr = theme.KeyStyle.Render(theme.IconCircleSmall+" "+label+" "+theme.IconChevronRight) +
			theme.DimStyle.Render("  "+hint)
	} else {
		labelStr = theme.Bg("  "+label+" "+theme.IconChevronRight) +
			theme.DimStyle.Render("  "+hint)
	}
	return labelStr + "\n  " + value
}

// renderNetworkSelector renders the network field as a cycle field (Rule 132)
func (f *LaunchForm) renderNetworkSelector() string {
	current := ""
	if len(f.networks) > 0 {
		current = f.networks[f.networkIdx]
	}
	label := "Network " + theme.IconSelect + " "
	value := theme.Bg(current)
	if f.focusedField == f.fieldNetwork() {
		return theme.KeyStyle.Render(theme.IconCircleSmall+" "+label+theme.IconChevronRight+" ") + value
	}
	return theme.Bg("  "+label+theme.IconChevronRight+" ") + value
}
