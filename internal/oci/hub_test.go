package oci

import "testing"

func TestIsDockerHub(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"docker.io", true},
		{"docker.io/", true},
		{"https://docker.io", true},
		{"https://docker.io/", true},
		{"http://docker.io", true},
		{"registry-1.docker.io", true},
		{"https://registry-1.docker.io/", true},
		{" Docker.IO ", true},
		{"HTTPS://DOCKER.IO", true},
		{"docker.io.evil.com", false},
		{"nexus/docker.io", false},
		{"registry.example.com", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := IsDockerHub(tt.in); got != tt.want {
				t.Errorf("IsDockerHub(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestRegistryHost(t *testing.T) {
	tests := []struct{ in, want string }{
		{"docker.io", "docker.io"},
		{"https://docker.io", "docker.io"},
		{"https://docker.io/", "docker.io"},
		{"http://localhost:5000", "localhost:5000"},
		{"HTTPS://Nexus.Example.com/repository/docker-group/", "Nexus.Example.com/repository/docker-group"},
		{"  registry-1.docker.io/ ", "registry-1.docker.io"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := RegistryHost(tt.in); got != tt.want {
				t.Errorf("RegistryHost(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
