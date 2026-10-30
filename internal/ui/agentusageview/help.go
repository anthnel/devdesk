package agentusageview

import "github.com/anthnel/devdesk/internal/ui/help"

// GetHelpContent returns the help for `?` (Rule 114).
func (m Model) GetHelpContent() help.Content {
	return help.Content{
		Title: "Agent usage",
		Description: "The Claude Code sessions found on this machine, newest first. " +
			"Only where and when a session ran is read — nothing a session says.",
		KeyBindings: []help.KeyBinding{
			{Key: "ctrl+r", Description: "Re-read the whole session from the start (at the sessions level: read the sessions again)"},
			{Key: "→/enter", Description: "Open the selected session's agents"},
			{Key: "←/esc", Description: "Back to the sessions"},
			{Key: "/", Description: "Filter by project or session ID"},
			{Key: ".", Description: "Cycle the sort column (agents: tree, Total largest first, Total smallest first)"},
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
				Title: "Agents",
				Body: "Opening a session lists its agents: the orchestrator first, then each sub-agent with its type, " +
					"as a tree, with the models used and the tokens in, out, and cached.\n" +
					"A ≥ before an Out count means a floor: that sub-agent's transcript was still being written, so the real figure is higher.\n" +
					"The orchestrator is the main conversation; its figures are never a floor.\n" +
					"When part of a transcript could not be read, the footer says the totals are approximate.",
			},
			{
				Title: "Agent columns",
				Body: "Agent: the orchestrator, or the sub-agent's type. ID: the start of the sub-agent's ID, long enough to tell two agents of the same type apart; " +
					"the orchestrator shows a dash. The filter matches the full ID.\n" +
					"State: running, idle, done or unknown. A sub-agent that finished is done. " +
					"One still running with no new transcript line for more than 5 min is idle. " +
					"The orchestrator is never done: it is running or idle while updates are live, and unknown once they pause. " +
					"A sub-agent whose state the files do not say is unknown once updates pause.\n" +
					"Tools: distinct tool calls read from the transcript.\n" +
					"Duration: first to last line of the transcript, wall clock, pauses included; a dash when the transcript has no timestamps.\n" +
					"Model, In, Out, Cache read, Cache write, Total: models used and token counts. " +
					"On a narrow terminal Cache write goes first, then Cache read, In, Model, Duration, Tools and State. Agent, ID, Out, Total and Share stay.\n" +
					"Share: the agent's part of the session total, to one decimal, rounded down. " +
					"The orchestrator takes the rounding remainder, so the column adds up to 100%. A dash means the session has no tokens.\n" +
					"Press . to cycle: tree, then Total largest first, then Total smallest first, then the tree again. " +
					"Sorted, the list is flat: the branches go away. The sort stays when you open another session or press ctrl+r.",
			},
			{
				Title: "Live updates",
				Body: "While a session is open and still active, its figures are refreshed every 2 s and the header shows Updates: live.\n" +
					"Updates pause after 5 min without activity once every agent has finished (a session with an unfinished sub-agent stays live), or when its transcript is deleted: the header shows Updates: paused.\n" +
					"An agent still running with no new transcript line for more than 5 min is shown idle; a long tool call does that too. The header counts the idle agents.\n" +
					"Press ctrl+r to resume. Re-read the whole session from the start with ctrl+r; coming back to the view only reads what changed since the last point read, and goes back to live if the session is being written to.\n" +
					"The list of sessions itself is not live: refresh it with ctrl+r at the sessions level.",
			},
			{
				Title: "Privacy",
				Body: "The sessions list reads the working directory and the file date alone. " +
					"Opening a session also reads its token counts, model names and agent types, and shows only the project's working directory, the date, the session ID, the agent types, the models and the tokens. " +
					"Prompts, replies, tool output and the paths of files a session touched are never displayed and never logged.\n" +
					"Tool calls are counted by their block type alone; their names, inputs and results are never read into DevDesk's data, displayed or logged.",
			},
		},
	}
}
