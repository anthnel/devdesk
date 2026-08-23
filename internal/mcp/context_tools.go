package mcp

import (
	"context"

	"github.com/anthnel/devdesk/internal/config"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// contextListOut is what context_list answers.
//
// Served is not decoration: this process serves one context, so an agent that
// reads Contexts alone would reasonably assume it can ask about any of them.
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

// toolDescription reads a tool's description off the declared table, so the
// registration and the vocabulary cannot drift into saying two different things
// about the same tool.
func toolDescription(name string) string {
	for _, t := range tools() {
		if t.Name == name {
			return t.Description
		}
	}
	return ""
}
