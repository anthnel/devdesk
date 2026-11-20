package docker

import "testing"

// DEVDESK-629 (F7): hostKey is the host[:port] of an entry URL as written, only
// when it adds a lookup beyond what registryCandidates already tries.
func TestHostKey(t *testing.T) {
	tests := []struct {
		url    string
		want   string
		wantOK bool
	}{
		{"http://127.0.0.1:30481/repository/docker-hosted", "127.0.0.1:30481", true},
		{"nexus.example/repository/x", "nexus.example", true},
		{"https://nexus.example:443/r/x", "nexus.example:443", true},
		{"https://nexus.example", "", false},
		{"https://nexus.example/", "", false},
		{"docker.io", "", false},
		{"https://registry-1.docker.io/v2/library", "", false},
		{"://bad", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got, ok := hostKey(tt.url)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("hostKey(%q) = %q, %v; want %q, %v", tt.url, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestCredentialPhasesAreExactThenHostKey(t *testing.T) {
	phases := credentialPhases("http://127.0.0.1:30481/repository/docker-hosted")
	if len(phases) != 2 {
		t.Fatalf("%d phases, want 2 (exact, then host key)", len(phases))
	}
	if phases[0].helperHost != "127.0.0.1:30481/repository/docker-hosted" {
		t.Errorf("phase 0 helper host = %q, want the exact address", phases[0].helperHost)
	}
	if phases[1].helperHost != "127.0.0.1:30481" {
		t.Errorf("phase 1 helper host = %q, want the host key", phases[1].helperHost)
	}
	if got := credentialPhases("https://nexus.example"); len(got) != 1 {
		t.Errorf("a URL without a path has %d phases, want 1", len(got))
	}
}
