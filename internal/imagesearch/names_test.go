package imagesearch

import "testing"

func TestValidRepoPath(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"nginx", true},
		{"bitnami/nginx", true},
		{"e__f", true},
		{"a--b", true},
		{"a.b/c_d", true},
		{"-rm", false},
		{"--help", false},
		{"A", false},
		{"a//b", false},
		{"a/", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := validRepoPath(tt.name); got != tt.want {
			t.Errorf("validRepoPath(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestMatchTier(t *testing.T) {
	tests := []struct {
		name  string
		query string
		image string
		want  int
	}{
		{"exact", "nginx", "nginx", TierExact},
		{"exact ignores case of the name", "nginx", "NGINX", TierExact},
		{"exact ignores case of the query", "NGINX", "nginx", TierExact},
		{"last segment equals the query", "nginx", "bitnami/nginx", TierLastEquals},
		{"last segment equals, deeper path", "nginx", "a/b/nginx", TierLastEquals},
		{"last segment starts with the query", "nginx", "nginx-proxy", TierLastPrefix},
		{"last segment starts with the query, namespaced", "nginx", "bitnami/nginx-ingress", TierLastPrefix},
		{"name contains the query", "nginx", "my-nginx", TierContains},
		{"namespace contains the query only", "nginx", "nginxinc/proxy", TierContains},
		{"no match sorts after every tier", "nginx", "redis", TierNoMatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchTier(tt.query, tt.image); got != tt.want {
				t.Errorf("MatchTier(%q, %q) = %d, want %d", tt.query, tt.image, got, tt.want)
			}
		})
	}
}
