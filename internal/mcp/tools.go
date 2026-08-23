package mcp

import (
	"fmt"
	"sort"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// toolDef declares one tool. The table below is the vocabulary, in the spirit of
// internal/ui/keymap and command.AllViewNames(): a name that is not here does
// not exist, and TestTheServerRegistersExactlyTheDeclaredTools holds the table
// and what the server actually serves in step.
//
// register is a closure rather than a handler field because sdk.AddTool is
// generic over the argument and result types — a homogeneous table cannot hold
// handlers that differ in both. The closure is where the typing happens, and it
// is what the round-trip test checks.
type toolDef struct {
	Name        string
	Description string
	register    func(*sdk.Server, *Env)
}

// tools is every tool DevDesk declares.
//
// The selection rule (§3.38): expose what DevDesk knows and an agent cannot get
// as cheaply itself. An agent can run `docker ps` and `trivy` on its own; it
// cannot know what the notion of a context means here, nor what has already been
// scanned.
func tools() []toolDef {
	return []toolDef{
		{
			Name:        "context_list",
			Description: "List the DevDesk configuration contexts on this machine, and say which one this server answers for.",
			register:    registerContextList,
		},
	}
}

// exposedTools applies `mcp.expose` as an allow-list.
//
// An allow-list rather than a deny-list, for the reason §3.38 states in the
// other direction: a tool that is never registered cannot fail to be excluded,
// where a deny-list is one forgotten line away from exposing whatever arrives
// next. Empty means every declared tool — the list is a narrowing, not an
// opt-in, or enabling the server at all would require naming ten tools.
//
// A name that matches nothing is refused rather than ignored. A typo in an
// allow-list silently exposes less than the user asked for, which is the failure
// nobody notices: everything works, quietly, with a tool missing.
func exposedTools(expose []string) ([]toolDef, error) {
	all := tools()
	if len(expose) == 0 {
		return all, nil
	}

	declared := make(map[string]toolDef, len(all))
	for _, t := range all {
		declared[t.Name] = t
	}

	var unknown []string
	seen := make(map[string]bool, len(expose))
	var out []toolDef
	for _, name := range expose {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		t, ok := declared[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		out = append(out, t)
	}

	if len(unknown) > 0 {
		return nil, fmt.Errorf("mcp.expose names %d tool(s) that do not exist: %s — the declared tools are: %s",
			len(unknown), strings.Join(unknown, ", "), strings.Join(toolNames(), ", "))
	}
	return out, nil
}

// toolNames returns every declared name, sorted, for error messages and tests.
func toolNames() []string {
	all := tools()
	names := make([]string, 0, len(all))
	for _, t := range all {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return names
}
