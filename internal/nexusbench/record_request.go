package nexusbench

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/anthnel/devdesk/internal/nexusfixture"
)

// What the recorder sends besides the GET of the closed list (record.go), each
// behind a guard (DEVDESK-580, PR2):
//
//   - the HEAD of a manifest. Through docker-hosted, on the HTTP port, it changes
//     nothing. Through docker-proxy, docker-group or a Docker connector it can cache
//     an image in the proxy: it is sent only for a fixture that says "effect:
//     caches" and a run that says --allow-cache-effects;
//   - the GET of a blob of docker-hosted, on the HTTP port, for a fixture that says
//     "flow: redirects" (the blobs are where a registry redirects to storage);
//   - a GET of a manifest, or of a blob anywhere else, stays refused: it would cache
//     an image through the proxy.
//
// The token paths are in no list: a Bearer flow builds its token request from the
// challenge (record_flow.go).

const (
	// repoNamePattern is an image name, and refPattern a tag: no escape, no upper
	// case in a name, as a registry holds them.
	repoNamePattern = `[a-z0-9._-]+(?:/[a-z0-9._-]+)*`
	refPattern      = `[A-Za-z0-9._-]+`
	manifestTail    = `/manifests/` + refPattern + `$`

	hostedRoutes = `(?:/repository/` + dockerHostedRepository + `/v2|/v2/` + dockerHostedRepository + `)`
	benchRepos   = `(?:` + dockerHostedRepository + `|` + dockerProxyRepository + `|` + dockerGroupRepository + `)`

	// checkClosedList is what to do about a request outside what the recorder sends.
	checkClosedList = "record only GET requests of the closed list of paths, the HEAD of a manifest, or in a redirects flow the blob of docker-hosted"
	// checkCacheEffects is what to do about a HEAD that can cache an image.
	checkCacheEffects = `say "# effect: caches" in the fixture and run record with --allow-cache-effects, then reset the bench`
)

var (
	// hostedManifest is a manifest of docker-hosted, by its repository path or by
	// the path routing of the HTTP port.
	hostedManifest = regexp.MustCompile(`^` + hostedRoutes + `/` + repoNamePattern + manifestTail)
	// hostedBlob is a blob of docker-hosted, named by its sha256 digest.
	hostedBlob = regexp.MustCompile(`^` + hostedRoutes + `/` + repoNamePattern + `/blobs/sha256:[0-9a-f]{64}$`)
	// restManifest is a manifest of any repository of the bench on the HTTP port.
	restManifest = regexp.MustCompile(`^(?:/repository/` + benchRepos + `/v2|/v2/` + benchRepos + `)/` + repoNamePattern + manifestTail)
	// connectorManifest is a manifest on a Docker connector, which fronts the group.
	connectorManifest = regexp.MustCompile(`^/v2/` + repoNamePattern + manifestTail)
)

// refusedFixtureRequest returns why the first request of a fixture may not be sent
// (reason, with what to do in check), or "". restHost is the host:port of the HTTP
// port, the only one that serves a repository path.
func refusedFixtureRequest(ex nexusfixture.Exchange, meta nexusfixture.Meta, restHost string, allowCache bool) (reason, check string) {
	switch {
	case ex.Method == http.MethodHead:
		return refusedHead(ex, meta, restHost, allowCache)
	case ex.Method == http.MethodGet && meta.Flow == nexusfixture.FlowRedirects &&
		ex.Host == restHost && !strings.Contains(ex.Target, "..") && hostedBlob.MatchString(ex.Target):
		return "", ""
	}
	if reason := refusedRequest(ex.Method, ex.Target); reason != "" {
		return reason, checkClosedList
	}
	return "", ""
}

// refusedHead applies the rules of a HEAD: a manifest of the bench, and the cache
// guards for what is not docker-hosted.
func refusedHead(ex nexusfixture.Exchange, meta nexusfixture.Meta, restHost string, allowCache bool) (reason, check string) {
	target := ex.Target
	onREST := ex.Host == restHost
	manifest := connectorManifest
	if onREST {
		manifest = restManifest
	}
	switch {
	case strings.Contains(target, "..") || strings.ContainsAny(target, "?%#"):
		return "the HEAD target holds a query, an escape or a dot-dot", checkClosedList
	case onREST && hostedManifest.MatchString(target):
		return "", ""
	case !manifest.MatchString(target):
		return "a HEAD is sent for the manifest of an image of the bench only", checkClosedList
	case meta.Effect != nexusfixture.EffectCaches:
		return "a HEAD of a manifest through the proxy, the group or a connector can change the bench: it can cache an image", checkCacheEffects
	case !allowCache:
		return "a HEAD of a manifest that can cache an image is sent only with --allow-cache-effects", checkCacheEffects
	}
	return "", ""
}
