package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// maxCatalogPage bounds one _catalog page: 1000 names of at most 255 bytes fit
// well under it.
const maxCatalogPage = 1 << 20

// ListCatalogContext walks the registry's /v2/_catalog from apiURL, asking
// pageSize names per page and following Link rel="next" for at most maxPages
// pages. truncated is true when the walk stopped with names left: page cap
// reached with a next link, more than maxPages*pageSize names, or a next link
// to another origin (never followed: the Basic credentials and the token are
// not sent to it). That guard covers the next link only: HTTP 3xx redirects
// follow registryHTTPClient's behavior, not hardened, as for tags.
// Authentication is the tag flow's (Basic, then Bearer on a challenge, token
// kept across pages); a non-200 answer is a *StatusError.
func ListCatalogContext(ctx context.Context, apiURL, username, password string, maxPages, pageSize int) (names []string, truncated bool, err error) {
	if maxPages <= 0 || pageSize <= 0 {
		return nil, false, fmt.Errorf("catalog walk needs positive limits, got %d pages of %d", maxPages, pageSize)
	}
	next := strings.TrimSuffix(apiURL, "/") + "/v2/_catalog?n=" + strconv.Itoa(pageSize)
	token := ""
	for page := 0; next != ""; page++ {
		if page == maxPages {
			return names, true, nil
		}
		body, link, usedToken, err := registryGETBoundedContext(ctx, next, username, password, token, maxCatalogPage)
		if err != nil {
			return nil, false, err
		}
		token = usedToken
		var result struct {
			Repositories []string `json:"repositories"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, false, fmt.Errorf("decode catalog response: %w", err)
		}
		names = append(names, result.Repositories...)
		if len(names) > maxPages*pageSize {
			return names[:maxPages*pageSize], true, nil
		}
		current := next
		if next = resolveNext(current, link); next != "" && !sameOrigin(current, next) {
			return names, true, nil
		}
	}
	return names, false, nil
}

// registryGETBoundedContext is registryGETContext with an explicit body cap:
// a body over limit is an error, not a silent cut.
func registryGETBoundedContext(ctx context.Context, rawURL, username, password, token string, limit int64) (body []byte, link, usedToken string, err error) {
	resp, token, err := registryDoContext(ctx, http.MethodGet, rawURL, "", username, password, token)
	if err != nil {
		return nil, "", token, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", token, &StatusError{Code: resp.StatusCode, Source: SourceRegistry}
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && int64(len(body)) > limit {
		err = errors.New("catalog page over " + strconv.FormatInt(limit, 10) + " bytes")
	}
	if err != nil {
		return nil, "", token, err
	}
	return body, resp.Header.Get("Link"), token, nil
}

// sameOrigin reports whether two absolute URLs share scheme, host and port.
func sameOrigin(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	return errA == nil && errB == nil && ua.Scheme == ub.Scheme && ua.Host == ub.Host
}
