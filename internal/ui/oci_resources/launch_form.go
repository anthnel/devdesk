package ociresources

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"gitlab.com/anthnell/devsecops/devdesk/internal/cache"
	"gitlab.com/anthnell/devsecops/devdesk/internal/docker"
	"gitlab.com/anthnell/devsecops/devdesk/internal/ui/theme"
)

// LaunchFormSubmitMsg is sent when the launch form is submitted
type LaunchFormSubmitMsg struct {
	Opts docker.ContainerLaunchOptions
}

// LaunchFormCancelMsg is sent when the launch form is cancelled
type LaunchFormCancelMsg struct{}

// EntrypointVerifyFinishedMsg is sent when the background entrypoint verification completes.
// Seq guards against stale results from previous verification runs.
type EntrypointVerifyFinishedMsg struct {
	Seq int
	OK  bool
}

// portEntry represents a port declared by EXPOSE in the image, with an editable host mapping.
type portEntry struct {
	containerPort string          // e.g. "80/tcp" — fixed, from image EXPOSE
	hostPortInput textinput.Model // editable host port number, defaults to the container port number
	enabled       bool
}

// containerPortNum returns the numeric part of containerPort (strips "/protocol" suffix).
func (p *portEntry) containerPortNum() string {
	if idx := strings.Index(p.containerPort, "/"); idx > 0 {
		return p.containerPort[:idx]
	}
	return p.containerPort
}

// mappingArg returns the "-p host:container" argument value for docker run.
// Falls back to the container port number when the host input is empty.
func (p *portEntry) mappingArg() string {
	host := strings.TrimSpace(p.hostPortInput.Value())
	container := p.containerPortNum()
	if host == "" {
		host = container
	}
	return host + ":" + container
}

// verifyState represents the entrypoint verification status
type verifyState int

const (
	verifyIdle     verifyState = iota // no entrypoint typed
	verifyChecking                    // docker check in progress
	verifyOK                          // binary found in image
	verifyWarning                     // binary not found
)

// commonShells lists base names that trigger auto-selection of Interactive/TTY mode
var commonShells = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true,
	"dash": true, "ksh": true, "csh": true, "tcsh": true,
	"ash": true, "python": true, "python3": true, "busybox": true,
}

// LaunchForm is a viewport form for launching a container from an image
type LaunchForm struct {
	image                string
	nameInput            textinput.Model
	ports                []portEntry     // one row per image EXPOSE port
	extraPortsInput      textinput.Model // free-form extra port mappings (comma-separated host:container)
	entrypointInput      textinput.Model
	envInput             textinput.Model
	volInput             textinput.Model
	userInput            textinput.Model // --user UID:GID or username (optional)
	networks             []string        // available Docker network names
	networkIdx           int             // index into networks
	optionRemove         bool            // --rm: remove container on exit
	optionDetach         bool            // -d: run in background
	optionInteractiveTTY bool            // -it: interactive + TTY
	verify               verifyState
	verifySeq            int    // incremented on each new verification to discard stale results
	lastEntrypoint       string // tracks previous entrypoint value for change detection
	focusedField         int
	err                  string
	width                int // viewport content width for two-column layout
}

// NewLaunchForm creates a launch form pre-filled with exposed ports and available networks.
// exposedPorts is a slice of "port/protocol" strings, e.g. ["80/tcp", "443/tcp"].
func NewLaunchForm(imageName string, exposedPorts []string, networks []docker.Network, width int) *LaunchForm {
	newInput := func(placeholder string) textinput.Model {
		ti := textinput.New()
		ti.CharLimit = 512
		theme.StyleTextInput(&ti)
		ti.Placeholder = placeholder
		return ti
	}

	nameInput := newInput("container-name (optional)")
	entrypointInput := newInput("/bin/sh (optional override)")
	envInput := newInput("APP_PORT=8080")
	volInput := newInput("myvolume:/app/data")
	userInput := newInput("1000:1000 or username (optional)")
	extraPortsInput := newInput("8080:8080, 9090:9090")

	// Build port entries from image EXPOSE specs — all enabled by default
	var ports []portEntry
	for _, spec := range exposedPorts {
		// spec = "80/tcp" or bare "80"
		portNum := spec
		if idx := strings.Index(spec, "/"); idx > 0 {
			portNum = spec[:idx]
		}
		hi := textinput.New()
		hi.CharLimit = 10
		hi.Width = 6
		theme.StyleTextInput(&hi)
		hi.SetValue(portNum)
		ports = append(ports, portEntry{
			containerPort: spec,
			hostPortInput: hi,
			enabled:       true,
		})
	}

	// Build network list, always include bridge as fallback
	var netNames []string
	seen := map[string]bool{}
	for _, n := range networks {
		if !seen[n.Name] {
			netNames = append(netNames, n.Name)
			seen[n.Name] = true
		}
	}
	if len(netNames) == 0 {
		netNames = []string{"bridge"}
	}

	nameInput.Focus()

	f := &LaunchForm{
		image:           imageName,
		nameInput:       nameInput,
		ports:           ports,
		extraPortsInput: extraPortsInput,
		entrypointInput: entrypointInput,
		envInput:        envInput,
		volInput:        volInput,
		userInput:       userInput,
		networks:        netNames,
		optionDetach:    true, // default: run in background
		width:           width,
	}
	f.resizeInputs()
	return f
}

// ToCacheEntry extracts the current form field values into a LaunchOptionsEntry for caching.
// The container name is intentionally excluded (must be unique per container).
func (f *LaunchForm) ToCacheEntry() cache.LaunchOptionsEntry {
	portMappings := make(map[string]cache.PortMappingEntry, len(f.ports))
	for _, p := range f.ports {
		portMappings[p.containerPort] = cache.PortMappingEntry{
			HostPort: p.hostPortInput.Value(),
			Enabled:  p.enabled,
		}
	}
	network := ""
	if len(f.networks) > 0 {
		network = f.networks[f.networkIdx]
	}
	return cache.LaunchOptionsEntry{
		Entrypoint:   f.entrypointInput.Value(),
		ExtraPorts:   f.extraPortsInput.Value(),
		Env:          f.envInput.Value(),
		Volumes:      f.volInput.Value(),
		User:         f.userInput.Value(),
		Network:      network,
		Remove:       f.optionRemove,
		Detach:       f.optionDetach,
		Interactive:  f.optionInteractiveTTY,
		PortMappings: portMappings,
	}
}

// ApplyCached pre-fills the form fields from a cached launch options entry.
func (f *LaunchForm) ApplyCached(entry cache.LaunchOptionsEntry) {
	f.entrypointInput.SetValue(entry.Entrypoint)
	f.lastEntrypoint = entry.Entrypoint
	f.extraPortsInput.SetValue(entry.ExtraPorts)
	f.envInput.SetValue(entry.Env)
	f.volInput.SetValue(entry.Volumes)
	f.userInput.SetValue(entry.User)
	f.optionRemove = entry.Remove
	f.optionDetach = entry.Detach
	f.optionInteractiveTTY = entry.Interactive

	// Apply cached network selection by name
	if entry.Network != "" {
		for i, n := range f.networks {
			if n == entry.Network {
				f.networkIdx = i
				break
			}
		}
	}

	// Apply cached port mappings by container port key; unmatched ports keep defaults
	for i := range f.ports {
		if pm, ok := entry.PortMappings[f.ports[i].containerPort]; ok {
			f.ports[i].hostPortInput.SetValue(pm.HostPort)
			f.ports[i].enabled = pm.Enabled
		}
	}
}

// GetTitle returns the form title for use in the viewport breadcrumb
func (f *LaunchForm) GetTitle() string { return "Launch Container" }

// SetWidth updates the form width for responsive two-column rendering
func (f *LaunchForm) SetWidth(w int) {
	f.width = w
	f.resizeInputs()
}

func (f *LaunchForm) resizeInputs() {
	// In two-column mode the left column is 55% of width; inputs sit below their label with 2-char indent.
	inputWidth := f.width - 4
	if f.width >= 100 {
		inputWidth = f.width*55/100 - 4
	}
	inputWidth = max(inputWidth, 20)
	f.nameInput.Width = inputWidth
	f.entrypointInput.Width = inputWidth
	f.envInput.Width = inputWidth
	f.volInput.Width = inputWidth
	f.userInput.Width = inputWidth
	f.extraPortsInput.Width = inputWidth
}

// Field indices follow the visual render order:
// 0:name  1:entrypoint  2:env  3:volumes  4:user  5:network  6..6+n-1:ports  6+n:extraPorts  6+n+1..+3:options  6+n+4:submit
const firstPortFieldOffset = 6

func (f *LaunchForm) fieldEntrypoint() int        { return 1 }
func (f *LaunchForm) fieldEnv() int               { return 2 }
func (f *LaunchForm) fieldVolumes() int           { return 3 }
func (f *LaunchForm) fieldUser() int              { return 4 }
func (f *LaunchForm) fieldNetwork() int           { return 5 }
func (f *LaunchForm) firstPortField() int         { return firstPortFieldOffset }
func (f *LaunchForm) fieldExtraPorts() int        { return firstPortFieldOffset + len(f.ports) }
func (f *LaunchForm) fieldOptionRemove() int      { return firstPortFieldOffset + len(f.ports) + 1 }
func (f *LaunchForm) fieldOptionDetach() int      { return firstPortFieldOffset + len(f.ports) + 2 }
func (f *LaunchForm) fieldOptionInteractive() int { return firstPortFieldOffset + len(f.ports) + 3 }
func (f *LaunchForm) fieldSubmit() int            { return firstPortFieldOffset + len(f.ports) + 4 }

// isPortField returns true if the given index corresponds to a port entry row
func (f *LaunchForm) isPortField(idx int) bool {
	return idx >= f.firstPortField() && idx < f.firstPortField()+len(f.ports)
}

// Update handles messages for the launch form
func (f *LaunchForm) Update(msg tea.Msg) (*LaunchForm, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return f.handleKeyMsg(msg)
	case EntrypointVerifyFinishedMsg:
		return f.handleVerifyFinished(msg)
	}
	return f.updateActiveInput(msg)
}

func (f *LaunchForm) handleKeyMsg(msg tea.KeyMsg) (*LaunchForm, tea.Cmd) {
	maxField := f.fieldSubmit()

	switch msg.String() {
	case "ctrl+y":
		return f, copyToClipboardCmd(f.buildCommandString())

	case "esc":
		return f, func() tea.Msg { return LaunchFormCancelMsg{} }

	case "down":
		f.focusedField = (f.focusedField + 1) % (maxField + 1)
		f.updateFocus()
		return f, nil

	case "up":
		f.focusedField = (f.focusedField - 1 + maxField + 1) % (maxField + 1)
		f.updateFocus()
		return f, nil

	case "left":
		if f.focusedField == f.fieldNetwork() && len(f.networks) > 0 {
			f.networkIdx = (f.networkIdx - 1 + len(f.networks)) % len(f.networks)
			return f, nil
		}
	case "right":
		if f.focusedField == f.fieldNetwork() && len(f.networks) > 0 {
			f.networkIdx = (f.networkIdx + 1) % len(f.networks)
			return f, nil
		}

	case " ":
		// Space toggles checkboxes (Rule 135): port fields and option checkboxes
		if f.isPortField(f.focusedField) {
			f.ports[f.focusedField-f.firstPortField()].enabled = !f.ports[f.focusedField-f.firstPortField()].enabled
			return f, nil
		}
		return f.handleSpaceKey()

	case "enter":
		// Enter advances to next field or submits; never toggles (Rule 135)
		return f.handleEnterKey()
	}

	return f.updateActiveInput(msg)
}

// handleEnterKey processes Enter: advances to next field or submits (Rule 135 — never toggles).
func (f *LaunchForm) handleEnterKey() (*LaunchForm, tea.Cmd) {
	maxField := f.fieldSubmit()
	if f.focusedField == maxField {
		return f.submit()
	}
	f.focusedField = (f.focusedField + 1) % (maxField + 1)
	f.updateFocus()
	return f, nil
}

// handleSpaceKey processes Space: toggles option checkboxes (Rule 135).
func (f *LaunchForm) handleSpaceKey() (*LaunchForm, tea.Cmd) {
	if f.focusedField == f.fieldOptionRemove() {
		f.optionRemove = !f.optionRemove
		return f, nil
	}
	if f.focusedField == f.fieldOptionDetach() {
		f.optionDetach = !f.optionDetach
		if f.optionDetach {
			f.optionInteractiveTTY = false
		}
		return f, nil
	}
	if f.focusedField == f.fieldOptionInteractive() {
		f.optionInteractiveTTY = !f.optionInteractiveTTY
		if f.optionInteractiveTTY {
			f.optionDetach = false
		}
		return f, nil
	}
	return f, nil
}

// handleVerifyFinished updates the verify state, discarding stale results
func (f *LaunchForm) handleVerifyFinished(msg EntrypointVerifyFinishedMsg) (*LaunchForm, tea.Cmd) {
	if msg.Seq != f.verifySeq {
		return f, nil // stale result from a previous entrypoint value
	}
	if msg.OK {
		f.verify = verifyOK
	} else {
		f.verify = verifyWarning
	}
	return f, nil
}

func (f *LaunchForm) updateFocus() {
	f.nameInput.Blur()
	f.extraPortsInput.Blur()
	f.entrypointInput.Blur()
	f.envInput.Blur()
	f.volInput.Blur()
	f.userInput.Blur()
	for i := range f.ports {
		f.ports[i].hostPortInput.Blur()
	}
	switch {
	case f.focusedField == 0:
		f.nameInput.Focus()
	case f.isPortField(f.focusedField):
		f.ports[f.focusedField-f.firstPortField()].hostPortInput.Focus()
	case f.focusedField == f.fieldExtraPorts():
		f.extraPortsInput.Focus()
	case f.focusedField == f.fieldEntrypoint():
		f.entrypointInput.Focus()
	case f.focusedField == f.fieldEnv():
		f.envInput.Focus()
	case f.focusedField == f.fieldVolumes():
		f.volInput.Focus()
	case f.focusedField == f.fieldUser():
		f.userInput.Focus()
	}
}

func (f *LaunchForm) updateActiveInput(msg tea.Msg) (*LaunchForm, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case f.focusedField == 0:
		f.nameInput, cmd = f.nameInput.Update(msg)
	case f.isPortField(f.focusedField):
		portIdx := f.focusedField - f.firstPortField()
		f.ports[portIdx].hostPortInput, cmd = f.ports[portIdx].hostPortInput.Update(msg)
	case f.focusedField == f.fieldExtraPorts():
		f.extraPortsInput, cmd = f.extraPortsInput.Update(msg)
	case f.focusedField == f.fieldEntrypoint():
		prev := f.entrypointInput.Value()
		f.entrypointInput, cmd = f.entrypointInput.Update(msg)
		if newVal := f.entrypointInput.Value(); newVal != prev {
			verifyCmd := f.onEntrypointChanged(newVal)
			cmd = tea.Batch(cmd, verifyCmd)
		}
	case f.focusedField == f.fieldEnv():
		f.envInput, cmd = f.envInput.Update(msg)
	case f.focusedField == f.fieldVolumes():
		f.volInput, cmd = f.volInput.Update(msg)
	case f.focusedField == f.fieldUser():
		f.userInput, cmd = f.userInput.Update(msg)
	}
	return f, cmd
}

// onEntrypointChanged applies auto-selection logic and triggers a verification check.
func (f *LaunchForm) onEntrypointChanged(newVal string) tea.Cmd {
	f.lastEntrypoint = newVal

	if newVal == "" {
		f.verify = verifyIdle
		f.verifySeq++
		return nil
	}

	// Auto-select Interactive/TTY when the entrypoint looks like a shell
	baseName := strings.ToLower(filepath.Base(newVal))
	if commonShells[baseName] {
		f.optionInteractiveTTY = true
		f.optionDetach = false
	}

	// Start debounced verification
	f.verify = verifyChecking
	f.verifySeq++
	return verifyEntrypointCmd(f.verifySeq, f.image, newVal)
}

func (f *LaunchForm) submit() (*LaunchForm, tea.Cmd) {
	opts := docker.ContainerLaunchOptions{
		Image:       f.image,
		Name:        strings.TrimSpace(f.nameInput.Value()),
		Entrypoint:  strings.TrimSpace(f.entrypointInput.Value()),
		User:        strings.TrimSpace(f.userInput.Value()),
		Detach:      f.optionDetach,
		Remove:      f.optionRemove,
		Interactive: f.optionInteractiveTTY,
		TTY:         f.optionInteractiveTTY,
	}
	// Collect enabled image ports with their (possibly customised) host mapping
	for i := range f.ports {
		if f.ports[i].enabled {
			opts.Ports = append(opts.Ports, f.ports[i].mappingArg())
		}
	}
	// Extra port mappings: comma-separated host:container pairs
	for p := range strings.SplitSeq(f.extraPortsInput.Value(), ",") {
		if p = strings.TrimSpace(p); p != "" {
			opts.Ports = append(opts.Ports, p)
		}
	}
	// Env vars: comma-separated KEY=VALUE entries
	for e := range strings.SplitSeq(f.envInput.Value(), ",") {
		if e = strings.TrimSpace(e); e != "" {
			opts.Env = append(opts.Env, e)
		}
	}
	// Volume mounts: comma-separated vol:/path entries
	for v := range strings.SplitSeq(f.volInput.Value(), ",") {
		if v = strings.TrimSpace(v); v != "" {
			opts.Volumes = append(opts.Volumes, v)
		}
	}
	if len(f.networks) > 0 {
		opts.Network = f.networks[f.networkIdx]
	}
	return f, func() tea.Msg { return LaunchFormSubmitMsg{Opts: opts} }
}
