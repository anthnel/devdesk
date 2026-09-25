package configuration

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// fieldKind decides the control (Rule 132 for the closed sets, Rule 135 for the
// keys each one answers to).
type fieldKind int

const (
	kindText fieldKind = iota
	kindInteger
	kindToggle
	kindCycle
	// kindStatic is a fact about the context, shown among the settings it
	// qualifies but edited nowhere: the config file's own path. It sits in the
	// form rather than in the header because it belongs beside the other paths,
	// and the header has seven lines to spend on what changes.
	kindStatic
	// kindSecret is a fact the user has to copy out but nobody should have on
	// screen by default: today the MCP server's bearer token. It is focusable
	// where kindStatic is not, because revealing it is an act — `space`, the
	// only key that toggles anything in a form (Rule 135).
	//
	// The alternative was a plain static row. It was refused because every
	// other secret in this application is masked — the forge token and the
	// registry password both set EchoPassword — and one screen showing one in
	// clear would be the exception nobody remembers making.
	kindSecret
)

// field is one editable setting.
//
// The accessors are pointers into the live config rather than get/set pairs:
// twenty-nine settings written as closures each way is where the copy-paste
// defects of §2 came from. One reference per field means the read and the write
// cannot disagree about which setting they mean.
type field struct {
	Label   string
	Kind    fieldKind
	Options []string // kindCycle only

	// Group and GroupIcon head a run of related settings inside a tab. They are
	// stamped on by group(), not written per field, so a run cannot be split by
	// a typo and render its header twice.
	Group     string
	GroupIcon string

	str func(*config.Config) *string // text, cycle
	num func(*config.Config) *int    // integer
	// list replaces str for a text field whose setting is a list — a tool's
	// extra arguments — edited as one line (scan.SplitArgs / JoinArgs).
	list func(*config.Config) *[]string
	flag func(*config.Config) *bool // toggle
	// get and set replace flag for a toggle that is not a single setting: a
	// tool ticked in a category (toolRow).
	get func(*config.Config) bool
	set func(*config.Config, bool)

	// Depth nests a checkbox under the one above it — a tool under its
	// category, a renderer under kubeconform. The focus order stays one flat
	// list; only the indent says what belongs to what.
	Depth int
	// Note is dimmed text right of a checkbox: what the row stands for, when
	// the label alone does not say it.
	Note string
	// Tool is the scanner a group of settings belongs to (the tools tab). Its
	// group heading carries the tool's state, read from the detection.
	Tool string
	// platform names a platform tool whose detected state the row shows,
	// read-only: "engine" or "git".
	platform string

	// hintOn and hintOff replace hint on a checkbox: what the box does in
	// the state it is in, so the footer says the effect of space as it is
	// pressed.
	hintOn, hintOff string
	// locked is why a checkbox cannot move right now — a Trivy server, a
	// category that is off — or "" when it can. The footer says it in place
	// of the hint, which answers the question a box that does not react
	// raises.
	locked func(*config.Config) string
	// guard is why a checkbox refuses the value it would take, or "".
	guard func(*config.Config, bool) string
	fact  string // kindStatic only — read once, never written

	min, max int                // integer bounds, inclusive
	validate func(string) error // text only, beyond emptiness
	hint     string             // shown when the field is focused
}

// section is one tab.
type section struct {
	Title  string
	Fields []field
	// Columns is how many columns the tab's groups are laid out on when the
	// width allows; 0 and 1 are one.
	Columns int
}

// group tags a run of fields with the heading they sit under. Fields stay one
// flat list per tab so the focus index needs no nesting; the renderer emits a
// heading wherever the group changes.
func group(title, icon string, fields ...field) []field {
	for i := range fields {
		fields[i].Group = title
		fields[i].GroupIcon = icon
	}
	return fields
}

// ── constructors ────────────────────────────────────────────────────────────

func text(label string, ref func(*config.Config) *string, hint string) field {
	return field{Label: label, Kind: kindText, str: ref, hint: hint}
}

func validated(label string, ref func(*config.Config) *string, hint string, v func(string) error) field {
	f := text(label, ref, hint)
	f.validate = v
	return f
}

func integer(label string, ref func(*config.Config) *int, min, max int, hint string) field {
	return field{Label: label, Kind: kindInteger, num: ref, min: min, max: max, hint: hint}
}

func toggle(label string, ref func(*config.Config) *bool, hint string) field {
	return field{Label: label, Kind: kindToggle, flag: ref, hint: hint}
}

func cycle(label string, ref func(*config.Config) *string, options []string, hint string) field {
	return field{Label: label, Kind: kindCycle, str: ref, Options: options, hint: hint}
}

func static(label, value, hint string) field {
	return field{Label: label, Kind: kindStatic, fact: value, hint: hint}
}

func secret(label, value, hint string) field {
	return field{Label: label, Kind: kindSecret, fact: value, hint: hint}
}

// mcpState is the sentence the State row shows.
//
// The three cases are distinct on purpose: "not enabled" is a choice, "not
// started" is a failure, and an address is neither. Collapsing the first two
// into "off" would make a taken port look like a setting nobody turned on.
func mcpState(mcp MCPFacts) string {
	switch {
	case mcp.Addr != "":
		return "serving on http://" + mcp.Addr
	case mcp.Reason != "":
		return "not started — " + mcp.Reason
	default:
		return "not enabled for this context"
	}
}

// ── reading and writing ─────────────────────────────────────────────────────

// Value renders the setting as the text the field edits.
func (f field) Value(c *config.Config) string {
	switch f.Kind {
	case kindInteger:
		return strconv.Itoa(*f.num(c))
	case kindToggle:
		return strconv.FormatBool(f.Bool(c))
	case kindStatic, kindSecret:
		return f.fact
	default:
		if f.list != nil {
			return scan.JoinArgs(*f.list(c))
		}
		return *f.str(c)
	}
}

// Bool reads a toggle.
func (f field) Bool(c *config.Config) bool {
	if f.get != nil {
		return f.get(c)
	}
	return *f.flag(c)
}

// Toggle flips a toggle and reports the new value.
func (f field) Toggle(c *config.Config) bool {
	if f.set != nil {
		on := !f.get(c)
		f.set(c, on)
		return on
	}
	p := f.flag(c)
	*p = !*p
	return *p
}

// Cycle moves a closed-set field by one step, wrapping. An unrecognised current
// value lands on the first option rather than being preserved: the set is
// closed, so a value outside it is not something to carry forward.
func (f field) Cycle(c *config.Config, step int) {
	if len(f.Options) == 0 {
		return
	}
	p := f.str(c)
	idx := 0
	for i, opt := range f.Options {
		if opt == *p {
			idx = i
			break
		}
	}
	*p = f.Options[(idx+step+len(f.Options))%len(f.Options)]
}

// Apply writes an edited text or integer value, or refuses it.
//
// Refusing is the whole point (Rule 128 reports it, the config keeps its old
// value): an unparseable number silently coerced to zero is how `trivy_server:
// ":"` reached a config file in the first place.
func (f field) Apply(c *config.Config, raw string) error {
	v := strings.TrimSpace(raw)

	if f.Kind == kindInteger {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s must be a number", f.Label)
		}
		if n < f.min || n > f.max {
			return fmt.Errorf("%s must be between %d and %d", f.Label, f.min, f.max)
		}
		*f.num(c) = n
		return nil
	}

	if f.validate != nil {
		if err := f.validate(v); err != nil {
			return err
		}
	}
	if f.list != nil {
		return f.applyList(c, v)
	}
	*f.str(c) = v
	return nil
}

// ── the settings ────────────────────────────────────────────────────────────

// sections lists every scalar setting a context carries, grouped into tabs.
//
// Lists stay where they are consulted: monitors keep their CRUD in the status
// view and registries keep RegistryForm in the OCI view. Duplicating them here
// would be the opposite of the point.
//
// themes and views are passed in because both are discovered rather than
// declared — themes from a directory, views from the command parser; configPath
// because it names the context's file, which the config itself does not carry.
// forgeType and v name the tab and its words. The tab is titled after the
// platform the context targets, not after the section key: `forge:` is what the
// file says, and no user calls it that.
//
// contextName is the name of the context being edited; the config does not
// carry it, which is the same reason configPath is passed.
func sections(themes, views []string, configPath, contextName, forgeType string, v forge.Vocabulary, mcp MCPFacts) []section {
	return []section{
		{Title: "app", Fields: slices.Concat(
			group("Appearance", theme.IconDashboard,
				cycle("Theme", func(c *config.Config) *string { return &c.App.Theme }, themes,
					"Applied as you cycle it"),
				cycle("Default view", func(c *config.Config) *string { return &c.App.DefaultView }, views,
					"The view DevDesk opens on"),
			),
			group("Paths", theme.IconDirectory,
				text("Workspaces dir", func(c *config.Config) *string { return &c.App.WorkspacesDir },
					"Root the workspaces view browses"),
				// Where the keystrokes land. It is the one path the user cannot
				// change from here, so it is shown rather than edited — and it
				// belongs beside the other two, not alone in the header.
				static("Config file", configPath, "Written as you edit; there is no save step"),
				text("Log file", func(c *config.Config) *string { return &c.App.LogFile }, ""),
				// Last of the group: it qualifies the paths above it, and a
				// checkbox wedged between two value rows breaks the column they
				// share.
				toggle("Show hidden files", func(c *config.Config) *bool { return &c.App.ShowHiddenFiles },
					"Dot entries, in the listing and in nested-repo discovery"),
			),
			group("External commands", theme.IconTools,
				text("IDE command", func(c *config.Config) *string { return &c.App.IDECommand },
					"Run by O in the workspaces view"),
				text("Terminal command", func(c *config.Config) *string { return &c.App.TerminalCommand },
					"Empty auto-detects; e.g. kitty --directory"),
				// The variant is a setting rather than a second key: the
				// capability belongs to the environment, not to the moment —
				// no window can be opened under WSL, and through SSH there is
				// none to open (§3.26).
				toggle("Terminal in a new window", func(c *config.Config) *bool { return &c.App.TerminalNewWindow },
					"T opens a separate window instead of suspending the TUI"),
				// The engine every container, image, network and volume comes
				// from. A path to a binary is honoured too and is deliberately
				// not cycled — there is nothing to cycle through, which is the
				// compromise scan.trivy_path already makes (§3.67).
				cycle(containerEngineLabel, func(c *config.Config) *string { return &c.App.ContainerEngine },
					config.ContainerEngines(), "auto prefers docker, then podman; a path also works"),
			),
			group("Secrets", theme.IconLock,
				cycle("Secret backend", func(c *config.Config) *string { return &c.App.SecretBackend },
					[]string{"auto", "keyring", "git-credential"},
					"Changing it does not migrate what is already stored"),
			),
		)},

		{Title: forgeType, Fields: slices.Concat(
			group("Connection", theme.ForgeIcon(forgeType),
				// Forge comes first, above the URL, because everything below it
				// reconfigures from it: the URL example, the visibility set and
				// the two labels beside them. Putting it first is what lets the
				// user see that happen.
				cycle(forgeLabel, func(c *config.Config) *string { return &c.Forge.Type },
					config.ForgeTypes(), "Declared, never guessed from the URL"),
				text("URL", func(c *config.Config) *string { return &c.Forge.URL },
					"e.g. "+v.ExampleURL),
				text("Default parent "+strings.ToLower(v.Namespace), func(c *config.Config) *string { return &c.Forge.DefaultParentGroup }, ""),
				cycle("Default visibility", func(c *config.Config) *string { return &c.Forge.DefaultVisibility },
					forge.ShapeFor(forgeType).Visibilities, ""),
				cycle("Clone method", func(c *config.Config) *string { return &c.Forge.CloneMethod },
					[]string{"https", "ssh"}, ""),
			),
			group("Pull", theme.IconGitBranch,
				integer("Parallel jobs", func(c *config.Config) *int { return &c.Forge.Pull.ParallelJobs }, 1, 32, ""),
				toggle("Include archived "+strings.ToLower(v.Repositories), func(c *config.Config) *bool { return &c.Forge.Pull.IncludeArchived }, ""),
			),
		)},

		// What a scan looks for, and which tools run each category (§3.86).
		// Where each tool runs from is the tools tab's business.
		{Title: "scan", Fields: slices.Concat(
			group("Categories", theme.IconSecurity, categoryFields()...),
			group("Remediation", theme.IconDocker,
				cycle("Base image bumps", func(c *config.Config) *string { return &c.Scan.BaseImageTrack },
					config.BaseImageTracks(), "same-line keeps the major version; next-major may take the next one"),
			),
			group("Signatures", theme.IconLock,
				cycle("Image verification", func(c *config.Config) *string { return &c.Scan.ImageVerification },
					config.ImageVerifications(), "Checks an image's signature before pulling it — rules in ~/.devdesk/trust.yaml"),
			),
			group("Limits", theme.IconHourglass,
				integer("Timeout (s)", func(c *config.Config) *int { return &c.Scan.Timeout }, 10, 3600, ""),
				integer("Max concurrent scans", func(c *config.Config) *int { return &c.Scan.MaxConcurrentScans }, 1, 16, ""),
				integer("Max cached reports", func(c *config.Config) *int { return &c.Scan.MaxCachedReports }, 1, 1000, ""),
			),
		)},

		{Title: "tools", Columns: 2, Fields: toolFields()},

		{Title: "network", Fields: slices.Concat(
			group("Checks", theme.IconHourglass,
				// One timeout rather than five. netcheck held 5 s for DNS and
				// the dial, 8 s for TLS and HTTP and 4 s for the ping, and
				// nothing argued the split — five rows for one idea.
				integer("Check timeout (s)", func(c *config.Config) *int { return &c.Network.CheckTimeout }, 1, 120,
					"How long one probe waits for an answer"),
				integer("Ping count", func(c *config.Config) *int { return &c.Network.PingCount }, 1, 20,
					"ICMP echo requests per run"),
				integer("Ports refresh (s)", func(c *config.Config) *int { return &c.Network.PortsRefreshInterval }, 1, 60,
					"How often the Ports tab re-reads the socket table"),
			),
			group("Certificates", theme.IconCertificate,
				integer("Expiry warning (days)", func(c *config.Config) *int { return &c.Network.CertExpiryWarnDays }, 1, 365,
					"A certificate closer than this warns"),
			),
			group("Forward", theme.IconNetwork,
				integer("Proxy port", func(c *config.Config) *int { return &c.Network.ProxyPort }, config.MinProxyPort, config.MaxProxyPort,
					"Port shared by every named route, e.g. http://api.localhost:8080"),
			),
		)},

		// A tab of its own for two scalars. Every other tab is named after the
		// section it writes, and `mcp:` is a section — putting them under `app`
		// would be the only settings in the view whose tab does not say where
		// they land (§3.34's argument for renaming `docker:`). `expose` is a
		// list, so it stays in the file, where the monitors and the registries
		// also stay.
		//
		// There was a third row here, a static one reading
		// `dk mcp --context <name>`: what to point a client at, because a
		// setting whose effect needs a command nobody has been told about reads
		// as broken. §3.61 deleted the subcommand, and with it the row's reason
		// — over HTTP there is no command, the address *is* the answer, and it
		// is the editable field right below.
		{Title: mcpTab, Fields: group("Server", theme.IconServer,
			toggle("Enabled", func(c *config.Config) *bool { return &c.MCP.Enabled },
				"Serves this context to an MCP client over HTTP, while dk runs"),
			text("Listen", func(c *config.Config) *string { return &c.MCP.Listen },
				"Loopback — a sandboxed agent reaches it at host.docker.internal"),
			// What became of the two settings above. A setting whose effect
			// cannot be seen anywhere reads as broken, and this is the one
			// screen where somebody looks for it.
			static("State", mcpState(mcp),
				"Restart or switch context to apply a change to the two settings above"),
			secret("Token", mcp.Token,
				"space reveals it — Y copies the whole Claude Code connect command"),
		)},

		{Title: "status", Fields: group("Monitoring", theme.IconRefresh,
			integer("Refresh interval (s)", func(c *config.Config) *int { return &c.Status.RefreshInterval }, 1, 3600, ""),
			integer("Check timeout (s)", func(c *config.Config) *int { return &c.Status.Timeout }, 1, 300, ""),
			toggle("Auto refresh", func(c *config.Config) *bool { return &c.Status.AutoRefresh }, ""),
		)},
	}
}
