package config

// How far a base image may move when remediation proposes a bump (§3.2).
//
// The names are stated here and in internal/remediation, which config must not
// import; TestTheTrackNamesMatchTheConfig (internal/remediation) holds the two
// in step — the same arrangement as forge.type and the container engines.
const (
	// BaseImageTrackSameLine keeps the major version: 3.18 may become 3.21,
	// never 4.0. The default, because a scan proves the CVEs are gone but not
	// that an application survives a major upgrade.
	BaseImageTrackSameLine = "same-line"
	// BaseImageTrackNextMajor also allows the next major version that exists.
	BaseImageTrackNextMajor = "next-major"
)

// BaseImageTracks is the set scan.base_image_track cycles through in the
// configuration view.
func BaseImageTracks() []string {
	return []string{BaseImageTrackSameLine, BaseImageTrackNextMajor}
}
