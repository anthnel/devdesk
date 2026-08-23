package mcp

import (
	"context"

	"github.com/anthnel/devdesk/internal/config"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// contextListOut is what context_list answers.
//
// Served is not decoration: this process serves one context, so an agent that
// read Contexts alone would reasonably assume it can ask about any of them.
// Saying which one is readable puts the scope decision in the data rather than
// only in the documentation.
type contextListOut struct {
	Contexts []string `json:"contexts" jsonschema:"every DevDesk configuration context on this machine"`
	Served   string   `json:"served" jsonschema:"the one context this server answers for; the others are listed but not readable through it"`
	Current  string   `json:"current" jsonschema:"the context the DevDesk TUI is currently set to, which may differ from the served one"`
}

func registerContextList(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "context_list",
		Description: toolDescription("context_list"),
	}, func(_ context.Context, _ *sdk.CallToolRequest, _ any) (*sdk.CallToolResult, contextListOut, error) {
		out := contextListOut{
			Contexts: []string{},
			Served:   env.Context,
			Current:  config.CurrentContextName(),
		}
		// A listing that cannot be read is not an empty list of contexts — the
		// same rule the forge listings follow. The served context is known
		// whatever happens, so it is reported rather than an error returned for
		// a question that is mostly answered.
		if names, err := config.ListContexts(); err == nil {
			out.Contexts = names
		} else {
			out.Contexts = []string{env.Context}
		}
		return nil, out, nil
	})
}

// ── context_get ─────────────────────────────────────────────────────────────

// contextGetOut is the served context's configuration, and **no field here can
// carry a secret**.
//
// That is not a filter applied on the way out, it is §3.9's guarantee arriving
// for free: ForgeConfig has no Token and RegistryItem has no Password, because
// no secret DevDesk holds is written to a file DevDesk owns. A projection of
// that file therefore has nothing to redact. The registry entries are the case
// worth noticing, since a registry is exactly the kind of thing that used to
// carry a password in its config block.
//
// The registries are **not** here, and that is a change from what §3.38 sketched.
// registries_list answers for them, with what discovery found alongside the
// declaration; carrying a second, thinner copy here would be two answers to one
// question, which is the shape D12, D24 and D25 each turned out to be. An agent
// with two tools for one fact also has to guess which is authoritative.
type contextGetOut struct {
	Name          string         `json:"name"`
	IsCurrent     bool           `json:"is_current" jsonschema:"whether the DevDesk TUI is currently set to this context"`
	WorkspacesDir string         `json:"workspaces_dir" jsonschema:"where this context's repositories live; workspaces_list reads under it"`
	Forge         forgeOut       `json:"forge"`
	Scan          scanOptionsOut `json:"scan" jsonschema:"the options a scan launched from this context runs with; a shared image entry may have been produced under another context's"`
	Monitors      []monitorOut   `json:"monitors" jsonschema:"the endpoints the status view watches; registries_list answers for the registries"`
}

type forgeOut struct {
	Type               string `json:"type" jsonschema:"gitlab or github"`
	URL                string `json:"url" jsonschema:"the web host; on an Enterprise install the API lives under a different path"`
	DefaultParentGroup string `json:"default_parent_group,omitempty"`
	DefaultVisibility  string `json:"default_visibility,omitempty"`
	IncludeArchived    bool   `json:"include_archived" jsonschema:"whether a clone walks archived repositories"`
}

type scanOptionsOut struct {
	Vulnerabilities bool `json:"vulnerabilities"`
	// SecretScanning is the odd name of the four, and deliberately: `secrets`
	// under a `scan` block reads as "this context has secrets", which is a
	// different claim from "its scans look for them".
	SecretScanning  bool `json:"secret_scanning"`
	Licenses        bool `json:"licenses"`
	Misconfig       bool `json:"misconfigurations"`
	IgnoreUnfixed   bool `json:"ignore_unfixed"`
	GitleaksHistory bool `json:"gitleaks_history" jsonschema:"whether the secret scan reads git history as well as the working tree"`
}

type monitorOut struct {
	Name   string `json:"name"`
	Type   string `json:"type" jsonschema:"http, https, icmp or dns"`
	Target string `json:"target"`
}

func registerContextGet(s *sdk.Server, env *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "context_get",
		Description: toolDescription("context_get"),
	}, func(_ context.Context, _ *sdk.CallToolRequest, _ any) (*sdk.CallToolResult, contextGetOut, error) {
		return nil, describeContext(env), nil
	})
}

// describeContext projects the loaded configuration. It names every field it
// copies, so a setting added to config.Config arrives here as an omission rather
// than as an exposure — the same argument the tool allow-list and the finding
// projection each make.
//
// It takes no context name: this process serves one, and the configuration it
// answers with was loaded once at startup. Reading another context's file here
// would undo the scope decision in a single function.
func describeContext(env *Env) contextGetOut {
	cfg := env.Config

	out := contextGetOut{
		Name:          env.Context,
		IsCurrent:     config.CurrentContextName() == env.Context,
		WorkspacesDir: cfg.App.WorkspacesDir,
		Forge: forgeOut{
			Type:               cfg.Forge.Type,
			URL:                cfg.Forge.URL,
			DefaultParentGroup: cfg.Forge.DefaultParentGroup,
			DefaultVisibility:  cfg.Forge.DefaultVisibility,
			IncludeArchived:    cfg.Forge.Pull.IncludeArchived,
		},
		Scan: scanOptionsOut{
			Vulnerabilities: cfg.Scan.EnableVuln,
			SecretScanning:  cfg.Scan.EnableSecret,
			Licenses:        cfg.Scan.EnableLicense,
			Misconfig:       cfg.Scan.EnableMisconfig,
			IgnoreUnfixed:   cfg.Scan.IgnoreUnfixed,
			GitleaksHistory: cfg.Scan.GitleaksHistory,
		},
		Monitors: []monitorOut{},
	}

	for _, component := range cfg.Status.Components {
		out.Monitors = append(out.Monitors, monitorOut{
			Name:   component.Name,
			Type:   component.Type,
			Target: component.Target,
		})
	}

	return out
}
