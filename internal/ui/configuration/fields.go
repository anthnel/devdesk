package configuration

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/anthnel/devdesk/internal/config"
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

	str  func(*config.Config) *string // text, cycle
	num  func(*config.Config) *int    // integer
	flag func(*config.Config) *bool   // toggle

	min, max int                // integer bounds, inclusive
	validate func(string) error // text only, beyond emptiness
	hint     string             // shown when the field is focused
}

// section is one tab.
type section struct {
	Title  string
	Fields []field
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

// ── reading and writing ─────────────────────────────────────────────────────

// Value renders the setting as the text the field edits.
func (f field) Value(c *config.Config) string {
	switch f.Kind {
	case kindInteger:
		return strconv.Itoa(*f.num(c))
	case kindToggle:
		return strconv.FormatBool(*f.flag(c))
	default:
		return *f.str(c)
	}
}

// Bool reads a toggle.
func (f field) Bool(c *config.Config) bool { return *f.flag(c) }

// Toggle flips a toggle and reports the new value.
func (f field) Toggle(c *config.Config) bool {
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
// declared — themes from a directory, views from the command parser.
func sections(themes, views []string) []section {
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
				text("Log file", func(c *config.Config) *string { return &c.App.LogFile }, ""),
			),
			group("External commands", theme.IconTools,
				text("IDE command", func(c *config.Config) *string { return &c.App.IDECommand },
					"Run by ctrl+o in the workspaces view"),
				text("Terminal command", func(c *config.Config) *string { return &c.App.TerminalCommand },
					"Empty auto-detects; e.g. kitty --directory"),
			),
			group("Secrets", theme.IconLock,
				cycle("Secret backend", func(c *config.Config) *string { return &c.App.SecretBackend },
					[]string{"auto", "keyring", "git-credential"},
					"Changing it does not migrate what is already stored"),
			),
		)},

		{Title: "gitlab", Fields: slices.Concat(
			group("Connection", theme.IconGitlab,
				text("URL", func(c *config.Config) *string { return &c.GitLab.URL },
					"e.g. https://gitlab.com"),
				text("Default parent group", func(c *config.Config) *string { return &c.GitLab.DefaultParentGroup }, ""),
				cycle("Default visibility", func(c *config.Config) *string { return &c.GitLab.DefaultVisibility },
					[]string{"private", "internal", "public"}, ""),
				cycle("Clone method", func(c *config.Config) *string { return &c.GitLab.CloneMethod },
					[]string{"https", "ssh"}, ""),
			),
			group("Pull", theme.IconGitBranch,
				text("Target dir", func(c *config.Config) *string { return &c.GitLab.Pull.TargetDir }, ""),
				integer("Parallel jobs", func(c *config.Config) *int { return &c.GitLab.Pull.ParallelJobs }, 1, 32, ""),
				integer("Max depth", func(c *config.Config) *int { return &c.GitLab.Pull.MaxDepth }, 1, 20, ""),
				toggle("Include archived projects", func(c *config.Config) *bool { return &c.GitLab.Pull.IncludeArchived }, ""),
			),
		)},

		{Title: "scan", Fields: slices.Concat(
			group("Scanners", theme.IconSecurity,
				toggle("Vulnerabilities", func(c *config.Config) *bool { return &c.Scan.EnableVuln }, "Trivy"),
				toggle("Secrets", func(c *config.Config) *bool { return &c.Scan.EnableSecret }, "Gitleaks"),
				toggle("Misconfiguration", func(c *config.Config) *bool { return &c.Scan.EnableMisconfig }, "Trivy"),
				toggle("Licenses", func(c *config.Config) *bool { return &c.Scan.EnableLicense }, "Trivy"),
			),
			group("Trivy", theme.IconTarget,
				cycle("Trivy source", func(c *config.Config) *string { return &c.Scan.TrivySource },
					[]string{config.ToolSourceAuto, config.ToolSourceBinary, config.ToolSourceImage},
					"binary fails rather than falling back to Docker"),
				text("Trivy binary", func(c *config.Config) *string { return &c.Scan.TrivyPath },
					"Empty resolves trivy on PATH"),
				text("Trivy image", func(c *config.Config) *string { return &c.Scan.TrivyImage },
					"Empty uses "+scan.DefaultTrivyImage),
				validated("Trivy server", func(c *config.Config) *string { return &c.Scan.TrivyServer },
					"Client-server mode; disables misconfig and license",
					func(v string) error { return scan.ValidateTrivyServer(v) }),
				toggle("Ignore unfixed", func(c *config.Config) *bool { return &c.Scan.IgnoreUnfixed }, ""),
				toggle("Ignore end-of-life", func(c *config.Config) *bool { return &c.Scan.IgnoreEOL }, ""),
			),
			group("Gitleaks", theme.IconToml,
				cycle("Gitleaks source", func(c *config.Config) *string { return &c.Scan.GitleaksSource },
					[]string{config.ToolSourceAuto, config.ToolSourceBinary, config.ToolSourceImage}, ""),
				text("Gitleaks binary", func(c *config.Config) *string { return &c.Scan.GitleaksPath },
					"Empty resolves gitleaks on PATH"),
				text("Gitleaks image", func(c *config.Config) *string { return &c.Scan.GitleaksImage },
					"Empty uses "+scan.DefaultGitleaksImage),
				text("Gitleaks config", func(c *config.Config) *string { return &c.Scan.GitleaksConfig },
					"Path to a .gitleaks.toml"),
				toggle("Scan git history", func(c *config.Config) *bool { return &c.Scan.GitleaksHistory }, "Slower"),
			),
			group("Limits", theme.IconHourglass,
				integer("Timeout (s)", func(c *config.Config) *int { return &c.Scan.Timeout }, 10, 3600, ""),
				integer("Max concurrent scans", func(c *config.Config) *int { return &c.Scan.MaxConcurrentScans }, 1, 16, ""),
				integer("Max cached reports", func(c *config.Config) *int { return &c.Scan.MaxCachedReports }, 1, 1000, ""),
			),
		)},

		{Title: "docker", Fields: group("Tools", theme.IconDocker,
			text("Network tool image", func(c *config.Config) *string { return &c.Docker.NetworkToolImage },
				"Must carry ping, curl and nc"),
		)},

		{Title: "status", Fields: group("Monitoring", theme.IconRefresh,
			integer("Refresh interval (s)", func(c *config.Config) *int { return &c.Status.RefreshInterval }, 1, 3600, ""),
			integer("Check timeout (s)", func(c *config.Config) *int { return &c.Status.Timeout }, 1, 300, ""),
			toggle("Auto refresh", func(c *config.Config) *bool { return &c.Status.AutoRefresh }, ""),
		)},
	}
}

// serverModeFields are the scan options the Trivy client-server protocol does
// not support. Setting a server address forces them off — a real constraint,
// not a defect, carried over from the security form it replaces.
var serverModeFields = map[string]bool{
	"Misconfiguration": true,
	"Licenses":         true,
}
