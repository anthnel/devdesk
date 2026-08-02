package docker

import "strings"

// Volume represents a Docker volume
type Volume struct {
	Name       string
	Driver     string
	Mountpoint string
	Created    string
}

// ListVolumes returns a list of Docker volumes
func ListVolumes() ([]Volume, error) {
	if err := requireDocker(); err != nil {
		return nil, err
	}
	output, err := dockerOutput("volume", "ls", "--format", "{{.Name}}\t{{.Driver}}\t{{.Mountpoint}}")
	if err != nil {
		return nil, wrapErr("docker volume ls", err)
	}

	var volumes []Volume
	for _, line := range splitLines(output) {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		v := Volume{Name: parts[0], Driver: parts[1]}
		if len(parts) > 2 {
			v.Mountpoint = parts[2]
		}
		volumes = append(volumes, v)
	}
	return volumes, nil
}

// CreateVolume creates a Docker volume with the given name and optional driver
func CreateVolume(name, driver string) error {
	args := []string{"volume", "create"}
	if driver != "" {
		args = append(args, "--driver", driver)
	}
	args = append(args, name)
	return mutate("docker volume create", args...)
}

// RemoveVolume removes a Docker volume by name
func RemoveVolume(name string) error {
	return mutate("docker volume rm", "volume", "rm", name)
}

// PruneVolumes removes all unused Docker volumes
func PruneVolumes() (string, error) {
	return prune("docker volume prune", "volume", "prune", "-f")
}
