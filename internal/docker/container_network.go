package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"

	"github.com/anthnel/devdesk/internal/engine"
)

// ContainerNetworking is the narrow, read-only view of a container's network
// attachment that the Path tab needs.
type ContainerNetworking struct {
	Name     string // engine's name without the leading "/"; untrusted text
	Running  bool
	Mode     string // HostConfig.NetworkMode
	Networks []EndpointNetwork
}

// EndpointNetwork is one network a container is attached to.
type EndpointNetwork struct {
	Name       string
	NetworkID  string // lowercase hex as the engine reports it, or "" when absent or not hex
	IPv4       netip.Addr
	Gateway    netip.Addr
	GwPriority *int
}

// ErrDockerOnly is returned for reads only Docker Engine answers.
var ErrDockerOnly = errors.New("only Docker Engine answers this read")

const containerNetworkingFormat = `{"Name":{{json .Name}},"Mode":{{json .HostConfig.NetworkMode}},"Running":{{json .State.Running}},"Networks":{{json .NetworkSettings.Networks}}}`

const maxInspectBytes = 256 << 10

// requireDockerEngine refuses reads only Docker Engine answers, before any
// invocation: Podman's inspect and info shapes differ.
func requireDockerEngine() error {
	if engine.Current().Name != engine.Docker {
		return ErrDockerOnly
	}
	return requireEngine()
}

// InspectContainerNetworking reads one container's network attachment with
// `container inspect --format`, which selects no Env, Labels or Mounts. It
// never starts the container and its error carries no command output.
func InspectContainerNetworking(ctx context.Context, id string) (ContainerNetworking, error) {
	if !IsContainerID(id) {
		return ContainerNetworking{}, fmt.Errorf("invalid container id %q", id)
	}
	if err := requireDockerEngine(); err != nil {
		return ContainerNetworking{}, err
	}
	out, err := runner.Run(dockerCmd{
		Args: []string{"container", "inspect", "--format", containerNetworkingFormat, id},
		Ctx:  ctx,
	})
	if err != nil {
		return ContainerNetworking{}, wrapErr(cmdLabel("container inspect"), err)
	}
	return parseContainerNetworking(out)
}

// parseContainerNetworking decodes the output of containerNetworkingFormat.
// Addresses that are not IPv4 become the zero Addr; networks are sorted by name.
func parseContainerNetworking(out []byte) (ContainerNetworking, error) {
	if len(out) > maxInspectBytes {
		return ContainerNetworking{}, fmt.Errorf("container inspect output exceeds %d bytes", maxInspectBytes)
	}
	var raw struct {
		Name     string
		Mode     string
		Running  bool
		Networks map[string]struct {
			NetworkID  string
			IPAddress  string
			Gateway    string
			GwPriority *int
		}
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return ContainerNetworking{}, fmt.Errorf("parse container inspect output: %w", err)
	}
	n := ContainerNetworking{
		Name:    strings.TrimPrefix(raw.Name, "/"),
		Running: raw.Running,
		Mode:    raw.Mode,
	}
	for _, name := range slices.Sorted(maps.Keys(raw.Networks)) {
		e := raw.Networks[name]
		n.Networks = append(n.Networks, EndpointNetwork{
			Name:       name,
			NetworkID:  hexOrEmpty(e.NetworkID),
			IPv4:       parseIPv4(e.IPAddress),
			Gateway:    parseIPv4(e.Gateway),
			GwPriority: e.GwPriority,
		})
	}
	return n, nil
}

// hexOrEmpty returns s when it is a full 64-hex engine ID, else "".
// InspectNetworkBridge accepts nothing shorter.
func hexOrEmpty(s string) string {
	if len(s) != maxIDLen || !IsContainerID(s) {
		return ""
	}
	return s
}

// parseIPv4 returns the address when s is a valid IPv4 one, else the zero Addr.
func parseIPv4(s string) netip.Addr {
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is4() {
		return netip.Addr{}
	}
	return a
}

// NetworkBridge is the host-side bridge of a Docker network.
type NetworkBridge struct {
	Driver string
	Name   string // host interface name; "" when it cannot be derived
}

// networkBridgeFormat selects only the driver, the bridge name option and the
// ID. A missing option prints an empty string ("").
const networkBridgeFormat = `{"Driver":{{json .Driver}},"Name":{{json (index .Options "com.docker.network.bridge.name")}},"Id":{{json .Id}}}`

// InspectNetworkBridge reads the host bridge of one network, by ID only: a
// name could be taken for an option by the CLI. Its error carries no command
// output.
func InspectNetworkBridge(ctx context.Context, networkID string) (NetworkBridge, error) {
	if err := requireDockerEngine(); err != nil {
		return NetworkBridge{}, err
	}
	if len(networkID) != maxIDLen || !IsContainerID(networkID) {
		return NetworkBridge{}, fmt.Errorf("invalid network id %q", networkID)
	}
	out, err := runner.Run(dockerCmd{
		Args: []string{"network", "inspect", "--format", networkBridgeFormat, networkID},
		Ctx:  ctx,
	})
	if err != nil {
		return NetworkBridge{}, wrapErr(cmdLabel("network inspect"), err)
	}
	return parseNetworkBridge(out)
}

// parseNetworkBridge decodes the output of networkBridgeFormat.
func parseNetworkBridge(out []byte) (NetworkBridge, error) {
	if len(out) > maxInspectBytes {
		return NetworkBridge{}, fmt.Errorf("network inspect output exceeds %d bytes", maxInspectBytes)
	}
	var raw struct {
		Driver string
		Name   string
		ID     string `json:"Id"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return NetworkBridge{}, fmt.Errorf("parse network inspect output: %w", err)
	}
	return NetworkBridge{Driver: raw.Driver, Name: bridgeNameFor(raw.Driver, raw.Name, raw.ID)}, nil
}

// bridgeNameFor returns the host interface of a bridge network: the explicit
// option, else the engine's "br-" + first 12 hex digits of a full 64-hex ID.
// Any other driver has no bridge of its own.
func bridgeNameFor(driver, option, id string) string {
	if driver != "bridge" {
		return ""
	}
	if option != "" {
		return option
	}
	if len(id) != maxIDLen || !IsContainerID(id) {
		return ""
	}
	return "br-" + id[:12]
}

// ListContainersContext is ListContainers with a context that kills the
// invocation when cancelled.
func ListContainersContext(ctx context.Context, all bool) ([]Container, error) {
	if err := requireEngine(); err != nil {
		return nil, err
	}

	args := []string{"ps"}
	if all {
		args = append(args, "--all")
	}
	args = append(args, "--format", templates().PS)

	output, err := runner.Run(dockerCmd{Args: args, Ctx: ctx})
	if err != nil {
		return nil, wrapErr(cmdLabel("ps"), err)
	}
	return parseContainers(output), nil
}

// EngineOperatingSystem returns the engine's operating system text as printed,
// trimmed but not sanitized: the caller treats it as untrusted.
func EngineOperatingSystem(ctx context.Context) (string, error) {
	if err := requireDockerEngine(); err != nil {
		return "", err
	}
	out, err := runner.Run(dockerCmd{Args: []string{"info", "--format", "{{.OperatingSystem}}"}, Ctx: ctx})
	if err != nil {
		return "", wrapErr(cmdLabel("info"), err)
	}
	return strings.TrimSpace(string(out)), nil
}
