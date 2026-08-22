package config

// The forge vocabulary. It is stated here as well as in internal/forge, and
// TestTheForgeVocabularyMatchesTheBackends keeps the two in step — the same
// arrangement the registry `provider` names have between config and
// registrymgr (§3.8). A config value that no backend answers to would resolve
// to nothing at load, which is a failure mode worth a test rather than a
// comment.
const (
	// ForgeGitLab is the default, and what every file written before `type:`
	// existed means.
	ForgeGitLab = "gitlab"
	ForgeGitHub = "github"
)

// ForgeTypes lists what `forge.type` accepts, in the order the configuration
// view cycles them.
func ForgeTypes() []string { return []string{ForgeGitLab, ForgeGitHub} }

// migrateGitLabSection carries a legacy `gitlab:` block into `forge:`.
//
// Field by field, and only where the new value is empty: a file that already
// carries `forge:` keeps it, so a half-written file does not have its new
// values overwritten by its old ones. The legacy struct is cleared once read,
// so the key leaves the file on the next save — the precedent is
// RegistryItem.AuthEnabled and, right below this in applyDefaults,
// `docker.network_tool_image`.
//
// It runs **before** the defaults. yaml.Unmarshal is not strict here, so an
// un-migrated block is dropped in silence, and the silence would point a
// context at no host at all.
func migrateGitLabSection(cfg *Config) {
	legacy := cfg.GitLab
	if legacy == (GitLabConfig{}) {
		return
	}
	defer func() {
		// A legacy file predates `type:` by definition, so it can only mean
		// GitLab. Cleared last, so the key leaves the file on the next save.
		if cfg.Forge.Type == "" {
			cfg.Forge.Type = ForgeGitLab
		}
		cfg.GitLab = GitLabConfig{}
	}()

	// Nothing under `forge:` at all — the ordinary case, and the only one where
	// a bool can be carried over safely: IncludeArchived's zero value *is*
	// false, so no per-field guard could tell "unset" from "deliberately off".
	if cfg.Forge == (ForgeConfig{}) {
		cfg.Forge = ForgeConfig{
			URL:                legacy.URL,
			DefaultParentGroup: legacy.DefaultParentGroup,
			DefaultVisibility:  legacy.DefaultVisibility,
			CloneMethod:        legacy.CloneMethod,
			Pull: ForgePullConfig{
				ParallelJobs:    legacy.Pull.ParallelJobs,
				IncludeArchived: legacy.Pull.IncludeArchived,
			},
		}
		return
	}

	// Both blocks are present, which a hand-edited file can be. The new one
	// wins field by field: its values were written later, and overwriting them
	// with the old ones would undo the edit that created the situation.
	if cfg.Forge.URL == "" {
		cfg.Forge.URL = legacy.URL
	}
	if cfg.Forge.DefaultParentGroup == "" {
		cfg.Forge.DefaultParentGroup = legacy.DefaultParentGroup
	}
	if cfg.Forge.DefaultVisibility == "" {
		cfg.Forge.DefaultVisibility = legacy.DefaultVisibility
	}
	if cfg.Forge.CloneMethod == "" {
		cfg.Forge.CloneMethod = legacy.CloneMethod
	}
	if cfg.Forge.Pull.ParallelJobs == 0 {
		cfg.Forge.Pull.ParallelJobs = legacy.Pull.ParallelJobs
	}
}
