package registrymgr

import "context"

func init() {
	Register(GenericDetector{})
}

// GenericDetector serves `generic` and every provider no detector implements.
//
// There is no vendor API to ask, so it reports that the registry is not a group
// rather than guessing at one. That is the whole point of it being last: a
// registry gets probed because it was declared as something, never because its
// URL looked like it.
type GenericDetector struct{}

// Provider returns the provider this detector serves.
func (GenericDetector) Provider() string { return ProviderGeneric }

// DetectGroup reports that nothing can be discovered without a manager to ask.
func (GenericDetector) DetectGroup(context.Context, RegistryInfo) ([]GroupMember, error) {
	return nil, nil
}
