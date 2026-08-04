package registrymgr

import "context"

// The repository managers a group may declare. These repeat the values in
// internal/config on purpose: config owns what a config file may say, this
// package owns what can actually be detected, and neither should have to import
// the other to say so. TestTheProviderVocabularyMatchesTheConfig pins them
// together.
const (
	ProviderGeneric     = "generic"
	ProviderNexus       = "nexus"
	ProviderHarbor      = "harbor"
	ProviderArtifactory = "artifactory"
	ProviderGitLab      = "gitlab"
)

// GroupMember represents a single member of a registry group.
type GroupMember struct {
	Alias string
	URL   string
}

// RegistryInfo carries the information needed for group detection.
// ManagementURL is optional: set it when the Docker registry URL differs from
// the URL the repository manager exposes for its API (group discovery, etc.).
type RegistryInfo struct {
	// Provider is the declared repository manager, and it is what picks the
	// detector. It is not sniffed from the URL: guessing made the detector list
	// effectively single-vendor and probed registries that had never claimed to
	// be one (§3.8, decision F).
	Provider      string
	URL           string
	ManagementURL string
	Username      string
	Password      string
}

// Detector enumerates the members of a group repository for one provider.
type Detector interface {
	// Provider returns the declared provider this detector serves.
	Provider() string
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

// DetectGroup dispatches to the detector serving info.Provider, falling back to
// the generic one — which discovers nothing — for a provider no detector
// implements. Registration order does not matter: the match is on a declared
// value, and the fallback is chosen only when nothing claims it.
func DetectGroup(ctx context.Context, info RegistryInfo) ([]GroupMember, error) {
	var fallback Detector
	for _, d := range detectors {
		if d.Provider() == info.Provider && info.Provider != "" {
			return d.DetectGroup(ctx, info)
		}
		if d.Provider() == ProviderGeneric {
			fallback = d
		}
	}
	if fallback == nil {
		return nil, nil
	}
	return fallback.DetectGroup(ctx, info)
}
