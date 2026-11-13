package imagesearch

import (
	"context"
	"errors"

	"github.com/anthnel/devdesk/internal/oci"
)

// ErrUnsupported: this registry has no search path yet (non-Hub until IS-03,
// dhi.io until IS-17).
var ErrUnsupported = errors.New("image search not supported on this registry")

// ByRegistry sends a Hub target (oci.IsDockerHub(t.RegistryURL)) to hub, any
// other to other; a nil searcher answers ErrUnsupported without any request.
func ByRegistry(hub, other Searcher) Searcher {
	return SearcherFunc(func(ctx context.Context, t Target, q Query) ([]Image, bool, error) {
		s := other
		if oci.IsDockerHub(t.RegistryURL) {
			s = hub
		}
		if s == nil {
			return nil, false, ErrUnsupported
		}
		return s.Search(ctx, t, q)
	})
}
