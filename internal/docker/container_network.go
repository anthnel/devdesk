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
			IPv4:       parseIPv4(e.IPAddress),
			Gateway:    parseIPv4(e.Gateway),
			GwPriority: e.GwPriority,
		})
	}
	return n, nil
}

// parseIPv4 returns the address when s is a valid IPv4 one, else the zero Addr.
func parseIPv4(s string) netip.Addr {
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is4() {
		return netip.Addr{}
	}
	return a
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
