package agentusageview

import "github.com/anthnel/devdesk/internal/ui/help"

// GetHelpContent returns the help for `?` (Rule 114).
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title: "Agent usage",
		Description: "The Claude Code sessions found on this machine, newest first. " +
			"Only where and when a session ran is read — nothing a session says.",
		KeyBindings: []help.KeyBinding{
			{Key: "ctrl+r", Description: "Read the sessions again"},
			{Key: "/", Description: "Filter by project or session ID"},
			{Key: ".", Description: "Cycle the sort column"},
			{Key: "ctrl+p", Description: "Open command mode"},
			{Key: "?", Description: "Show this help"},
		},
		Sections: []help.Section{
			{
				Title: "Columns",
				Body: "Project: the working directory the session ran in, with your home folded to ~. " +
					"A long path is cut from the left so the project's own name stays visible; a dash means the transcript named none.\n" +
					"Last active: when the session's transcript was last written, in local time.\n" +
					"Session: the start of the session ID. The filter matches the full ID.",
			},
			{
				Title: "Where sessions come from",
				Body: "Claude Code keeps one transcript per session under projects/ in its configuration directory: " +
					"$CLAUDE_CONFIG_DIR when set, otherwise ~/.claude. " +
					"Claude Code deletes old transcripts itself (after 30 days by default), so older sessions disappear from here too.\n" +
					"Sessions that could not be listed (symbolic links, which are refused rather than followed, unreadable directories, " +
					"or sessions beyond the listing cap) are counted as Skipped in the header.",
			},
			{
				Title: "Privacy",
				Body: "Only the working directory and the file date are shown. " +
					"Prompts, replies and tool output are never read into this view, never displayed and never logged.",
			},
		},
	}
}
