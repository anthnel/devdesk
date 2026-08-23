package registrymgr

import (
	"strings"
	"testing"
)

func TestParseNexusURL(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantHost string
		wantRepo string
		wantErr  bool
	}{
		{
			name:     "canonical https repository URL",
			in:       "https://nexus.example.com/repository/docker-group",
			wantHost: "https://nexus.example.com",
			wantRepo: "docker-group",
		},
		{
			name:     "scheme defaults to https when omitted",
			in:       "nexus.example.com/repository/docker-group",
			wantHost: "https://nexus.example.com",
			wantRepo: "docker-group",
		},
		{
			name:     "explicit http scheme and port are preserved",
			in:       "http://nexus.local:8081/repository/docker-hosted",
			wantHost: "http://nexus.local:8081",
			wantRepo: "docker-hosted",
		},
		{
			name:     "trailing registry path after the repository name is ignored",
			in:       "https://nexus.example.com/repository/docker-group/v2/_catalog",
			wantHost: "https://nexus.example.com",
			wantRepo: "docker-group",
		},
		{
			name:     "trailing slash after the repository name",
			in:       "https://nexus.example.com/repository/docker-group/",
			wantHost: "https://nexus.example.com",
			wantRepo: "docker-group",
		},
		{
			name:    "a plain registry URL is not a Nexus repository",
			in:      "https://registry.example.com/v2/",
			wantErr: true,
		},
		{
			name:    "repository path with no repository name",
			in:      "https://nexus.example.com/repository/",
			wantErr: true,
		},
		{
			name:    "host without a path",
			in:      "https://nexus.example.com",
			wantErr: true,
		},
		{
			name:    "empty input",
			in:      "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, repo, err := parseNexusURL(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseNexusURL(%q) = (%q, %q, nil), want an error", tt.in, host, repo)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseNexusURL(%q) returned unexpected error: %v", tt.in, err)
			}
			if host != tt.wantHost || repo != tt.wantRepo {
				t.Errorf("parseNexusURL(%q) = (%q, %q), want (%q, %q)", tt.in, host, repo, tt.wantHost, tt.wantRepo)
			}
		})
	}
}

func TestCleanMemberAlias(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"proxy suffix is stripped", "docker-proxy", "docker"},
		{"hosted suffix is stripped", "docker-hosted", "docker"},
		{"local suffix is stripped", "maven-local", "maven"},
		{"unknown suffix is preserved", "docker-group", "docker-group"},
		{"name without a suffix is unchanged", "docker", "docker"},
		{"empty name", "", ""},
		{"only the trailing suffix is stripped", "proxy-repo-proxy", "proxy-repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanMemberAlias(tt.in); got != tt.want {
				t.Errorf("cleanMemberAlias(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A member is an address, not a URL (§3.18). The synthesis this replaces put the
// member's name into the *path* — `host/repository/<name>` — which browses and
// cannot pull: the Docker client puts `/v2/` first and the whole path after it,
// so the two ends of the same row meant two different repositories (D39).
func TestAMemberIsAddressedByPrefixAndNeverBySynthesisedPath(t *testing.T) {
	tests := []struct {
		name       string
		dockerURL  string
		member     string
		wantURL    string
		wantPrefix string
	}{
		{
			name:      "a group reached by path prefix serves its members the same way",
			dockerURL: "https://nexus.example.com/repository/docker-group",
			member:    "dhi-io-proxy",
			// The host, and the member in front of the repository name: browse
			// and pull build the same path from it.
			wantURL: "https://nexus.example.com", wantPrefix: "dhi-io-proxy",
		},
		{
			name:      "the scheme the group was written with is kept",
			dockerURL: "http://nexus.local:8081/repository/docker-group",
			member:    "quay-io-proxy",
			wantURL:   "http://nexus.local:8081", wantPrefix: "quay-io-proxy",
		},
		{
			name: "a connector port is the group's own, so nothing is claimed about a member",
			// Which connector a member answers on is a setting on that
			// repository, and the endpoint carrying it is refused to an ordinary
			// pull account. An empty prefix is wrong and visibly so; a
			// synthesised path is wrong and plausible.
			dockerURL: "nexus.example.com:8082",
			member:    "dhi-io-proxy",
			wantURL:   "nexus.example.com:8082", wantPrefix: "",
		},
		{
			name:      "a subdomain connector says nothing about a member either",
			dockerURL: "https://nexus-docker-group.example.com",
			member:    "dhi-io-proxy",
			wantURL:   "https://nexus-docker-group.example.com", wantPrefix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, prefix := memberAddress(tt.dockerURL, tt.member)
			if url != tt.wantURL || prefix != tt.wantPrefix {
				t.Errorf("memberAddress(%q, %q) = (%q, %q), want (%q, %q)",
					tt.dockerURL, tt.member, url, prefix, tt.wantURL, tt.wantPrefix)
			}
			if strings.Contains(url, "/repository/") {
				t.Errorf("the member URL carries a synthesised repository path: %q", url)
			}
		})
	}
}
