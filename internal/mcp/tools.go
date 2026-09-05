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
		{
			Name:        "context_get",
			Description: "Read the configuration of the context this server answers for: where its repositories live, which forge it targets, the options its scans run with, its registries and its monitors. It carries no credential of any kind, and registries_list is what answers for its registries.",
			register:    registerContextGet,
		},
		{
			Name:        "workspaces_list",
			Description: "List the git repositories checked out under this context's workspaces directory, with their branch, their uncommitted and unpushed work, and what the scan cache knows about each. The behind count is only as fresh as the last fetch.",
			register:    registerWorkspacesList,
		},
		{
			Name:        "registries_list",
			Description: "List the OCI registries this context is configured with — their address, kind, provider and authentication mode — together with the members that discovery last found for a repository-manager group. It reads the cache and never the network, so a group nobody has probed is reported as unprobed rather than probed here.",
			register:    registerRegistriesList,
		},
		{
			Name:        "containers_list",
			Description: "List the Docker containers on this machine, with their state and their published ports already parsed — the scope of each publication says whether it is reachable from the network or from this machine only. It carries no CPU or memory figures, which need a second call that blocks.",
			register:    registerContainersList,
		},
		{
			Name:        "images_list",
			Description: "List the Docker images on this machine, with their size, how many containers use each, and whether DevDesk has ever scanned it. The findings themselves come from scan_result.",
			register:    registerImagesList,
		},
		{
			Name:        "ports_list",
			Description: "List the TCP and UDP sockets open on this machine, with the process holding each one. It reads the socket table in this process — no container, no network call — and the addresses are never resolved to host names.",
			register:    registerPortsList,
		},
		{
			Name:        "net_check",
			Description: "Run DevDesk's connectivity pipeline against a host and port: name resolution, ICMP reachability, the TCP connect, the certificate — handshake, chain, hostname, expiry, version — and an HTTP response. Each check answers with a verdict, what was observed, why it matters and what to do about it. This is the one tool here that touches the network, and it can be pointed at any host.",
			register:    registerNetCheck,
		},
		{
			Name:        "scan_inventory",
			Description: "List every image and repository this DevDesk context has scanned, with its severity counts, its secret verdict and how long ago the scan ran. Targets whose image or directory no longer exists are left out.",
			register:    registerScanInventory,
		},
		{
			Name:        "jobs_list",
			Description: "List the work this DevDesk session has started — scans, clones, syncs, pulls — with what each is doing right now. This is the one thing the disk cannot answer: a finished scan is in the cache, a running one exists only in the session. Runs do not survive a restart.",
			register:    registerJobsList,
		},
		{
			Name:        "jobs_get",
			Description: "Read one run of work in full, target by target, with the state of each and why any of them failed. It says which DevDesk context the run was launched in, which is not necessarily the one on screen now.",
			register:    registerJobsGet,
		},
		{
			Name:        "workspace_scan_start",
			Description: "Scan repositories for vulnerabilities, secrets, licences and misconfiguration, with this context's scan settings. It returns a job id at once and the scan runs on: jobs_get follows it, scan_result reads the findings once it has finished. It never purges what is already cached; nothing is deleted.",
			register:    registerWorkspaceScanStart,
		},
		{
			Name:        "workspace_sync_start",
			Description: "Fast-forward repositories that are behind their remote. It returns a job id at once. A repository with local changes that would be overwritten is left alone rather than forced, and nothing is ever reset or discarded.",
			register:    registerWorkspaceSyncStart,
		},
		{
			Name:        "image_scan_start",
			Description: "Scan container images for vulnerabilities, secrets, licences and misconfiguration, with this context's scan settings. It returns a job id at once; scan_result reads the findings once it has finished. It never purges what is already cached.",
			register:    registerImageScanStart,
		},
		{
			Name:        "image_pull_start",
			Description: "Pull an image onto this machine. It returns a job id at once and the download runs on, resumable by layer, so jobs_cancel can stop it. It adds an image and removes none.",
			register:    registerImagePullStart,
		},
		{
			Name:        "jobs_cancel",
			Description: "Stop a run. The queue always stops, so nothing further starts; work already in flight is cut only where cutting it leaves nothing behind — a scan and a pull stop cleanly, while a clone in progress is waited out because a cut one leaves half a repository on disk. A run that has already settled is refused rather than reported as stopped.",
			register:    registerJobsCancel,
		},
		{
			Name:        "scan_result",
			Description: "Read the findings of one stored scan, filtered by severity and category and returned one page at a time. The matched string of a secret finding is never included.",
			register:    registerScanResult,
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

// toolDescription reads a tool's description off the declared table, so the
// registration and the vocabulary cannot drift into saying two different things
// about one tool.
func toolDescription(name string) string {
	for _, t := range tools() {
		if t.Name == name {
			return t.Description
		}
	}
	return ""
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
