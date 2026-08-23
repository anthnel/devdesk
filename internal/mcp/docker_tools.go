package mcp

import (
	"context"
	"log"
	"sort"
	"strings"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/docker"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// What the daemon holds. These are the tools whose value is not the data — an
// agent can run `docker ps` itself — but the shape: parsed ports rather than the
// string docker printed, and, for an image, whether DevDesk has ever looked at
// it.
//
// # ports_list is not here, and that is a decision
//
// §3.38 named it beside these two. It is not built, for the reason
// registry_tags is not: what the entry asks for cannot be had under the rules it
// sets in the same breath.
//
// The host's listening sockets are read by `docker.RunSS`, which is
// `docker run --rm --net=host --pid=host --privileged` — the same call as
// `KillProcess`, differing only in the command handed to an equally privileged
// container. Nothing persistent changes on the host, so it is not a write in the
// sense §3.9 means; but the promise of this server is that it does not act on
// the machine, and starting a privileged container is acting on it. An image
// that is not already local would be pulled, which is a network call and a disk
// write from a server that promised neither, and an agent can call a tool in a
// loop.
//
// The sockets remain readable in `:net`, where a person is present.

// ── containers_list ─────────────────────────────────────────────────────────

// portBinding is one publication, after the two rows docker prints for a
// dual-stack one have been merged back into the single publication they are.
//
// Scope is a word rather than the host address, for the reason the containers
// view gives: `0.0.0.0` and `127.0.0.1` are eleven and nine characters carrying
// one bit each — reachable from the network, or from this machine only — and
// that bit is the security-relevant one, drowned in the noise of spelling it
// out. The address is kept for the one scope where it is not a constant.
type portBinding struct {
	Scope         string `json:"scope" jsonschema:"all for every interface, loopback for this machine only, address for one named address, exposed for a port the image declares and nothing published"`
	HostAddress   string `json:"host_address,omitempty" jsonschema:"only when the scope is address; the other scopes are constants the word already names"`
	HostPort      string `json:"host_port,omitempty" jsonschema:"empty when nothing is published; may be a range exactly as docker printed it"`
	ContainerPort string `json:"container_port"`
	Protocol      string `json:"protocol"`
}

type containerOut struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Image   string        `json:"image"`
	State   string        `json:"state" jsonschema:"running, exited, paused, created, restarting or dead"`
	Status  string        `json:"status" jsonschema:"what docker prints, such as Up 2 hours or Exited (0) 5 minutes ago"`
	Created string        `json:"created"`
	Ports   []portBinding `json:"ports"`
}

type containersListIn struct {
	All bool `json:"all,omitempty" jsonschema:"include stopped containers; false lists only what is running"`
}

type containersListOut struct {
	Containers []containerOut `json:"containers"`
}

func registerContainersList(s *sdk.Server, _ *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "containers_list",
		Description: toolDescription("containers_list"),
	}, func(_ context.Context, _ *sdk.CallToolRequest, in containersListIn) (*sdk.CallToolResult, containersListOut, error) {
		list, err := listContainers(in.All)
		if err != nil {
			return nil, containersListOut{}, err
		}
		return nil, containersListOut{Containers: list}, nil
	})
}

// listContainers reports what the daemon holds.
//
// It carries no live metrics. CPU and memory come from `docker stats`, a second
// call that blocks for a second or more, and an agent asking what is running does
// not need them — a tool that always paid for them would make the common question
// the slow one.
func listContainers(all bool) ([]containerOut, error) {
	list, err := listDockerContainers(all)
	if err != nil {
		// The daemon being down is the caller's answer, not an empty list: an
		// absence read as an emptiness is what §3.37 exists to avoid, and here
		// there is no cache to fall back on.
		return nil, err
	}

	out := make([]containerOut, 0, len(list))
	for _, c := range list {
		entry := containerOut{
			ID: c.ID, Name: c.Name, Image: c.Image,
			State: c.State, Status: c.Status, Created: c.CreatedAt,
			Ports: make([]portBinding, 0, len(c.Ports)),
		}
		for _, p := range c.Ports {
			entry.Ports = append(entry.Ports, portBinding{
				Scope:         scopeName(p.Scope),
				HostAddress:   addressOf(p),
				HostPort:      p.HostPort,
				ContainerPort: p.ContainerPort,
				Protocol:      p.Protocol,
			})
		}
		out = append(out, entry)
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// scopeName is docker.PortScope as a word. The type is an int, so a conversion
// would yield a rune — the same trap scan.Category set — and the four values are
// a vocabulary an agent reads, which makes them part of the contract.
func scopeName(scope docker.PortScope) string {
	switch scope {
	case docker.ScopeAll:
		return "all"
	case docker.ScopeLoopback:
		return "loopback"
	case docker.ScopeAddress:
		return "address"
	default:
		return "exposed"
	}
}

// addressOf returns the host address only for the one scope whose address is
// not a constant. Repeating `0.0.0.0` beside the word "all" says it twice.
func addressOf(p docker.PortBinding) string {
	if p.Scope == docker.ScopeAddress {
		return p.HostIP
	}
	return ""
}

// ── images_list ─────────────────────────────────────────────────────────────

type imageOut struct {
	ID         string `json:"id"`
	Repository string `json:"repository"`
	Tag        string `json:"tag,omitempty"`
	Reference  string `json:"reference" jsonschema:"repository and tag joined; this is what scan_result takes as its target for an image"`
	SizeBytes  int64  `json:"size_bytes"`
	Containers int    `json:"containers" jsonschema:"how many containers use this image"`
	Created    string `json:"created"`

	// Scanned says whether this context's scan cache has an entry for the
	// image, and deliberately carries no counts: scan_inventory and scan_result
	// own those, and two tools reporting the same numbers is the shape D12, D24
	// and D25 each turned out to be. The flag adds what neither of them can
	// answer — which images have never been looked at.
	Scanned bool `json:"scanned" jsonschema:"whether a scan result is stored for this image; scan_result carries the findings"`
}

type imagesListOut struct {
	Images []imageOut `json:"images"`
}

func registerImagesList(s *sdk.Server, _ *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "images_list",
		Description: toolDescription("images_list"),
	}, func(_ context.Context, _ *sdk.CallToolRequest, _ any) (*sdk.CallToolResult, imagesListOut, error) {
		list, err := listLocalImages()
		if err != nil {
			return nil, imagesListOut{}, err
		}
		return nil, imagesListOut{Images: list}, nil
	})
}

func listLocalImages() ([]imageOut, error) {
	list, err := listImages()
	if err != nil {
		return nil, err
	}

	scanned, err := cache.ReadImageScanEntries()
	if err != nil {
		log.Printf("ERROR [mcp/docker] read image scan cache: %v", err)
		scanned = nil
	}

	out := make([]imageOut, 0, len(list))
	for _, img := range list {
		name := img.Name()
		_, isScanned := scanned[name]
		out = append(out, imageOut{
			ID:         img.ID,
			Repository: img.Repository,
			Tag:        img.Tag,
			Reference:  name,
			SizeBytes:  img.Size,
			Containers: img.Containers,
			Created:    img.CreatedAt,
			Scanned:    isScanned,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Reference) < strings.ToLower(out[j].Reference)
	})
	return out, nil
}

// listDockerContainers is docker.ListContainers, indirected for the tests for
// the reason listImages is: a test cannot start a container, and without the
// seam the projection could only be asserted against whatever the developer's
// daemon happens to hold.
var listDockerContainers = docker.ListContainers
