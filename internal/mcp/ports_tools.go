package mcp

import (
	"context"

	"github.com/anthnel/devdesk/internal/ports"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ── ports_list ──────────────────────────────────────────────────────────────

// §3.38 named this tool and then refused it, for a reason that no longer holds:
// the sockets were read by `docker.RunSS`, which was
// `docker run --rm --net=host --pid=host --privileged`, and starting a
// privileged container is acting on the machine — which is the one thing this
// server promises not to do. D55 established that the same call was also reading
// the wrong machine.
//
// `internal/ports` reads the socket table in this process. There is no container
// to start, nothing is signalled, and the read costs two system calls — so the
// argument that kept the tool out is gone with the container.
//
// **No reverse DNS here.** The Ports tab offers it behind `n`; this tool never
// asks. `net_check` is the one tool that touches the network, deliberately and
// with the target named by the caller, and a listing that quietly resolved every
// peer it found would put a query on the wire for each of them.

type socketOut struct {
	Protocol  string `json:"protocol" jsonschema:"tcp or udp"`
	State     string `json:"state" jsonschema:"LISTEN, ESTAB, UNCONN, TIME_WAIT and the other socket states; UNCONN is an unconnected datagram socket"`
	LocalAddr string `json:"local_address"`
	PeerAddr  string `json:"peer_address" jsonschema:"the wildcard form when the socket has no peer"`
	PID       string `json:"pid,omitempty" jsonschema:"empty when the system attributed the socket to no process"`
	Process   string `json:"process,omitempty" jsonschema:"empty when the process could not be named"`
}

type portsListIn struct {
	ListeningOnly bool `json:"listening_only,omitempty" jsonschema:"return only sockets in the LISTEN state, which is what the machine offers"`
}

type portsListOut struct {
	Sockets []socketOut `json:"sockets"`
}

func registerPortsList(s *sdk.Server, _ *Env) {
	sdk.AddTool(s, &sdk.Tool{
		Name:        "ports_list",
		Description: toolDescription("ports_list"),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in portsListIn) (*sdk.CallToolResult, portsListOut, error) {
		// Addresses stay numeric: see the note above.
		sockets, err := ports.List(ctx, false)
		if err != nil {
			// The table being unreadable is the caller's answer, not an empty
			// list — an absence read as an emptiness is D20.
			return nil, portsListOut{}, err
		}
		out := make([]socketOut, 0, len(sockets))
		for _, s := range sockets {
			if in.ListeningOnly && s.State != "LISTEN" {
				continue
			}
			out = append(out, socketOut{
				Protocol:  s.Protocol,
				State:     s.State,
				LocalAddr: s.LocalAddr,
				PeerAddr:  s.PeerAddr,
				PID:       s.PID,
				Process:   s.Process,
			})
		}
		return nil, portsListOut{Sockets: out}, nil
	})
}
