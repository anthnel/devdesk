package registrymgr

import "context"

// GroupMember represents a single member of a registry group.
type GroupMember struct {
	Alias string
	URL   string
}

// RegistryInfo carries the information needed for group detection.
// ManagementURL is optional: set it when the Docker registry URL differs from
// the URL the repository manager exposes for its API (group discovery, etc.).
type RegistryInfo struct {
	URL           string
	ManagementURL string
	Username      string
	Password      string
}

// Detector can identify whether a registry URL points to a group repository
// and enumerate its members.
type Detector interface {
	// CanHandle returns true if this detector recognises the registry.
	CanHandle(info RegistryInfo) bool
	// DetectGroup returns the group members when the registry is a group repo.
	// Returns nil, nil when the registry is not a group (not an error).
	DetectGroup(ctx context.Context, info RegistryInfo) ([]GroupMember, error)
}

var detectors []Detector

// Register adds a Detector to the global registry.
// Implementations call this from their init() function.
func Register(d Detector) {
	detectors = append(detectors, d)
}

// DetectGroup iterates registered detectors and returns members for the first
// one that recognises the registry. Returns nil, nil when no detector matches.
func DetectGroup(ctx context.Context, info RegistryInfo) ([]GroupMember, error) {
	for _, d := range detectors {
		if d.CanHandle(info) {
			return d.DetectGroup(ctx, info)
		}
	}
	return nil, nil
}
