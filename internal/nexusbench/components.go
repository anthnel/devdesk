package nexusbench

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const (
	// componentsPath lists the components of a repository.
	componentsPath = restAPI + "/components"
	// maxComponentPages bounds a listing: a continuation token that never ends
	// is an error after this many calls, not an endless loop.
	maxComponentPages = 1000
	// maxListedComponents bounds the components one listing returns: more than
	// this is an error, never a partial result (F6 of the audit).
	maxListedComponents = 5000
	// manifestMarker is what the path of a Docker manifest asset holds:
	// v2/<name>/manifests/<tag>.
	manifestMarker = "/manifests/"

	// upCheck is what a missing admin password asks the operator to do.
	upCheck = "run `mise run nexus-bench:up` to provision the bench"
)

// component is one image tag a repository lists: the image name, the tag and,
// when the instance lists it, the digest of the manifest ("sha256:<hex>").
type component struct {
	Name    string
	Version string
	Digest  string
}

// ListedImage is one image tag a repository lists: the name, the tag and the
// manifest digest ("sha256:<hex>", empty when the instance lists none).
type ListedImage struct{ Name, Tag, Digest string }

// ListImagesAnonymously lists the images of repo on the instance at baseURL
// with no credential at all: it is a read of what anonymous access allows, and
// a 401 or 403 is an *APIError like any other refusal. It is the listing of the
// status command, which never reads docker-group (F9) and never starts skopeo.
func ListImagesAnonymously(ctx context.Context, baseURL, repo string) ([]ListedImage, error) {
	c := newClient(baseURL, "", "")
	defer c.closeIdle()
	list, err := c.listComponents(ctx, repo)
	if err != nil {
		return nil, err
	}
	out := make([]ListedImage, 0, len(list))
	for _, comp := range list {
		out = append(out, ListedImage{Name: comp.Name, Tag: comp.Version, Digest: comp.Digest})
	}
	return out, nil
}

// adminFromStore returns a client authenticated with the stored admin password
// and the stored secrets. It never reads the initial password of the pod: seed,
// restore and reset need a bench that up already provisioned. It sends no
// request, so a stored password the instance refuses shows on the first call.
func adminFromStore(ctx context.Context, baseURL string, store SecretStore) (*client, Secrets, error) {
	if err := ctx.Err(); err != nil {
		return nil, Secrets{}, &transportError{Op: "read the stored admin password", Err: err}
	}
	sec, found, err := store.Load()
	if err != nil {
		return nil, Secrets{}, &BenchError{
			Kind:  ProvisionFailed,
			Cause: "the secrets file of the bench cannot be read",
			Check: "check the permissions and the content of the secrets file, then " + upCheck,
			Err:   err,
		}
	}
	if !found || sec.AdminPassword == "" {
		return nil, Secrets{}, &BenchError{
			Kind:  ProvisionFailed,
			Cause: "the bench has no admin password stored: it was never provisioned, or its provisioning did not finish",
			Check: upCheck,
			Err:   errors.New("no admin password in the secrets file"),
		}
	}
	return newClient(baseURL, adminUserID, sec.AdminPassword), sec, nil
}

// componentPage is one page of GET /v1/components.
type componentPage struct {
	Items             []componentItem `json:"items"`
	ContinuationToken *string         `json:"continuationToken"`
}

type componentItem struct {
	Name    string           `json:"name"`
	Version string           `json:"version"`
	Assets  []componentAsset `json:"assets"`
}

type componentAsset struct {
	Path     string `json:"path"`
	Checksum struct {
		SHA256 string `json:"sha256"`
	} `json:"checksum"`
}

// listComponents lists the components of repo, following the continuation token
// until the last page; a token equal to the previous one is an error. A
// repository the instance does not know lists as no component. An error on any
// page drops what the earlier pages returned.
//
// H-COMPONENTS (to settle at step 12): a Docker component is one image tag, and
// the sha256 checksum of its asset under v2/<name>/manifests/<tag> is the digest
// of the manifest.
func (c *client) listComponents(ctx context.Context, repo string) ([]component, error) {
	op := "list the components of " + repo
	var out []component
	token := ""
	for page := 0; page < maxComponentPages; page++ {
		query := url.Values{"repository": {repo}}
		if token != "" {
			query.Set("continuationToken", token)
		}
		var body componentPage
		found, err := c.get(ctx, op, componentsPath, query, &body)
		if err != nil {
			return nil, err
		}
		if !found {
			if page == 0 {
				return nil, nil
			}
			return nil, &APIError{Op: op, Status: http.StatusNotFound}
		}
		if len(out)+len(body.Items) > maxListedComponents {
			return nil, &tooManyError{Op: op}
		}
		for _, item := range body.Items {
			out = append(out, component{Name: item.Name, Version: item.Version, Digest: manifestDigest(item)})
		}
		if body.ContinuationToken == nil || *body.ContinuationToken == "" {
			return out, nil
		}
		// The same token as the previous page would repeat that page for ever.
		if *body.ContinuationToken == token {
			return nil, &decodeError{Op: op, Err: errors.New("the continuation token repeats")}
		}
		token = *body.ContinuationToken
	}
	return nil, &decodeError{Op: op, Err: errors.New("the continuation token never ends")}
}

// manifestDigest returns "sha256:<hex>" of the manifest asset of item, or "" when
// the instance lists none. The asset of the tag itself wins over any other
// manifest asset (a digest-addressed one, for instance).
func manifestDigest(item componentItem) string {
	digest := ""
	for _, a := range item.Assets {
		if a.Checksum.SHA256 == "" || !strings.Contains(a.Path, manifestMarker) {
			continue
		}
		if strings.HasSuffix(a.Path, manifestMarker+item.Version) {
			return "sha256:" + a.Checksum.SHA256
		}
		if digest == "" {
			digest = "sha256:" + a.Checksum.SHA256
		}
	}
	return digest
}
