package mcp

import (
	"context"
	"log"
	"net/url"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anthnel/devdesk/internal/template"
)

// ── templates_list ──────────────────────────────────────────────────────────

// Disk only: the catalog is ~/.devdesk/templates.yaml and the cache is a
// directory beside it. It is global rather than per context (template/store.go),
// so the answer carries no context and is declared machine-wide in
// TestEveryContextDependentAnswerSaysWhichContextServedIt.
//
// What is left out is the point. There is no file content — a template can hold
// anything, secrets included — and no template.Credentials, which is resolved
// from the secret store that no tool reads. A source URL can still carry a
// secret of its own (https://user:token@host/…), so it goes out with the userinfo
// removed.

type templateOut struct {
	Slug        string   `json:"slug" jsonschema:"the identifier of the template in the catalog"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Kind        string   `json:"kind" jsonschema:"git, local or oci"`
	URL         string   `json:"url,omitempty" jsonschema:"the clone URL of a git template or the registry of an OCI one, without any user or password"`
	Path        string   `json:"path,omitempty" jsonschema:"a subdirectory for git, the directory for local, the repository for oci"`
	Ref         string   `json:"ref,omitempty" jsonschema:"branch, tag or SHA"`
	FetchedAt   string   `json:"cached_at,omitempty" jsonschema:"RFC 3339, when the cached copy was read; absent when there is none for the current source"`
}

type templatesListIn struct{}

type templatesListOut struct {
	Templates []templateOut `json:"templates" jsonschema:"sorted by name"`
	// Unusable counts what the file holds and the catalog cannot use — a bad
	// slug, a refused URL scheme, a duplicate. Their reasons are left out: they
	// quote the entry as written, and this tool sends nothing of it that the
	// fields above do not.
	Unusable int `json:"unusable" jsonschema:"entries of the catalog file that were left out because they do not validate"`
}

func registerTemplatesList(s *sdk.Server, _ *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "templates_list",
		Description: toolDescription("templates_list"),
	}, func(_ context.Context, _ *sdk.CallToolRequest, _ templatesListIn) (*sdk.CallToolResult, templatesListOut, error) {
		return nil, listTemplates(), nil
	})
}

func listTemplates() templatesListOut {
	// No catalog file is an empty catalog, and so is a directory that cannot be
	// found; an unreadable or malformed file is a failure the caller should see,
	// but this handler has no error to return that is not the caller's — so it is
	// logged, and the answer is honest about being empty rather than claiming a
	// catalog it could not read.
	out := templatesListOut{Templates: []templateOut{}}

	path, err := template.DefaultPath()
	if err != nil {
		log.Printf("ERROR [mcp/templates] catalog path: %v", err)
		return out
	}
	store, err := template.Open(path)
	if err != nil {
		log.Printf("ERROR [mcp/templates] read catalog: %v", err)
		return out
	}

	entries := store.List()
	cached := template.NewCache().FetchedAtAll(entries)
	for _, e := range entries {
		out.Templates = append(out.Templates, templateOut{
			Slug:        e.Slug,
			Name:        e.Name,
			Description: e.Description,
			Tags:        e.Tags,
			Kind:        string(e.Source.Kind),
			URL:         withoutUserinfo(e.Source.URL),
			Path:        e.Source.Path,
			Ref:         e.Source.Ref,
			FetchedAt:   stamp(cached[e.Slug]),
		})
	}
	out.Unusable = len(store.Problems())
	return out
}

// withoutUserinfo drops the user and password of a URL. Entries reach here
// validated (template.validateGitURL): a value with no "://" is the scp form,
// user@host:path, whose user part cannot hold a colon and so no password, and it
// is returned as it is. What has a scheme but does not parse is returned empty
// rather than guessed at — a string that failed to parse cannot be shown to hold
// no credential.
func withoutUserinfo(raw string) string {
	if !strings.Contains(raw, "://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User = nil
	return u.String()
}
