package docker

// This file covers Docker network resources — the `docker network` subcommand
// family. Not to be confused with ports.go, which reports the host's own
// listening sockets by running `ss` inside a --net=host container.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Network represents a Docker network
type Network struct {
	ID      string
	Name    string
	Driver  string
	Scope   string
	Created string
}

// ListNetworks returns a list of Docker networks
func ListNetworks() ([]Network, error) {
	if err := requireDocker(); err != nil {
		return nil, err
	}
	output, err := dockerOutput("network", "ls", "--format",
		"{{.ID}}\t{{.Name}}\t{{.Driver}}\t{{.Scope}}\t{{.CreatedAt}}")
	if err != nil {
		return nil, wrapErr("docker network ls", err)
	}

	var networks []Network
	for _, line := range splitLines(output) {
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 4 {
			continue
		}
		n := Network{ID: parts[0], Name: parts[1], Driver: parts[2], Scope: parts[3]}
		if len(parts) > 4 {
			n.Created = parts[4]
		}
		networks = append(networks, n)
	}
	return networks, nil
}

// CreateNetwork creates a Docker network with the given name and driver
func CreateNetwork(name, driver string) error {
	if driver == "" {
		driver = "bridge"
	}
	return mutate("docker network create", "network", "create", "--driver", driver, name)
}

// RemoveNetwork removes a Docker network by ID or name
func RemoveNetwork(id string) error {
	return mutate("docker network rm", "network", "rm", id)
}

// PruneNetworks removes all unused Docker networks
func PruneNetworks() (string, error) {
	return prune("docker network prune", "network", "prune", "-f")
}

// NetworkContainer represents a container connected to a Docker network
type NetworkContainer struct {
	Name       string
	IPv4       string
	MacAddress string
}

// networkInspectContainer is used for JSON parsing of docker network inspect output
type networkInspectContainer struct {
	Name        string `json:"Name"`
	MacAddress  string `json:"MacAddress"`
	IPv4Address string `json:"IPv4Address"`
}

// networkInspectResult is the top-level JSON structure from docker network inspect
type networkInspectResult struct {
	Containers map[string]networkInspectContainer `json:"Containers"`
}

// InspectNetwork returns the containers connected to a Docker network by ID or name.
func InspectNetwork(id string) ([]NetworkContainer, error) {
	if err := requireDocker(); err != nil {
		return nil, err
	}
	output, err := dockerOutput("network", "inspect", id)
	if err != nil {
		return nil, wrapErr("docker network inspect", err)
	}
	return parseNetworkInspect(output)
}

// parseNetworkInspect turns `docker network inspect` JSON into the connected
// container list, sorted by name so the table does not reshuffle between polls.
func parseNetworkInspect(output []byte) ([]NetworkContainer, error) {
	var results []networkInspectResult
	if err := json.Unmarshal(output, &results); err != nil {
		return nil, fmt.Errorf("failed to parse network inspect output: %w", err)
	}
	if len(results) == 0 {
		return nil, nil
	}

	var containers []NetworkContainer
	for _, c := range results[0].Containers {
		// IPv4Address carries a CIDR suffix ("172.17.0.2/16"); drop the mask.
		ip := c.IPv4Address
		if idx := strings.Index(ip, "/"); idx > 0 {
			ip = ip[:idx]
		}
		containers = append(containers, NetworkContainer{
			Name:       c.Name,
			IPv4:       ip,
			MacAddress: c.MacAddress,
		})
	}
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].Name < containers[j].Name
	})
	return containers, nil
}
