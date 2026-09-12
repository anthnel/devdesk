package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the application's complete configuration
type Config struct {
	App      AppConfig      `yaml:"app"`
	Status   StatusConfig   `yaml:"status"`
	Forge    ForgeConfig    `yaml:"forge"`
	Registry RegistryConfig `yaml:"registry"`
	Scan     ScanConfig     `yaml:"scan"`
	Network  NetworkConfig  `yaml:"network"`
	MCP      MCPConfig      `yaml:"mcp"`

	// GitLab is what `forge:` replaced. Same treatment as Docker above, and for
	// the same reason: a context targets one forge, so the section is named
	// after the role rather than after the one implementation there was.
	//
	// Deprecated: use Forge.
	GitLab GitLabConfig `yaml:"gitlab,omitempty"`
}

// MCPConfig governs the MCP server the TUI serves over HTTP (§3.61).
//
// It was a read-only stdio subcommand, `dk mcp` (§3.38). Three of that entry's
// decisions were reversed together: an agent running in a container cannot
// execute the host binary, which was §3.38's own open question 1 and became the
// only case anyone had.
//
// Enabled is false by default and that is the whole safety story: turning it on
// is the moment the user decides an agent may reach this context. Nothing
// migrates it, and nothing turns it on as a side effect.
//
// **What it means widened with §3.61, and nothing re-asks.** Under §3.38 it
// meant a read-only server on stdio, started by the client, with no listener at
// all; it now opens a loopback port and — `expose` being empty by default —
// serves the action tier as well. A context that said yes to the first is not
// asked again about the second, and a file written before §3.61 is recognisable
// (it carries no `listen`), so a narrower default was available and was
// deliberately not taken: the setting has always meant "an agent may reach this
// context", the widening is the entry's whole subject, and a migration would be
// ceremony around a decision its only user had just made. It is written here
// rather than left for someone to discover.
//
// There is no setting for the `Match` of a secret finding, and its absence is
// the guarantee. The entry proposed `redact_secret_matches: true` while also
// classing the string itself as never exposed — a setting whose other value is
// refused is a parameter that has to be ignored (§3.39). Worse, `false` is a
// bool's zero value, so every file written before the key existed would decode
// to "do not redact": the exact shape of D12. The tool schemas simply have
// nowhere to carry the string, which is the guarantee `context_get` already
// gets by construction (§3.9).
type MCPConfig struct {
	// Enabled decides whether the TUI serves at all. The refusal names this
	// setting and the context, because activating it in the wrong one is
	// otherwise an hour spent looking at a server that will not start.
	Enabled bool `yaml:"enabled"`

	// Expose is an **allow-list** of tool names; empty means every declared
	// tool. It is not a deny-list, for the reason §3.38 gives in the other
	// direction: a tool never registered cannot fail to be excluded, whereas a
	// deny-list is one forgotten line away from exposing what arrives next.
	Expose []string `yaml:"expose,omitempty"`

	// Listen is the address the server binds, and it is a loopback address
	// because that is both the most closed bind available and the one that
	// works: on Docker Desktop a container reaching `host.docker.internal`
	// arrives at the host's loopback, so a sandboxed agent is served without
	// the LAN ever being offered the port. The sandbox's own network policy is
	// a second lock — the port has to be allowed there by name.
	//
	// It is a setting rather than a literal for one reason, and it is not
	// configurability: on native Linux Docker `host.docker.internal` does not
	// reach the host's loopback, and the bridge gateway would have to be bound
	// instead. That case does not exist here and is not handled — but a literal
	// would have to be rewritten, where a setting has to be documented.
	//
	// An empty value means the default, never "listen nowhere": the zero value
	// of a string cannot be allowed to read as a choice, which is the shape of
	// D12. applyDefaults fills it.
	Listen string `yaml:"listen,omitempty"`
}

// DefaultMCPListen is where the server binds when the context does not say.
//
// Loopback, and a port high enough to be out of the way. It is named rather
// than inlined because applyDefaults and Default both need it, and a second
// literal is how the two drift.
const DefaultMCPListen = "127.0.0.1:7777"

// NetworkConfig holds what the netdiag view runs on.
type NetworkConfig struct {
	// CheckTimeout is how long one probe waits for an answer, in seconds.
	//
	// One setting rather than five: netcheck held 5 s for DNS and the dial, 8 s
	// for TLS and HTTP and 4 s for the ping, and nothing anywhere argued for the
	// split — it read as five separate guesses. The default is the old maximum,
	// so nothing that answers today starts failing; the cost is that an
	// unreachable host now spends 8 s on DNS instead of 5, which the staged
	// progress line makes legible rather than mysterious.
	CheckTimeout int `yaml:"check_timeout"`

	// PingCount is how many ICMP echo requests one reachability probe sends.
	PingCount int `yaml:"ping_count"`

	// CertExpiryWarnDays is how close a certificate may come to expiring before
	// the TLS check warns. Thirty days is a renewal cycle.
	CertExpiryWarnDays int `yaml:"cert_expiry_warn_days"`

	// PortsRefreshInterval is how often the Ports tab re-reads the socket
	// table, in seconds.
	PortsRefreshInterval int `yaml:"ports_refresh_interval"`
}

// Network defaults. They are the values internal/netcheck and the netdiag view
// held as constants before they became settings, so a config that predates them
// behaves exactly as it did — except CheckTimeout, which is the old maximum
// rather than any one of the five values it replaces.
const (
	DefaultCheckTimeout         = 8
	DefaultPingCount            = 3
	DefaultCertExpiryWarnDays   = 30
	DefaultPortsRefreshInterval = 2
)

// AppConfig holds the app's global settings
type AppConfig struct {
	Theme           string `yaml:"theme"`
	LogFile         string `yaml:"log_file"`
	DefaultView     string `yaml:"default_view"`
	WorkspacesDir   string `yaml:"workspaces_dir"`
	IDECommand      string `yaml:"ide_command"`
	TerminalCommand string `yaml:"terminal_command"` // e.g. "kitty --directory" — empty = auto-detect

	// TerminalNewWindow decides what T does: open a shell in place, suspending
	// the TUI (false, the default), or launch a separate terminal window (true).
	//
	// It is a setting rather than a second key because the capability depends on
	// the environment and not on the intent: no window can be opened under WSL,
	// and through SSH there is none to open. A key that is inert on two setups
	// out of three is worse than a setting that is simply off there (§3.26).
	TerminalNewWindow bool `yaml:"terminal_new_window"`

	// ShowHiddenFiles decides whether the workspaces view lists entries whose
	// name starts with a dot. False is the default because that is what the
	// view has always done, so an existing config keeps its meaning and there
	// is nothing to migrate.
	//
	// It governs the listing *and* the nested-repo discovery behind S, F and A:
	// what the view shows is what those act on, and two rules for one question
	// would let a repository be a visible row and an invisible target at once.
	ShowHiddenFiles bool `yaml:"show_hidden_files"`

	// SecretBackend pins where secrets are stored: "auto" (default), "keyring"
	// for the host secret manager only, or "git-credential" for git's helper.
	// See credentials.Select for what each one resolves to.
	SecretBackend string `yaml:"secret_backend"`

	// ContainerEngine names the engine DevDesk drives: "auto" (default, docker
	// if it is on PATH and podman otherwise), "docker", "podman", or an
	// explicit path to a binary. See engine.Resolve.
	//
	// It sits in `app` beside ide_command and terminal_command — this section
	// already holds the external tools the application drives — and
	// deliberately not in `network`, which carries only netcheck's dials
	// despite once being called `docker` (§3.67).
	ContainerEngine string `yaml:"container_engine"`
}

// ForgeConfig is the code-hosting platform this context targets — exactly one,
// never two (§3.6).
//
// The token is not here: it lives in the host's secret manager
// (§3.9). A `token:` left by an earlier version is moved into the
// store and then removed from the file — see secrets.go.
type ForgeConfig struct {
	// Type names the backend: "gitlab" or "github". It is **declared, never
	// sniffed** — the registry `provider` field is the precedent (§3.8), and
	// `git.acme.com` is exactly the URL that cannot be told apart.
	//
	// Empty means "gitlab", which is what every file written before this key
	// existed meant.
	Type string `yaml:"type"`

	URL                string          `yaml:"url"`
	DefaultParentGroup string          `yaml:"default_parent_group"`
	DefaultVisibility  string          `yaml:"default_visibility"`
	CloneMethod        string          `yaml:"clone_method"`
	Pull               ForgePullConfig `yaml:"pull"`
}

// ForgePullConfig holds the configuration for synchronization
type ForgePullConfig struct {
	ParallelJobs    int  `yaml:"parallel_jobs"`
	IncludeArchived bool `yaml:"include_archived"`
}

// GitLabConfig is the shape `gitlab:` had. It is read at load and migrated into
// ForgeConfig; nothing else may use it.
//
// Deprecated: use ForgeConfig.
type GitLabConfig struct {
	URL                string           `yaml:"url,omitempty"`
	DefaultParentGroup string           `yaml:"default_parent_group,omitempty"`
	DefaultVisibility  string           `yaml:"default_visibility,omitempty"`
	CloneMethod        string           `yaml:"clone_method,omitempty"`
	Pull               GitLabPullConfig `yaml:"pull,omitempty"`
}

// GitLabPullConfig is `gitlab.pull:`.
//
// Deprecated: use ForgePullConfig.
type GitLabPullConfig struct {
	ParallelJobs    int  `yaml:"parallel_jobs,omitempty"`
	IncludeArchived bool `yaml:"include_archived,omitempty"`
}

// RegistryItem represents a single Docker/OCI registry with optional alias support.
// The password is NOT stored here; it is persisted via `docker login` / system credential helper.
type RegistryItem struct {
	// Slug identifies the entry inside DevDesk: it is what a member points at
	// and what the group cache is keyed on. Everything Docker-facing stays keyed
	// on the URL, because that is what Docker itself is keyed on — which is
	// exactly why the URL is the wrong thing to hang a parent link on, editing
	// one would silently orphan the group's members (§3.8, decision 1).
	// Filled in at load for a config that predates it; see registries.go.
	Slug string `yaml:"slug"`
	// Kind is "registry" or "group". A group fronts several registries and is
	// pullable itself, so both kinds share this one list (§3.8, decision A).
	Kind string `yaml:"kind,omitempty"`
	// Parent is the slug of the group this entry belongs to. Config entries are
	// what the user declares and normally carry none; it is discovered members,
	// which live in the group cache, that point back at their group.
	Parent string `yaml:"parent,omitempty"`
	// Provider is the repository manager serving a group — nexus, harbor,
	// artifactory, gitlab or generic. Declared rather than sniffed from the URL.
	Provider string `yaml:"provider,omitempty"`
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	Alias    string `yaml:"alias"`
	// RepoPrefix is what goes in front of the repository name to reach this
	// entry: `(url, repo_prefix)` is the whole address, and browse and pull both
	// derive from it, which is what stops them meaning two different repositories
	// (D39, §3.18).
	//
	// It is declared rather than sniffed, and for the same reason `provider` is:
	// whether a repository manager answers on a path prefix, a dedicated
	// connector port or a subdomain is a setting on that repository, and the one
	// endpoint that would say so is the one an ordinary pull account is refused.
	// Empty is the ordinary case — a registry reached at its own host.
	RepoPrefix string `yaml:"repo_prefix,omitempty"`
	// AuthMode says whether DevDesk may send stored credentials to this entry:
	// "credentials" or "anonymous", plus "inherit" for a member that takes its
	// group's. It is the only per-member override the credential store can
	// represent — see registries.go.
	AuthMode string `yaml:"auth_mode"`
	// AuthEnabled is what auth_mode replaced. It is read once at load, migrated
	// into AuthMode and cleared, so it disappears from the file on the next save.
	//
	// Deprecated: use AuthMode.
	AuthEnabled bool `yaml:"auth_enabled,omitempty"`
	// ManagementURL is optional. Set it when the Docker registry URL differs from the
	// URL used by the repository manager's API (e.g. Nexus connector subdomains,
	// Artifactory virtual repos). The group member discovery uses this URL instead of
	// the registry URL. Example: "https://nexus.example.com/repository/docker-group".
	ManagementURL string `yaml:"management_url,omitempty"`
}

// RegistryConfig holds the OCI registry configuration
//
// As with GitLabConfig, the password is not here (§3.9).
type RegistryConfig struct {
	URL                 string         `yaml:"url,omitempty"`
	Username            string         `yaml:"username,omitempty"`
	TemplatesRepository string         `yaml:"templates_repository"`
	CacheDir            string         `yaml:"cache_dir"`
	Registries          []RegistryItem `yaml:"registries,omitempty"`
}

// Tool source preferences. They say where a scanner is run from, which used to
// be decided for the user: detection resolved the binary first and only reached
// for Docker in the else, so a binary on PATH always won (D27).
const (
	// ToolSourceAuto keeps the historical resolution: the binary when there is
	// one, the Docker image otherwise. The default, so existing configs do not
	// change meaning.
	ToolSourceAuto = "auto"
	// ToolSourceBinary runs the configured path, else the name on PATH — and
	// fails when neither exists rather than falling back to Docker. The silent
	// fallback is what kept D27 invisible.
	ToolSourceBinary = "binary"
	// ToolSourceImage runs the Docker image even when a binary is installed.
	ToolSourceImage = "image"
)

// ScanConfig holds the configuration for security scans
type ScanConfig struct {
	TrivySource        string `yaml:"trivy_source"`         // auto | binary | image
	TrivyPath          string `yaml:"trivy_path"`           // Custom path to trivy (optional)
	TrivyImage         string `yaml:"trivy_image"`          // Docker image for trivy (default: aquasec/trivy)
	UseTrivyServer     bool   `yaml:"use_trivy_server"`     // Enables client-server mode; trivy_server is ignored if false
	TrivyServer        string `yaml:"trivy_server"`         // Trivy server URL (client-server mode)
	GitleaksSource     string `yaml:"gitleaks_source"`      // auto | binary | image
	GitleaksPath       string `yaml:"gitleaks_path"`        // Custom path to gitleaks (optional)
	GitleaksImage      string `yaml:"gitleaks_image"`       // Docker image for gitleaks (default: zricethezav/gitleaks)
	PlumberSource      string `yaml:"plumber_source"`       // auto | binary | image
	PlumberPath        string `yaml:"plumber_path"`         // Custom path to plumber (optional)
	PlumberImage       string `yaml:"plumber_image"`        // Docker image for plumber (default: getplumber/plumber)
	CacheDir           string `yaml:"cache_dir"`            // Report cache
	MaxCachedReports   int    `yaml:"max_cached_reports"`   // Max number of reports kept
	Timeout            int    `yaml:"timeout"`              // Timeout in seconds
	MaxConcurrentScans int    `yaml:"max_concurrent_scans"` // Max number of parallel scans

	// Scan options (persisted from the security view)
	EnableVuln      bool   `yaml:"enable_vuln"`
	EnableSecret    bool   `yaml:"enable_secret"`
	EnableMisconfig bool   `yaml:"enable_misconfig"`
	EnableLicense   bool   `yaml:"enable_license"`
	IgnoreUnfixed   bool   `yaml:"ignore_unfixed"`
	IgnoreEOL       bool   `yaml:"ignore_eol"`
	GitleaksHistory bool   `yaml:"gitleaks_history"`
	GitleaksConfig  string `yaml:"gitleaks_config"`

	// EnableCIScore runs plumber over the repository's CI configuration.
	//
	// The "all disabled means never configured" migration reads it and never
	// writes it. Reading it, because "the four off and CI on" is a deliberate
	// configuration that must not have vuln and secret forced back on; never
	// writing it, because this key is newer than the four and an absent
	// enable_ci_score means the user has not asked for it, not that the file
	// predates the question.
	EnableCIScore bool `yaml:"enable_ci_score"`
	// PlumberConfig is the rules file for every repository of the context, in
	// place of the .plumber.yaml plumber looks for in each one. Made absolute at
	// load for §3.50's reason: a relative path means DevDesk's working directory
	// in binary mode and the container's in Docker mode.
	PlumberConfig string `yaml:"plumber_config"`
}

// StatusConfig holds the configuration for monitoring
type StatusConfig struct {
	RefreshInterval int               `yaml:"refresh_interval"`
	Timeout         int               `yaml:"timeout"` // in seconds
	AutoRefresh     bool              `yaml:"auto_refresh"`
	Components      []ComponentConfig `yaml:"components"`
}

// ComponentConfig represents a component to monitor
type ComponentConfig struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`              // http, https, icmp, dns
	Target  string `yaml:"target"`            // URL, IP, or hostname
	Timeout int    `yaml:"timeout,omitempty"` // In seconds

	// Legacy support
	URL string `yaml:"url,omitempty"` // Deprecated, use Target

	// ICMP specific
	Count int `yaml:"count,omitempty"` // Number of pings

	// DNS specific
	Nameserver string `yaml:"nameserver,omitempty"` // Custom DNS server
}

// Load loads the configuration from the current context
func Load() (*Config, error) {
	ctx, err := GetCurrentContext()
	if err != nil {
		// Fallback to default on error
		ctx = "default"
	}

	cfg, err := LoadContext(ctx)
	if err != nil {
		// If the file does not exist, return the default config
		if os.IsNotExist(err) || strings.Contains(err.Error(), "does not exist") {
			return Default(), nil
		}
		// Otherwise (parsing error, permission error, etc.), propagate the error
		return nil, err
	}

	return cfg, nil
}

// applyDefaults applies the default values to a configuration.
// Returns an error when the file cannot be normalized — today
// only for registries (duplicate slug, missing parent group).
func applyDefaults(cfg *Config) error {
	homeDir, _ := os.UserHomeDir()

	// `gitlab:` → `forge:`, and it runs **first**. That is not tidiness: every
	// default below writes into cfg.Forge, so a migration placed after them
	// would find a block that is no longer empty and take the field-by-field
	// path — where every field is already filled with a default, and the user's
	// own `parallel_jobs: 7` is silently dropped. Measured, not theorised: it
	// is what the first draft did, and the retired-keys test caught it.
	//
	// The other half of the reason is `docker:` → `network:`'s: yaml.Unmarshal
	// is not strict here, so an un-migrated block is dropped in silence, and the
	// silence would point a context at no host at all.
	migrateGitLabSection(cfg)

	// "dark" was a third name for the built-in theme: LoadTheme accepts "",
	// "dark" and "default" alike, but ListThemes only ever offers "default", so
	// a config saying "dark" named a theme no picker could show. Normalised at
	// load; LoadTheme still accepts the old name for a file not yet rewritten.
	if cfg.App.Theme == "" || cfg.App.Theme == "dark" {
		cfg.App.Theme = "default"
	}
	if cfg.App.LogFile == "" {
		cfg.App.LogFile = filepath.Join(homeDir, ".devdesk", "devdesk.log")
	}
	if cfg.App.DefaultView == "" {
		cfg.App.DefaultView = "dashboard"
	}
	if cfg.App.WorkspacesDir == "" {
		cfg.App.WorkspacesDir = filepath.Join(homeDir, "workspaces")
	}
	if cfg.App.IDECommand == "" {
		cfg.App.IDECommand = "code"
	}
	// Left empty, credentials.Select already treats this as "auto"; naming it
	// keeps the setting inside the closed set the configuration view cycles
	// through, so the first arrow press does not jump somewhere arbitrary.
	if cfg.App.SecretBackend == "" {
		cfg.App.SecretBackend = "auto"
	}
	// Same reason: engine.Resolve reads "" as auto, but the configuration view
	// cycles a closed set and a value outside it has nowhere to start from.
	if cfg.App.ContainerEngine == "" {
		cfg.App.ContainerEngine = EngineAuto
	}
	if cfg.Status.RefreshInterval == 0 {
		cfg.Status.RefreshInterval = 10
	}
	if cfg.Status.Timeout == 0 {
		cfg.Status.Timeout = 5
	}
	if cfg.Forge.Type == "" {
		cfg.Forge.Type = ForgeGitLab
	}
	if cfg.Forge.DefaultVisibility == "" {
		cfg.Forge.DefaultVisibility = "private"
	}
	if cfg.Forge.CloneMethod == "" {
		cfg.Forge.CloneMethod = "https"
	}
	if cfg.Forge.Pull.ParallelJobs == 0 {
		cfg.Forge.Pull.ParallelJobs = 4
	}
	if cfg.Registry.CacheDir == "" {
		cfg.Registry.CacheDir = filepath.Join(homeDir, ".devdesk", "cache", "templates")
	}
	// Migrate old single-registry config to the new Registries list
	if cfg.Registry.URL != "" && len(cfg.Registry.Registries) == 0 {
		cfg.Registry.Registries = []RegistryItem{{
			URL:      cfg.Registry.URL,
			Username: cfg.Registry.Username,
			AuthMode: AuthCredentials,
		}}
	}
	if cfg.Scan.CacheDir == "" {
		cfg.Scan.CacheDir = filepath.Join(homeDir, ".devdesk", "cache", "scans")
	}
	if cfg.Scan.MaxCachedReports == 0 {
		cfg.Scan.MaxCachedReports = 50
	}
	if cfg.Scan.Timeout == 0 {
		cfg.Scan.Timeout = 300
	}
	if cfg.Scan.MaxConcurrentScans == 0 {
		cfg.Scan.MaxConcurrentScans = 3
	}
	// An address the file does not carry is the default, never "nowhere":
	// a bind that silently does not happen would look exactly like a server
	// that is off, and `mcp.enabled` is what answers that question.
	if cfg.MCP.Listen == "" {
		cfg.MCP.Listen = DefaultMCPListen
	}

	// Auto is the historical resolution, so a config that predates the setting
	// keeps behaving exactly as it did.
	if cfg.Scan.TrivySource == "" {
		cfg.Scan.TrivySource = ToolSourceAuto
	}
	if cfg.Scan.GitleaksSource == "" {
		cfg.Scan.GitleaksSource = ToolSourceAuto
	}
	if cfg.Scan.PlumberSource == "" {
		cfg.Scan.PlumberSource = ToolSourceAuto
	}

	if cfg.Network.CheckTimeout == 0 {
		cfg.Network.CheckTimeout = DefaultCheckTimeout
	}
	if cfg.Network.PingCount == 0 {
		cfg.Network.PingCount = DefaultPingCount
	}
	if cfg.Network.CertExpiryWarnDays == 0 {
		cfg.Network.CertExpiryWarnDays = DefaultCertExpiryWarnDays
	}
	if cfg.Network.PortsRefreshInterval == 0 {
		cfg.Network.PortsRefreshInterval = DefaultPortsRefreshInterval
	}

	// Backward compat: configs created before the scan-option booleans were introduced
	// will have all of them at Go's zero value (false). Treat "all disabled" as
	// "never configured" and apply sensible defaults so scans work out of the box.
	//
	// EnableCIScore is read here but never written below, and the asymmetry is
	// the point. It is newer than the four, so a file that predates them has it
	// false too and the migration still fires; but "the four off and CI on" is a
	// deliberate configuration — the user asked for CI alone — and forcing vuln
	// and secret back on would overwrite it on every load. Leaving it out of the
	// condition is what a test caught.
	if !cfg.Scan.EnableVuln && !cfg.Scan.EnableSecret && !cfg.Scan.EnableMisconfig &&
		!cfg.Scan.EnableLicense && !cfg.Scan.EnableCIScore {
		cfg.Scan.EnableVuln = true
		cfg.Scan.EnableSecret = true
	}

	// Last, so that the registry migrated from the legacy single-registry keys
	// above gets a slug like every other one.
	return normalizeRegistries(cfg.Registry.Registries)
}

// Default returns the default configuration
func Default() *Config {
	homeDir, _ := os.UserHomeDir()
	return &Config{
		App: AppConfig{
			Theme:           "default",
			LogFile:         filepath.Join(homeDir, ".devdesk", "devdesk.log"),
			DefaultView:     "dashboard",
			WorkspacesDir:   filepath.Join(homeDir, "workspaces"),
			IDECommand:      "code",
			SecretBackend:   "auto",
			ContainerEngine: EngineAuto,
		},
		Status: StatusConfig{
			RefreshInterval: 10,
			AutoRefresh:     true,
			Timeout:         5,
			Components:      []ComponentConfig{},
		},
		Forge: ForgeConfig{
			Type:              ForgeGitLab,
			DefaultVisibility: "private",
			CloneMethod:       "https",
			Pull: ForgePullConfig{
				ParallelJobs:    4,
				IncludeArchived: false,
			},
		},
		Registry: RegistryConfig{
			CacheDir: filepath.Join(homeDir, ".devdesk", "cache", "templates"),
		},
		Scan: ScanConfig{
			TrivySource:        ToolSourceAuto,
			TrivyPath:          "", // Auto-detect in PATH
			GitleaksSource:     ToolSourceAuto,
			GitleaksPath:       "", // Auto-detect in PATH
			PlumberSource:      ToolSourceAuto,
			PlumberPath:        "", // Auto-detect in PATH
			CacheDir:           filepath.Join(homeDir, ".devdesk", "cache", "scans"),
			MaxCachedReports:   50,
			Timeout:            300, // 5 minutes
			MaxConcurrentScans: 3,
			EnableVuln:         true,
			EnableSecret:       true,
		},
		MCP: MCPConfig{
			Listen: DefaultMCPListen,
		},
		Network: NetworkConfig{
			CheckTimeout:         DefaultCheckTimeout,
			PingCount:            DefaultPingCount,
			CertExpiryWarnDays:   DefaultCertExpiryWarnDays,
			PortsRefreshInterval: DefaultPortsRefreshInterval,
		},
	}
}

// ConfigDir returns the configuration directory
func ConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".devdesk"), nil
}

// EnsureConfigDir creates the configuration directory if it does not exist
func EnsureConfigDir() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}

	return os.MkdirAll(dir, 0755)
}

// Save saves the configuration to the current context's file
func Save(cfg *Config) error {
	ctx, err := GetCurrentContext()
	if err != nil {
		// Fallback to default on error
		ctx = "default"
	}

	return SaveContext(cfg, ctx)
}

// Context Management Functions

// GetCurrentContext reads the current context name from .current-context
// Returns "default" if the file does not exist or is empty
func GetCurrentContext() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	contextFile := filepath.Join(homeDir, ".devdesk", ".current-context")

	// If the file does not exist, return "default"
	if _, err := os.Stat(contextFile); os.IsNotExist(err) {
		return "default", nil
	}

	data, err := os.ReadFile(contextFile)
	if err != nil {
		return "", err
	}

	context := strings.TrimSpace(string(data))
	if context == "" {
		return "default", nil
	}

	return context, nil
}

// CurrentContextName returns the current context, falling back to "default"
// when it cannot be read.
//
// Everything that needs the context name to scope something — Load, Save, the
// scan caches — wants a name and has no use for the error, since a home
// directory that cannot be resolved leaves nothing better to do than assume the
// default context. Three copies of that fallback existed before this.
func CurrentContextName() string {
	ctx, err := GetCurrentContext()
	if err != nil {
		return "default"
	}
	return ctx
}

// SetCurrentContext writes the context name to .current-context
func SetCurrentContext(name string) error {
	if err := ValidateContextName(name); err != nil {
		return err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	contextFile := filepath.Join(homeDir, ".devdesk", ".current-context")

	// Ensure the directory exists
	if err := EnsureConfigDir(); err != nil {
		return err
	}

	return os.WriteFile(contextFile, []byte(name), 0600)
}

// ListContexts discovers the available contexts by listing config-*.yaml files
// Returns a slice of context names (without the "config-" prefix or ".yaml" suffix)
// Always includes "default" if config.yaml exists
func ListContexts() ([]string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	configDir := filepath.Join(homeDir, ".devdesk")
	contexts := []string{}

	// Check the default context
	defaultPath := filepath.Join(configDir, "config.yaml")
	if _, err := os.Stat(defaultPath); err == nil {
		contexts = append(contexts, "default")
	}

	// Glob for named contexts
	pattern := filepath.Join(configDir, "config-*.yaml")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	for _, match := range matches {
		// Extract the name: config-{name}.yaml -> {name}
		basename := filepath.Base(match)
		name := strings.TrimPrefix(basename, "config-")
		name = strings.TrimSuffix(name, ".yaml")
		contexts = append(contexts, name)
	}

	return contexts, nil
}

// LoadContext loads the configuration for a specific context
// contextName = "default" -> config.yaml
// contextName = "dev" -> config-dev.yaml
func LoadContext(contextName string) (*Config, error) {
	if err := ValidateContextName(contextName); err != nil {
		return nil, err
	}

	configPath, err := GetContextPath(contextName)
	if err != nil {
		return nil, err
	}

	// Check whether the file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("context '%s' does not exist (no file at %s)", contextName, configPath)
	}

	// Read and parse YAML
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse context '%s': %w", contextName, err)
	}

	// Apply the default values
	if err := applyDefaults(&cfg); err != nil {
		return nil, fmt.Errorf("invalid context '%s': %w", contextName, err)
	}

	// Expand paths (tildes)
	if home, err := os.UserHomeDir(); err == nil {
		cfg.ExpandPaths(home)
	}

	return &cfg, nil
}

// ExpandPaths expands tildes (~) in configuration paths
func (c *Config) ExpandPaths(homeDir string) {
	expand := func(path string) string {
		if strings.HasPrefix(path, "~/") {
			return filepath.Join(homeDir, path[2:])
		}
		return path
	}

	c.App.LogFile = expand(c.App.LogFile)
	c.App.WorkspacesDir = expand(c.App.WorkspacesDir)
	c.Registry.CacheDir = expand(c.Registry.CacheDir)
	c.Scan.CacheDir = expand(c.Scan.CacheDir)
	c.Scan.TrivyPath = expand(c.Scan.TrivyPath)
	c.Scan.GitleaksPath = expand(c.Scan.GitleaksPath)
	c.Scan.GitleaksConfig = absolute(expand(c.Scan.GitleaksConfig))
	c.Scan.PlumberConfig = absolute(expand(c.Scan.PlumberConfig))
}

// absolute pins a configured file path to one meaning.
//
// A relative gitleaks_config does not mean the same thing on both sides of the
// Docker boundary — DevDesk's working directory in binary mode, the container's
// in Docker mode — and the file is now mounted, so the two readings would
// disagree about which file is scanned with. Resolving it here settles it
// against DevDesk's own working directory, once, at load: the path that gets
// mounted and the path the configuration view shows are then the same string.
//
// A path that cannot be resolved is left as it was rather than dropped:
// RunGitleaks refuses an unreadable config with the reason, and that is a
// better place to fail than a config file that silently loses a setting.
func absolute(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// SaveContext saves the configuration to a specific context file
func SaveContext(cfg *Config, contextName string) error {
	if err := ValidateContextName(contextName); err != nil {
		return err
	}

	configPath, err := GetContextPath(contextName)
	if err != nil {
		return err
	}

	// Ensure the directory exists
	if err := EnsureConfigDir(); err != nil {
		return err
	}

	// Marshal to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	// Write the file
	return os.WriteFile(configPath, data, 0600)
}

// ContextExists checks whether a context configuration file exists
func ContextExists(contextName string) (bool, error) {
	if err := ValidateContextName(contextName); err != nil {
		return false, err
	}

	configPath, err := GetContextPath(contextName)
	if err != nil {
		return false, err
	}

	_, err = os.Stat(configPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// GetContextPath returns the file path for a context
// "default" -> ~/.devdesk/config.yaml
// "dev" -> ~/.devdesk/config-dev.yaml
func GetContextPath(contextName string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	configDir := filepath.Join(homeDir, ".devdesk")

	var filename string
	if contextName == "default" {
		filename = "config.yaml"
	} else {
		filename = fmt.Sprintf("config-%s.yaml", contextName)
	}

	return filepath.Join(configDir, filename), nil
}

// ValidateContextName validates the format of the context name
// Must be "default" or match ^[a-z0-9-]+$
func ValidateContextName(name string) error {
	if name == "default" {
		return nil
	}

	// Pattern: lowercase letters, digits, hyphens
	matched, err := regexp.MatchString("^[a-z0-9-]+$", name)
	if err != nil {
		return err
	}

	if !matched {
		return fmt.Errorf("invalid context name '%s': must contain only lowercase letters, numbers, and hyphens", name)
	}

	return nil
}

// CreateContext creates a new context with a default configuration
// Empty monitors, empty GitLab URL, other values default
func CreateContext(contextName string) error {
	if err := ValidateContextName(contextName); err != nil {
		return err
	}

	// Check whether it already exists
	exists, err := ContextExists(contextName)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("context '%s' already exists", contextName)
	}

	// Create config with defaults
	cfg := Default()

	// Clear monitors and forge for the new context
	cfg.Status.Components = []ComponentConfig{}
	cfg.Forge.URL = ""

	// Save
	return SaveContext(cfg, contextName)
}
