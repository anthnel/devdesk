package theme

import "github.com/anthnel/devdesk/internal/config"

var (
	IconPause                 = "\U000F03E4" // 󰏤 nf-md-pause
	IconPlay                  = "\U000F040A" // 󰐊 nf-md-play
	IconStop                  = "\U000F04DB" // 󰓛 nf-md-stop
	IconCheckbox              = "\U000F0131" // 󰄱 nf-md-checkbox_blank_outline
	IconChecked               = "\U000F0132" // 󰄲 nf-md-checkbox_marked
	IconCheckboxIndeterminate = "\U000F0135" // 󰄵 nf-md-checkbox_intermediate
	IconError                 = "\U000F0159" // 󰅙 nf-md-close_circle
	IconWarning               = "\U000F0026" // 󰀦 nf-md-alert
	IconOK                    = "\U000F05E0" // 󰗠 nf-md-check_circle
	IconLock                  = "\U000F033E" // 󰌾 nf-md-lock
	IconUnlock                = "\U000F033F" // 󰌿 nf-md-lock_open
	IconSecurity              = "\U000F0483" // 󰒃 nf-md-security
	IconUser                  = "\U000F0004" // 󰀄 nf-md-account
	IconCheck                 = "\U000F012C" // 󰄬 nf-md-check
	IconCross                 = "\U000F0156" // 󰅖 nf-md-close
	IconChevronRight          = "\U000F0142" // 󰅂 nf-md-chevron_right
	IconChevronDown           = "\U000F0140" // 󰅀 nf-md-chevron_down
	IconConfig                = "\ue615"     //  nf-seti-config
	IconDirectory             = "\U000F024B" // 󰉋 nf-md-folder
	IconDirectoryOpen         = "\uf4d4"     //  nf-oct-file_directory_open_fill
	IconFile                  = "\U000F0214" // 󰈔 nf-md-file
	IconCodeFile              = "\U000F102B" // 󱀫 nf-md-file_code_outline
	IconDocker                = "\ue7b0"     //  nf-dev-docker
	IconContainer             = "\uf4b7"     //  nf-oct-container
	IconTools                 = "\ue20f"     //  nf-fae-tools
	IconServer                = "\U000F048B" // 󰒋 nf-md-server
	IconArrowDown             = "\U000F0045" // 󰁅 nf-md-arrow_down
	IconArrowUp               = "\U000F005D" // 󰁝 nf-md-arrow_up
	IconArrowLeft             = "\U000F004D" // 󰁍 nf-md-arrow_left
	IconArrowRight            = "\U000F0054" // 󰁔 nf-md-arrow_right
	IconRefresh               = "\U000F0450" // 󰑐 nf-md-refresh
	IconTarget                = "\U000F04FE" // 󰓾 nf-md-target
	IconHourglass             = "\U000F051F" // 󰔟 nf-md-timer_sand
	IconCircleSmall           = "\ueb8a"     //  nf-cod-circle_small_filled
	IconCircle                = "\U000F09DE" // 󰧞 nf-md-circle_medium
	IconSelect                = "\uf516"     //  nf-oct-single_selectc
	IconCertificate           = "\uf23e"     //  nf-fa-expeditedssl
	IconInfo                  = "\U000F02FD" // 󰋽 nf-md-information_outline
	IconGitlab                = "\uf296"     //  nf-fa-gitlab
	IconGithub                = "\uf09b"     //  nf-fa-github
	IconWorkspace             = IconDirectoryOpen
	IconDashboard             = "\ueacd"        //  nf-cod-dashboard
	IconWorkspaceTrusted      = "\uebc1"        //  nf-cod-workspace_trusted
	IconWorkspaceUnknown      = "\uebc3"        //  nf-cod-workspace_unknown
	IconWorkspaceUntrusted    = "\uebc2"        //  nf-cod-workspace_untrusted
	IconHome                  = "\U000F02DC"    // 󰋜 nf-md-home
	IconTreeBranch            = "\u251c\u2500 " // a tree node with siblings below it
	IconTreeEnd               = "\u2514\u2500 " // the last node of a run
	IconGitBranch             = "\ue725"        //  nf-dev-git_branch
	IconGitUntracked          = "\U000F02D6"    // 󰋖 nf-md-help
	IconGitUnpushed           = "\uf403"        //  nf-oct-arrow_up
	IconGitUnpulled           = "\ueb40"        //  nf-cod-arrow_down
	IconGitModified           = "\U000F03EB"    // 󰏫 nf-md-pencil
	IconToml                  = "\ue6b2"        //  nf-custom-toml
	IconPending               = "\U000F0150"    // 󰅐 nf-md-clock_outline
	IconCanceled              = "\U000F0376"    // 󰍶 nf-md-minus_circle
	IconSkipped               = "\U000F0B2A"    // 󰬪 nf-md-chevron_right_circle
	IconManual                = "\U000F040D"    // 󰐍 nf-md-play_circle_outline
	// Container state icons (compact)
	IconCaretRight  = "\U000F035F" // 󰍟 nf-md-menu_right (small play)
	IconCaretUp     = "\U000F0360" // 󰍠 nf-md-menu_up
	IconSmallPause  = "\uead1"     //  nf-cod-debug_pause (compact pause)
	IconSmallSquare = "\u25a0"     // ■ black square (stop)
	IconBan         = "\U000F073A" // 󰜺 nf-md-cancel (dead)

	IconGo     = "\ue626" //  nf-seti-go
	IconRust   = "\ue7a8" //  nf-dev-rust
	IconNode   = "\ue718" //  nf-dev-nodejs_small
	IconPython = "\ue73c" //  nf-dev-python
	IconJava   = "\ue738" //  nf-dev-java
	IconRuby   = "\ue739" //  nf-dev-ruby
	IconPHP    = "\ue73d" //  nf-dev-php
	IconElixir = "\ue62d" //  nf-seti-elixir

	// Explorer glyphs. They are deliberately none of the workspaces set —
	// IconGitBranch, IconDirectory and the fileicon table — because the two
	// views list different things: ws lists what is on disk, exp lists what the
	// forge holds, and a row that looked the same in both would claim they are
	// the same object.
	//
	// The visibility trio is GitLab's and GitHub's own: a globe for what anyone
	// can read, a shield for what a signed-in user can, a lock for neither. The
	// lock is IconLock, already here — a fourth name for one codepoint is how a
	// glyph table stops being one.
	IconNamespace          = "\U000F0849" // 󰡉 nf-md-account_group
	IconRepository         = "\uf401"     //  nf-oct-repo
	IconVisibilityPublic   = "\U000F01E7" // 󰇧 nf-md-earth
	IconVisibilityInternal = "\U000F0498" // 󰒘 nf-md-shield

	IconNetwork = "\U000F06F3" // 󰛳 nf-md-network
	IconVolume  = "\U000F01BC" // 󰆼 nf-md-database
)

// Aliases (avoid duplicating literals)
var (
	IconService = IconPlay //  status view title
	IconRunning = IconPlay //  pipeline running
)

// ForgeIcon is the glyph that names a code-hosting platform.
//
// The vocabulary lives in internal/forge and the glyphs live here, because a
// domain package must not import the UI. Both key on config's own constants
// rather than on a literal, so there is one spelling of "gitlab" in the
// application and a new forge cannot be half-added.
//
// An unknown forge gets the generic git glyph rather than nothing: a missing
// icon shifts every title it prefixes by one cell.
func ForgeIcon(forgeType string) string {
	switch forgeType {
	case config.ForgeGitHub:
		return IconGithub
	case config.ForgeGitLab:
		return IconGitlab
	default:
		return IconGitBranch
	}
}
