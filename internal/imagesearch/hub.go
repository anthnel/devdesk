package imagesearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/anthnel/devdesk/internal/oci"
)

// hubSearchURL is the v1 search endpoint `docker search` uses; not a public
// Hub contract.
var hubSearchURL = "https://index.docker.io/v1/search"

// HubLimit is the validated cap: one request, one page.
const HubLimit = 50

// hubMaxBody caps what is read from the response.
const hubMaxBody = 1 << 20

// HubSearcher searches Docker Hub anonymously: one GET, no credentials, no
// pagination. Whatever Target carries for authentication is ignored.
type HubSearcher struct {
	// Endpoint overrides hubSearchURL when set.
	Endpoint string
	// Client overrides the default client (15 s timeout, no redirects).
	Client *http.Client
}

type hubResponse struct {
	Results []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		IsOfficial  bool   `json:"is_official"`
		StarCount   *int   `json:"star_count"`
	} `json:"results"`
}

// Search runs one anonymous search. A non-200 answer is an *oci.StatusError.
func (h HubSearcher) Search(ctx context.Context, t Target, q Query) ([]Image, bool, error) {
	limit := q.Limit
	if limit <= 0 || limit > HubLimit {
		limit = HubLimit
	}
	endpoint := h.Endpoint
	if endpoint == "" {
		endpoint = hubSearchURL
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, false, fmt.Errorf("hub search: %w", err)
	}
	vals := u.Query()
	vals.Set("q", q.Text)
	vals.Set("n", strconv.Itoa(limit))
	u.RawQuery = vals.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, false, fmt.Errorf("hub search: %w", err)
	}
	client := h.Client
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("hub search: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, false, &oci.StatusError{Code: resp.StatusCode, Source: oci.SourceRegistry}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, hubMaxBody+1))
	if err != nil {
		return nil, false, fmt.Errorf("hub search: %w", err)
	}
	if len(body) > hubMaxBody {
		return nil, false, fmt.Errorf("hub search: response over %d bytes", hubMaxBody)
	}
	var parsed hubResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, false, fmt.Errorf("hub search: %w", err)
	}

	prefix := strings.Trim(t.RepoPrefix, "/")
	var out []Image
	for _, r := range parsed.Results {
		repo, ok := hubRepo(r.Name)
		if !ok {
			continue
		}
		if prefix != "" && !strings.HasPrefix(repo, prefix+"/") {
			continue
		}
		img := Image{
			EntryKey:    t.EntryKey,
			Name:        strings.TrimPrefix(repo, "library/"),
			Repo:        repo,
			Description: sanitizeDescription(r.Description),
			Official:    r.IsOfficial,
		}
		if r.StarCount != nil {
			img.Stars, img.HasStars = *r.StarCount, true
		}
		out = append(out, img)
		if len(out) == HubLimit {
			break
		}
	}
	return out, false, nil
}

// hubRepo validates a name Hub returned and returns the final repository path:
// a bare name lives under library/ (D39). The name ends up in a docker
// command, so anything that is not a valid repository name is dropped.
func hubRepo(name string) (string, bool) {
	if !validRepoPath(name) {
		return "", false
	}
	if !strings.Contains(name, "/") {
		return "library/" + name, true
	}
	return name, true
}

// sanitizeDescription removes escape sequences and control characters (a
// description is remote, untrusted text), invisible format characters (bidi
// overrides, zero-width, BOM: category Cf) and collapses whitespace.
func sanitizeDescription(s string) string {
	s = ansi.Strip(s)
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
