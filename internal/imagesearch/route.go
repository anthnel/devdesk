package imagesearch

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/anthnel/devdesk/internal/oci"
)

// ErrUnsupported: this registry has no search path yet (non-Hub until IS-03,
// dhi.io until IS-17).
var ErrUnsupported = errors.New("image search not supported on this registry")

// isDHI reports whether the registry's host is exactly dhi.io, ignoring
// scheme, port, path, case and a trailing dot. Userinfo is not the host.
func isDHI(registryURL string) bool {
	raw := strings.TrimSpace(registryURL)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSuffix(u.Hostname(), "."), "dhi.io")
}

// ByRegistry sends a Hub target (oci.IsDockerHub(t.RegistryURL)) to hub, any
// other to other; a nil searcher answers ErrUnsupported without any request.
// dhi.io has no documented catalog API: it is refused before any routing.
func ByRegistry(hub, other Searcher) Searcher {
	return SearcherFunc(func(ctx context.Context, t Target, q Query) ([]Image, bool, error) {
		if isDHI(t.RegistryURL) {
			return nil, false, ErrUnsupported
		}
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
