package nexusbench

// Location is where an image of the data set lives: "docker-hosted" or "upstream".
type Location string

// The two places the data set puts an image.
const (
	// LocationHosted is the docker-hosted repository of the bench Nexus.
	LocationHosted Location = "docker-hosted"
	// LocationUpstream is the registry the docker-proxy repository mirrors.
	LocationUpstream Location = "upstream"
)

// DatasetImage is one image of the fixed data set.
type DatasetImage struct {
	Name, Tag   string
	Location    Location
	PullThrough bool   // pulled once through docker-proxy by Seed
	Digest      string // manifest digest, sha256:...
}

// datasetTable is the fixed data set of DEVDESK-505 (D-I), in order. The digests
// are not written here: Dataset computes them from the generator, so a change of
// the generator can never leave a stale value behind.
var datasetTable = []DatasetImage{
	{Name: "api", Tag: "1.0.0", Location: LocationHosted},
	{Name: "api", Tag: "1.1.0", Location: LocationHosted},
	{Name: "team/api", Tag: "1.0.0", Location: LocationHosted},
	{Name: "tools/cli", Tag: "0.1.0", Location: LocationHosted},
	// The same name and tag in both places, with different contents: a read
	// through the group says which member answered.
	{Name: "shared/base", Tag: "1.0", Location: LocationHosted},
	{Name: "shared/base", Tag: "1.0", Location: LocationUpstream},
	{Name: "mirror/cached", Tag: "1.0", Location: LocationUpstream, PullThrough: true},
	{Name: "mirror/uncached", Tag: "1.0", Location: LocationUpstream},
}

// Dataset returns the fixed data set, in order, with the manifest digest of every
// image filled from the layout generator. The slice is a copy.
func Dataset() []DatasetImage {
	images := make([]DatasetImage, len(datasetTable))
	for i, img := range datasetTable {
		img.Digest = imageDigest(img)
		images[i] = img
	}
	return images
}
