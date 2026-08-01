package registrymgr

import "testing"

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
