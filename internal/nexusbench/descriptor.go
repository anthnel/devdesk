package nexusbench

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

const (
	// descriptorSchemaVersion is the only schema LoadDescriptor accepts.
	descriptorSchemaVersion = 1
	descriptorFileName      = "descriptor.json"

	// Container ports of the Nexus pod; the manifests carry the same two.
	restContainerPort   = 8081
	dockerContainerPort = 8082

	dockerGroupRepository = "docker-group"

	loopbackHost = "127.0.0.1"

	descriptorFileMode = 0o644
	descriptorDirMode  = 0o755
)

// ErrNoDescriptor is what LoadDescriptor returns, wrapped, when the file does
// not exist: the bench has not been described yet.
var ErrNoDescriptor = errors.New("no bench descriptor")

// DeployResult is what the deployment step learned about the running bench.
type DeployResult struct {
	// ReachableHosts are the hosts on which the NodePorts answered.
	ReachableHosts []string
	// NodeIP is the InternalIP of the node, empty when it has none.
	NodeIP string
}

// ProvisionResult is what the provisioning step read from the instance.
type ProvisionResult struct {
	NexusVersion string
	Edition      string
	EULAAccepted bool
}

// Descriptor describes the bench to whoever measures against it. It holds no
// secret, no timestamp and no pod name, so two runs with the same parameters
// write the same bytes. The field order is the order of the JSON file.
type Descriptor struct {
	SchemaVersion    int                    `json:"schemaVersion"`
	KubeContext      string                 `json:"kubeContext"`
	Namespace        string                 `json:"namespace"`
	Image            string                 `json:"image"`
	NexusVersion     string                 `json:"nexusVersion"`
	Edition          string                 `json:"edition"`
	EULAAccepted     bool                   `json:"eulaAccepted"`
	Hosts            []string               `json:"hosts"`
	REST             Endpoint               `json:"rest"`
	DockerConnectors []DockerConnector      `json:"dockerConnectors"`
	Repositories     []DescriptorRepository `json:"repositories"`
	Realms           []string               `json:"realms"`
	Accounts         []DescriptorAccount    `json:"accounts"`
}

// Endpoint is a port of the pod published as a NodePort, with the URLs it
// answers on, one per host.
type Endpoint struct {
	NodePort      int      `json:"nodePort"`
	ContainerPort int      `json:"containerPort"`
	URLs          []string `json:"urls"`
}

// DockerConnector is a Docker connector of a repository.
type DockerConnector struct {
	Repository    string   `json:"repository"`
	NodePort      int      `json:"nodePort"`
	ContainerPort int      `json:"containerPort"`
	URLs          []string `json:"urls"`
}

// DescriptorRepository is a repository of the bench: hosted, proxy or group.
type DescriptorRepository struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	RemoteURL string   `json:"remoteUrl,omitempty"`
	Members   []string `json:"members,omitempty"`
}

// DescriptorAccount is an account of the bench and what it may do. Enabled is a
// pointer so that "disabled" is written, while an account that has no such
// switch writes nothing.
type DescriptorAccount struct {
	UserID     string   `json:"userId"`
	Enabled    *bool    `json:"enabled,omitempty"`
	Roles      []string `json:"roles"`
	Privileges []string `json:"privileges,omitempty"`
	Purpose    string   `json:"purpose"`
}

// BuildDescriptor returns the descriptor of the bench that p, the deployment
// and the provisioning describe. Hosts are p.Hosts when set, else the loopback
// address followed by the node's IP when there is one; the slices of p are
// copied, not shared.
func BuildDescriptor(p Params, d DeployResult, r ProvisionResult) Descriptor {
	hosts := append([]string(nil), p.Hosts...)
	if len(hosts) == 0 {
		hosts = []string{loopbackHost}
		if d.NodeIP != "" {
			hosts = append(hosts, d.NodeIP)
		}
	}
	return Descriptor{
		SchemaVersion: descriptorSchemaVersion,
		KubeContext:   p.Context,
		Namespace:     p.Namespace,
		Image:         p.ImageRef(),
		NexusVersion:  r.NexusVersion,
		Edition:       r.Edition,
		EULAAccepted:  r.EULAAccepted,
		Hosts:         hosts,
		REST: Endpoint{
			NodePort: p.HTTPNodePort, ContainerPort: restContainerPort,
			URLs: hostURLs(hosts, p.HTTPNodePort),
		},
		DockerConnectors: []DockerConnector{{
			Repository: dockerGroupRepository, NodePort: p.DockerNodePort, ContainerPort: dockerContainerPort,
			URLs: hostURLs(hosts, p.DockerNodePort),
		}},
		Repositories: []DescriptorRepository{
			{Name: "docker-hosted", Type: "hosted"},
			{Name: "docker-proxy", Type: "proxy", RemoteURL: p.UpstreamURL},
			{Name: dockerGroupRepository, Type: "group", Members: []string{"docker-hosted", "docker-proxy"}},
		},
		Realms: []string{"NexusAuthenticatingRealm", "DockerToken"},
		Accounts: []DescriptorAccount{
			{UserID: "admin", Roles: []string{"nx-admin"}, Purpose: "bench administrator"},
			{
				UserID: "bench-pull", Roles: []string{"bench-docker-pull"},
				Privileges: []string{"nx-repository-view-docker-*-browse", "nx-repository-view-docker-*-read"},
				Purpose:    "read-only pull",
			},
			{UserID: "anonymous", Enabled: &p.Anonymous, Roles: []string{"nx-anonymous"}, Purpose: "anonymous access"},
		},
	}
}

// hostURLs returns the http URL of a port on each host; an IPv6 address gets
// its brackets.
func hostURLs(hosts []string, port int) []string {
	urls := make([]string, 0, len(hosts))
	for _, h := range hosts {
		urls = append(urls, "http://"+net.JoinHostPort(h, strconv.Itoa(port)))
	}
	return urls
}

// WriteDescriptor writes d to path as JSON indented by two spaces with a final
// newline, atomically. The bytes depend on d alone.
func WriteDescriptor(path string, d Descriptor) error {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the descriptor: %w", err)
	}
	return writeFileAtomic(path, append(data, '\n'), descriptorFileMode, descriptorDirMode)
}

// LoadDescriptor reads the descriptor at path. A missing file is ErrNoDescriptor
// (wrapped); a file that is not JSON, or whose schemaVersion is not 1, is an
// error that does not show the content of the file.
func LoadDescriptor(path string) (Descriptor, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Descriptor{}, fmt.Errorf("%w: %s does not exist", ErrNoDescriptor, path)
	}
	if err != nil {
		return Descriptor{}, fmt.Errorf("read the descriptor: %w", err)
	}
	// The version is read first: a later schema may have fields this one cannot
	// decode, and the useful message is then the version.
	var probe struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return Descriptor{}, fmt.Errorf("the descriptor %s is not valid JSON", path)
	}
	if probe.SchemaVersion != descriptorSchemaVersion {
		return Descriptor{}, fmt.Errorf("the descriptor %s has schemaVersion %d; this build reads schemaVersion %d only", path, probe.SchemaVersion, descriptorSchemaVersion)
	}
	var d Descriptor
	if err := json.Unmarshal(data, &d); err != nil {
		return Descriptor{}, fmt.Errorf("the descriptor %s does not match schemaVersion %d", path, descriptorSchemaVersion)
	}
	return d, nil
}

// DescriptorPath returns where the descriptor lives: NEXUS_BENCH_DESCRIPTOR when
// set, else descriptor.json next to the sources of this package, whatever the
// working directory. A binary built with -trimpath reports a module-relative
// path that is no folder on disk, so this is meant for go run, go test and a
// plain go build.
func DescriptorPath() string {
	if v := os.Getenv(envDescriptor); v != "" {
		return v
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return descriptorFileName
	}
	return filepath.Join(filepath.Dir(filepath.FromSlash(file)), descriptorFileName)
}
