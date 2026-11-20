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
	"unicode"
)

// maxCatalogPage bounds one _catalog page: 1000 names of at most 255 bytes fit
// well under it.
const maxCatalogPage = 1 << 20

// Bounds on a catalog prefix: a repository path segment holds at most 255
// bytes and a whole name at most 1024 here, so a longer prefix names nothing
// and is refused before any request.
const (
	maxCatalogPrefixSegment = 255
	maxCatalogPrefix        = 1024
)

// ListCatalogContext walks the registry's /v2/_catalog from apiURL; it is
// ListCatalogUnderContext with an empty prefix.
func ListCatalogContext(ctx context.Context, apiURL, username, password string, maxPages, pageSize int) (names []string, truncated bool, err error) {
	return ListCatalogUnderContext(ctx, apiURL, "", username, password, maxPages, pageSize)
}

// ListCatalogUnderContext walks the catalog of the repository at prefix,
// /v2/<prefix>/_catalog (a Nexus group member, DEVDESK-579), or the registry's
// /v2/_catalog when prefix is empty. Names come back as the server sent them,
// without the prefix: rebuilding "<prefix>/<name>" is the caller's job.
// It asks pageSize names per page and follows Link rel="next" for at most
// maxPages pages. truncated is true when the walk stopped with names left: page
// cap reached with a next link, more than maxPages*pageSize names, or a next
// link to another origin (never followed: the Basic credentials and the token
// are not sent to it). HTTP 3xx redirects follow registryHTTPClient, which
// refuses off-origin ones (RefuseOffOriginRedirect, DEVDESK-410).
// Authentication is the tag flow's (Basic, then Bearer on a challenge, token
// kept across pages); a non-200 answer is a *StatusError. A prefix that
// checkCatalogPrefix refuses is an error before any request.
// With a non-empty prefix a next link is also followed only when its escaped
// path is exactly the member's catalog path: a link to /v2/_catalog or to
// another repository would attach names of another catalog to the prefix, so
// it ends the walk as truncated, unfollowed. With an empty prefix any
// same-origin path is followed, as ListCatalogContext always did.
func ListCatalogUnderContext(ctx context.Context, apiURL, prefix, username, password string, maxPages, pageSize int) (names []string, truncated bool, err error) {
	if maxPages <= 0 || pageSize <= 0 {
		return nil, false, fmt.Errorf("catalog walk needs positive limits, got %d pages of %d", maxPages, pageSize)
	}
	path, err := catalogPath(prefix)
	if err != nil {
		return nil, false, err
	}
	next := strings.TrimSuffix(apiURL, "/") + path + "?n=" + strconv.Itoa(pageSize)
	// memberPath is the only path a next link may take; "" means no constraint.
	memberPath := ""
	if prefix != "" {
		memberPath = path
	}
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
		if next = resolveNext(current, link); next != "" && (!sameOrigin(current, next) || !onCatalogPath(next, memberPath)) {
			return names, true, nil
		}
	}
	return names, false, nil
}

// catalogPath is the escaped catalog path for prefix: "/v2/_catalog" for an
// empty prefix, "/v2/<seg>/<seg>/_catalog" otherwise, each segment escaped
// with url.PathEscape so it stays one path segment on the wire.
func catalogPath(prefix string) (string, error) {
	if prefix == "" {
		return "/v2/_catalog", nil
	}
	if err := checkCatalogPrefix(prefix); err != nil {
		return "", err
	}
	segments := strings.Split(prefix, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return "/v2/" + strings.Join(segments, "/") + "/_catalog", nil
}

// onCatalogPath reports whether rawURL's escaped path is exactly want; an empty
// want accepts any path. An unparsable URL is refused.
func onCatalogPath(rawURL, want string) bool {
	if want == "" {
		return true
	}
	u, err := url.Parse(rawURL)
	return err == nil && u.EscapedPath() == want
}

// checkCatalogPrefix refuses, before any request, a prefix that could leave
// the member's catalog path or that no repository name holds: an empty segment
// (so a leading, trailing or doubled slash), "." or "..", any ".." (read
// literally, so "a..b" too), a backslash, a segment of spaces only, and any
// control character (NUL included), a segment over 255 bytes and a prefix over
// 1024 bytes. The error names the rule, never the prefix or any credential.
func checkCatalogPrefix(prefix string) error {
	if len(prefix) > maxCatalogPrefix {
		return fmt.Errorf("catalog prefix refused: longer than %d bytes", maxCatalogPrefix)
	}
	if strings.Contains(prefix, "..") {
		return errors.New("catalog prefix refused: contains \"..\"")
	}
	if strings.ContainsRune(prefix, '\\') {
		return errors.New("catalog prefix refused: contains a backslash")
	}
	if strings.IndexFunc(prefix, unicode.IsControl) >= 0 {
		return errors.New("catalog prefix refused: contains a control character")
	}
	for _, seg := range strings.Split(prefix, "/") {
		switch {
		case seg == "":
			return errors.New("catalog prefix refused: empty path segment")
		case seg == ".":
			return errors.New("catalog prefix refused: \".\" path segment")
		case strings.TrimSpace(seg) == "":
			return errors.New("catalog prefix refused: blank path segment")
		case len(seg) > maxCatalogPrefixSegment:
			return fmt.Errorf("catalog prefix refused: path segment longer than %d bytes", maxCatalogPrefixSegment)
		}
	}
	return nil
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
