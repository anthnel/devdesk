package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config représente la configuration complète de l'application
type Config struct {
	App      AppConfig      `yaml:"app"`
	Status   StatusConfig   `yaml:"status"`
	GitLab   GitLabConfig   `yaml:"gitlab"`
	Registry RegistryConfig `yaml:"registry"`
	Scan     ScanConfig     `yaml:"scan"`
	Docker   DockerConfig   `yaml:"docker"`
}

// DockerConfig contient les paramètres Docker de l'application
type DockerConfig struct {
	// NetworkToolImage is the Docker image used for network connectivity tests.
	// It must include ping, curl, and nc (netcat) binaries.
	NetworkToolImage string `yaml:"network_tool_image"`
}

// AppConfig contient les paramètres globaux de l'app
type AppConfig struct {
	Theme           string `yaml:"theme"`
	LogFile         string `yaml:"log_file"`
	DefaultView     string `yaml:"default_view"`
	WorkspacesDir   string `yaml:"workspaces_dir"`
	IDECommand      string `yaml:"ide_command"`
	TerminalCommand string `yaml:"terminal_command"` // e.g. "kitty --directory" — empty = auto-detect

	// SecretBackend pins where secrets are stored: "auto" (default), "keyring"
	// for the host secret manager only, or "git-credential" for git's helper.
	// See credentials.Select for what each one resolves to.
	SecretBackend string `yaml:"secret_backend"`
}

// GitLabConfig contient la configuration GitLab
//
// Le token n'est pas ici : il vit dans le gestionnaire de secrets de l'hôte
// (§3.9). Un `token:` laissé par une version antérieure est déplacé dans le
// store puis retiré du fichier — voir secrets.go.
type GitLabConfig struct {
	URL                string           `yaml:"url"`
	DefaultParentGroup string           `yaml:"default_parent_group"`
	DefaultVisibility  string           `yaml:"default_visibility"`
	CloneMethod        string           `yaml:"clone_method"`
	Pull               GitLabPullConfig `yaml:"pull"`
}

// GitLabPullConfig contient la configuration pour la synchronisation
type GitLabPullConfig struct {
	TargetDir       string `yaml:"target_dir"`
	ParallelJobs    int    `yaml:"parallel_jobs"`
	MaxDepth        int    `yaml:"max_depth"`
	IncludeArchived bool   `yaml:"include_archived"`
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

// RegistryConfig contient la configuration du registre OCI
//
// Comme pour GitLabConfig, le mot de passe n'est pas ici (§3.9).
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

// ScanConfig contient la configuration pour les scans de sécurité
type ScanConfig struct {
	TrivySource        string `yaml:"trivy_source"`         // auto | binary | image
	TrivyPath          string `yaml:"trivy_path"`           // Chemin custom vers trivy (optionnel)
	TrivyImage         string `yaml:"trivy_image"`          // Image Docker trivy (défaut: aquasec/trivy)
	TrivyServer        string `yaml:"trivy_server"`         // URL du serveur Trivy (optionnel, mode client-serveur)
	GitleaksSource     string `yaml:"gitleaks_source"`      // auto | binary | image
	GitleaksPath       string `yaml:"gitleaks_path"`        // Chemin custom vers gitleaks (optionnel)
	GitleaksImage      string `yaml:"gitleaks_image"`       // Image Docker gitleaks (défaut: zricethezav/gitleaks)
	CacheDir           string `yaml:"cache_dir"`            // Cache des rapports
	SBOMOutputDir      string `yaml:"sbom_output_dir"`      // Répertoire de sortie SBOM (optionnel, défaut: à côté de la cible)
	MaxCachedReports   int    `yaml:"max_cached_reports"`   // Nombre max de rapports conservés
	Timeout            int    `yaml:"timeout"`              // Timeout en secondes
	MaxConcurrentScans int    `yaml:"max_concurrent_scans"` // Nombre max de scans parallèles

	// Scan options (persistées depuis la vue security)
	EnableVuln      bool   `yaml:"enable_vuln"`
	EnableSecret    bool   `yaml:"enable_secret"`
	EnableMisconfig bool   `yaml:"enable_misconfig"`
	EnableLicense   bool   `yaml:"enable_license"`
	GenerateSBOM    bool   `yaml:"generate_sbom"`
	IgnoreUnfixed   bool   `yaml:"ignore_unfixed"`
	IgnoreEOL       bool   `yaml:"ignore_eol"`
	GitleaksHistory bool   `yaml:"gitleaks_history"`
	GitleaksConfig  string `yaml:"gitleaks_config"`
}

// StatusConfig contient la configuration pour le monitoring
type StatusConfig struct {
	RefreshInterval int               `yaml:"refresh_interval"`
	Timeout         int               `yaml:"timeout"` // in seconds
	AutoRefresh     bool              `yaml:"auto_refresh"`
	Components      []ComponentConfig `yaml:"components"`
}

// ComponentConfig représente un composant à monitorer
type ComponentConfig struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`              // http, https, icmp, dns
	Target  string `yaml:"target"`            // URL, IP, ou hostname
	Timeout int    `yaml:"timeout,omitempty"` // En secondes

	// Legacy support
	URL string `yaml:"url,omitempty"` // Deprecated, use Target

	// ICMP specific
	Count int `yaml:"count,omitempty"` // Nombre de pings

	// DNS specific
	Nameserver string `yaml:"nameserver,omitempty"` // Serveur DNS custom
}

// Load charge la configuration depuis le contexte actuel
func Load() (*Config, error) {
	ctx, err := GetCurrentContext()
	if err != nil {
		// Fallback to default on error
		ctx = "default"
	}

	cfg, err := LoadContext(ctx)
	if err != nil {
		// Si le fichier n'existe pas, retourner config par défaut
		if os.IsNotExist(err) || strings.Contains(err.Error(), "does not exist") {
			return Default(), nil
		}
		// Sinon (parsing error, permission error, etc.), propager l'erreur
		return nil, err
	}

	return cfg, nil
}

// applyDefaults applique les valeurs par défaut à une configuration.
// Retourne une erreur quand le fichier ne peut pas être normalisé — aujourd'hui
// uniquement pour les registres (slug dupliqué, groupe parent absent).
func applyDefaults(cfg *Config) error {
	homeDir, _ := os.UserHomeDir()

	if cfg.App.Theme == "" {
		cfg.App.Theme = "dark"
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
	if cfg.Status.RefreshInterval == 0 {
		cfg.Status.RefreshInterval = 10
	}
	if cfg.Status.Timeout == 0 {
		cfg.Status.Timeout = 5
	}
	if cfg.GitLab.DefaultVisibility == "" {
		cfg.GitLab.DefaultVisibility = "private"
	}
	if cfg.GitLab.CloneMethod == "" {
		cfg.GitLab.CloneMethod = "https"
	}
	if cfg.GitLab.Pull.ParallelJobs == 0 {
		cfg.GitLab.Pull.ParallelJobs = 4
	}
	if cfg.GitLab.Pull.MaxDepth == 0 {
		cfg.GitLab.Pull.MaxDepth = 5
	}
	if cfg.GitLab.Pull.TargetDir == "" {
		cfg.GitLab.Pull.TargetDir = filepath.Join(homeDir, "workspace")
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
	// Auto is the historical resolution, so a config that predates the setting
	// keeps behaving exactly as it did.
	if cfg.Scan.TrivySource == "" {
		cfg.Scan.TrivySource = ToolSourceAuto
	}
	if cfg.Scan.GitleaksSource == "" {
		cfg.Scan.GitleaksSource = ToolSourceAuto
	}

	if cfg.Docker.NetworkToolImage == "" {
		cfg.Docker.NetworkToolImage = "nicolaka/netshoot"
	}

	// Backward compat: configs created before the scan-option booleans were introduced
	// will have all of them at Go's zero value (false). Treat "all disabled" as
	// "never configured" and apply sensible defaults so scans work out of the box.
	if !cfg.Scan.EnableVuln && !cfg.Scan.EnableSecret && !cfg.Scan.EnableMisconfig && !cfg.Scan.EnableLicense {
		cfg.Scan.EnableVuln = true
		cfg.Scan.EnableSecret = true
	}

	// Last, so that the registry migrated from the legacy single-registry keys
	// above gets a slug like every other one.
	return normalizeRegistries(cfg.Registry.Registries)
}

// Default retourne la configuration par défaut
func Default() *Config {
	homeDir, _ := os.UserHomeDir()
	return &Config{
		App: AppConfig{
			Theme:         "dark",
			LogFile:       filepath.Join(homeDir, ".devdesk", "devdesk.log"),
			DefaultView:   "dashboard",
			WorkspacesDir: filepath.Join(homeDir, "workspaces"),
			IDECommand:    "code",
		},
		Status: StatusConfig{
			RefreshInterval: 10,
			AutoRefresh:     true,
			Timeout:         5,
			Components:      []ComponentConfig{},
		},
		GitLab: GitLabConfig{
			DefaultVisibility: "private",
			CloneMethod:       "https",
			Pull: GitLabPullConfig{
				TargetDir:       filepath.Join(homeDir, "workspace"),
				ParallelJobs:    4,
				MaxDepth:        5,
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
			CacheDir:           filepath.Join(homeDir, ".devdesk", "cache", "scans"),
			MaxCachedReports:   50,
			Timeout:            300, // 5 minutes
			MaxConcurrentScans: 3,
			EnableVuln:         true,
			EnableSecret:       true,
		},
		Docker: DockerConfig{
			NetworkToolImage: "nicolaka/netshoot",
		},
	}
}

// ConfigDir retourne le répertoire de configuration
func ConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".devdesk"), nil
}

// EnsureConfigDir crée le répertoire de configuration s'il n'existe pas
func EnsureConfigDir() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}

	return os.MkdirAll(dir, 0755)
}

// Save sauvegarde la configuration dans le fichier du contexte actuel
func Save(cfg *Config) error {
	ctx, err := GetCurrentContext()
	if err != nil {
		// Fallback to default on error
		ctx = "default"
	}

	return SaveContext(cfg, ctx)
}

// Context Management Functions

// GetCurrentContext lit le nom du contexte actuel depuis .current-context
// Retourne "default" si le fichier n'existe pas ou est vide
func GetCurrentContext() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	contextFile := filepath.Join(homeDir, ".devdesk", ".current-context")

	// Si le fichier n'existe pas, retourner "default"
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

// SetCurrentContext écrit le nom du contexte dans .current-context
func SetCurrentContext(name string) error {
	if err := ValidateContextName(name); err != nil {
		return err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	contextFile := filepath.Join(homeDir, ".devdesk", ".current-context")

	// Assurer que le répertoire existe
	if err := EnsureConfigDir(); err != nil {
		return err
	}

	return os.WriteFile(contextFile, []byte(name), 0600)
}

// ListContexts découvre les contextes disponibles en listant les fichiers config-*.yaml
// Retourne une slice de noms de contextes (sans préfixe "config-" ni suffixe ".yaml")
// Inclut toujours "default" si config.yaml existe
func ListContexts() ([]string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	configDir := filepath.Join(homeDir, ".devdesk")
	contexts := []string{}

	// Vérifier le contexte default
	defaultPath := filepath.Join(configDir, "config.yaml")
	if _, err := os.Stat(defaultPath); err == nil {
		contexts = append(contexts, "default")
	}

	// Glob pour les contextes nommés
	pattern := filepath.Join(configDir, "config-*.yaml")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	for _, match := range matches {
		// Extraire le nom: config-{name}.yaml → {name}
		basename := filepath.Base(match)
		name := strings.TrimPrefix(basename, "config-")
		name = strings.TrimSuffix(name, ".yaml")
		contexts = append(contexts, name)
	}

	return contexts, nil
}

// LoadContext charge la configuration pour un contexte spécifique
// contextName = "default" → config.yaml
// contextName = "dev" → config-dev.yaml
func LoadContext(contextName string) (*Config, error) {
	if err := ValidateContextName(contextName); err != nil {
		return nil, err
	}

	configPath, err := GetContextPath(contextName)
	if err != nil {
		return nil, err
	}

	// Vérifier si le fichier existe
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("context '%s' does not exist (no file at %s)", contextName, configPath)
	}

	// Lire et parser YAML
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse context '%s': %w", contextName, err)
	}

	// Appliquer les valeurs par défaut
	if err := applyDefaults(&cfg); err != nil {
		return nil, fmt.Errorf("invalid context '%s': %w", contextName, err)
	}

	// Étendre les chemins (tildes)
	if home, err := os.UserHomeDir(); err == nil {
		cfg.ExpandPaths(home)
	}

	return &cfg, nil
}

// ExpandPaths étend les tildes (~) dans les chemins de configuration
func (c *Config) ExpandPaths(homeDir string) {
	expand := func(path string) string {
		if strings.HasPrefix(path, "~/") {
			return filepath.Join(homeDir, path[2:])
		}
		return path
	}

	c.App.LogFile = expand(c.App.LogFile)
	c.App.WorkspacesDir = expand(c.App.WorkspacesDir)
	c.GitLab.Pull.TargetDir = expand(c.GitLab.Pull.TargetDir)
	c.Registry.CacheDir = expand(c.Registry.CacheDir)
	c.Scan.CacheDir = expand(c.Scan.CacheDir)
	c.Scan.SBOMOutputDir = expand(c.Scan.SBOMOutputDir)
	c.Scan.TrivyPath = expand(c.Scan.TrivyPath)
	c.Scan.GitleaksPath = expand(c.Scan.GitleaksPath)
	c.Scan.GitleaksConfig = expand(c.Scan.GitleaksConfig)
}

// SaveContext sauvegarde la configuration dans un fichier de contexte spécifique
func SaveContext(cfg *Config, contextName string) error {
	if err := ValidateContextName(contextName); err != nil {
		return err
	}

	configPath, err := GetContextPath(contextName)
	if err != nil {
		return err
	}

	// Assurer que le répertoire existe
	if err := EnsureConfigDir(); err != nil {
		return err
	}

	// Marshaller en YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	// Écrire le fichier
	return os.WriteFile(configPath, data, 0600)
}

// ContextExists vérifie si un fichier de configuration de contexte existe
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

// GetContextPath retourne le chemin du fichier pour un contexte
// "default" → ~/.devdesk/config.yaml
// "dev" → ~/.devdesk/config-dev.yaml
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

// ValidateContextName valide le format du nom de contexte
// Doit être "default" ou correspondre à ^[a-z0-9-]+$
func ValidateContextName(name string) error {
	if name == "default" {
		return nil
	}

	// Pattern: lettres minuscules, chiffres, tirets
	matched, err := regexp.MatchString("^[a-z0-9-]+$", name)
	if err != nil {
		return err
	}

	if !matched {
		return fmt.Errorf("invalid context name '%s': must contain only lowercase letters, numbers, and hyphens", name)
	}

	return nil
}

// CreateContext crée un nouveau contexte avec une configuration par défaut
// Monitors vides, GitLab URL vide, autres valeurs par défaut
func CreateContext(contextName string) error {
	if err := ValidateContextName(contextName); err != nil {
		return err
	}

	// Vérifier si existe déjà
	exists, err := ContextExists(contextName)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("context '%s' already exists", contextName)
	}

	// Créer config avec defaults
	cfg := Default()

	// Vider monitors et GitLab pour nouveau contexte
	cfg.Status.Components = []ComponentConfig{}
	cfg.GitLab.URL = ""

	// Sauvegarder
	return SaveContext(cfg, contextName)
}
