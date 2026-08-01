package docker

import "testing"

func TestIsContainerID(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"short ID from docker ps", "a1b2c3d4e5f6", true},
		{"full SHA-256 ID", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", true},
		{"all digits", "000000000000", true},
		{"all hex letters", "abcdefabcdef", true},

		{"empty", "", false},
		{"too short", "a1b2c3d4e5", false},
		{"too long", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0", false},
		{"uppercase hex", "A1B2C3D4E5F6", false},
		{"non-hex letter", "a1b2c3d4e5g6", false},
		{"container name instead of ID", "my-container", false},

		// Shell metacharacters must never pass: these IDs are embedded in the
		// command lines built by the log and inspect pagers.
		{"semicolon injection", "a1b2c3d4e5f6; rm -rf /", false},
		{"pipe injection", "a1b2c3d4e5f6 | cat /etc/passwd", false},
		{"command substitution", "a1b2c3d4e5f6$(whoami)", false},
		{"backtick substitution", "a1b2c3d4e5f6`id`", false},
		{"newline injection", "a1b2c3d4e5f6\nwhoami", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsContainerID(tt.id); got != tt.want {
				t.Errorf("IsContainerID(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}
