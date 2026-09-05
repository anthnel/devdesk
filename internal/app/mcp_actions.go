package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthnel/devdesk/internal/command"
	mcpserver "github.com/anthnel/devdesk/internal/mcp"
	ociresources "github.com/anthnel/devdesk/internal/ui/oci_resources"
	"github.com/anthnel/devdesk/internal/ui/workspaces"
)

// mcpAction turns an action tool into the view that owns it and the request
// that view understands.
//
// It is a table because the selection rule is one: **an action tool exists for
// an entry of the uppercase vocabulary that keeps a meaning without a screen**
// (§3.61, `internal/ui/keymap`). The table is what
// TestEveryActionToolMapsToADeclaredKey opposes to that vocabulary, so a tool
// invented here without a key behind it fails rather than accumulates.
//
// What is deliberately absent, and why:
//
//   - `D` `P` `M` `N`, and `K` on a container — destructive, or irreversible
//     because their only undo is a delete that is not exposed. §3.61 keeps the
//     class out rather than guarding it: a confirmation dialog raised by a tool
//     call blocks the agent on an event the user is not looking at, and an
//     action never registered cannot be wrongly confirmed.
//   - `T` `O` `W` `V` `L` `B` `R` `I` — they open an interactive process or a
//     screen on somebody's desktop. An agent does nothing with one.
//   - `U` — it touches the secret store (§3.9).
//   - `X` `Y` — excluding a finding writes configuration, which §3.61 leaves
//     out of v1; copying a path aims at the host's clipboard.
//   - `C` — cloning. It is neither destructive nor out of scope; it is not
//     buildable through this door yet. See the note in mcpActionTable.
func mcpAction(act mcpserver.Action, invocation string) (command.ViewType, tea.Msg, bool) {
	build, ok := mcpActionTable()[act.Tool]
	if !ok {
		return "", nil, false
	}
	view, msg := build(act.Targets, invocation)
	return view, msg, true
}

// mcpActionTable is the declared surface, one entry per exposed action.
//
// **`clone_start` is not here, and the reason is in the explorer.** `C` opens a
// selection the user builds by walking the forge tree, then a second screen for
// the destination; `handleCloneDestinationSelected` resolves it through
// `m.rootNodes()` and `m.selection`, both of which are the state of a tree
// somebody browsed. An agent has none of that, so exposing the tool would mean
// a headless resolution path — a group path to a node set, without the tree —
// which is a feature to specify rather than plumbing to add here. §3.61's
// vocabulary rule stands; what it maps to does not exist yet.
func mcpActionTable() map[string]func(targets []string, invocation string) (command.ViewType, tea.Msg) {
	return map[string]func([]string, string) (command.ViewType, tea.Msg){
		"workspace_scan_start": func(targets []string, inv string) (command.ViewType, tea.Msg) {
			return command.ViewWorkspaces, workspaces.ScanRequestedMsg{Paths: targets, Invocation: inv}
		},
		"workspace_sync_start": func(targets []string, inv string) (command.ViewType, tea.Msg) {
			return command.ViewWorkspaces, workspaces.SyncRequestedMsg{Paths: targets, Invocation: inv}
		},
		"image_scan_start": func(targets []string, inv string) (command.ViewType, tea.Msg) {
			return command.ViewOCIResources, ociresources.ImageScanRequestedMsg{Images: targets, Invocation: inv}
		},
		"image_pull_start": func(targets []string, inv string) (command.ViewType, tea.Msg) {
			image := ""
			if len(targets) > 0 {
				image = targets[0]
			}
			return command.ViewOCIResources, ociresources.ImagePullRequestedMsg{Image: image, Invocation: inv}
		},
	}
}

// mcpActionKeys maps each exposed action to the key it is the headless form of.
// It exists to be walked by a test against internal/ui/keymap, and it is
// separate from the table above so that adding a tool without answering "which
// key is this" fails to compile the test rather than passing it.
func mcpActionKeys() map[string]string {
	return map[string]string{
		"workspace_scan_start": "S",
		"workspace_sync_start": "F",
		"image_scan_start":     "S",
		"image_pull_start":     "G",
	}
}
