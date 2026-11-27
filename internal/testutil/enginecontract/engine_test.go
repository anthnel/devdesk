package enginecontract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// The API these tests expect:
//
//	type Engine struct {
//		Tool    enginefixture.Tool
//		Version string             // the client version, equal to the server's for docker
//		Mode    enginefixture.Mode // rootful or rootless
//	}
//	type Probe func(tool enginefixture.Tool) (Engine, error)
//	func VersionsFile(getenv func(string) string) (string, error) // VersionsEnv, else tools/engine-vms/versions.env of the repository
//	func CheckSupported(e Engine, s enginefixture.Supported) error // the error names the version, N and N-1
//	func Banner(e Engine, runID string, m Mode) string

// versionsText is what tools/engine-vms/versions.env holds, reduced to the keys
// enginefixture.ParseVersions reads. It is synthetic: the tests do not follow the
// lines of the real file.
const versionsText = `# synthetic
DOCKER_N_VERSION=29.10.1
DOCKER_N1_VERSION=28.5.2
PODMAN_N_VERSION=6.2.1
PODMAN_N1_VERSION=5.8.11
`

func supported(t *testing.T) enginefixture.Supported {
	t.Helper()
	s, err := enginefixture.ParseVersions([]byte(versionsText))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCheckSupported(t *testing.T) {
	tests := []struct {
		name    string
		engine  enginecontract.Engine
		wantErr []string // substrings; nil: the engine is supported
	}{
		{"podman N", enginecontract.Engine{Tool: enginefixture.Podman, Version: "6.2.1", Mode: enginefixture.Rootless}, nil},
		{"podman N-1", enginecontract.Engine{Tool: enginefixture.Podman, Version: "5.8.11", Mode: enginefixture.Rootful}, nil},
		{"docker N", enginecontract.Engine{Tool: enginefixture.Docker, Version: "29.10.1", Mode: enginefixture.Rootful}, nil},
		{"docker N-1", enginecontract.Engine{Tool: enginefixture.Docker, Version: "28.5.2", Mode: enginefixture.Rootful}, nil},
		{
			name:    "a podman newer than N",
			engine:  enginecontract.Engine{Tool: enginefixture.Podman, Version: "6.3.0", Mode: enginefixture.Rootless},
			wantErr: []string{"6.3.0", "6.2.1, 5.8.11"},
		},
		{
			name:    "a podman older than N-1",
			engine:  enginecontract.Engine{Tool: enginefixture.Podman, Version: "5.7.0", Mode: enginefixture.Rootless},
			wantErr: []string{"5.7.0", "6.2.1, 5.8.11"},
		},
		{
			name:    "a docker version under podman is not a podman version",
			engine:  enginecontract.Engine{Tool: enginefixture.Podman, Version: "29.10.1", Mode: enginefixture.Rootless},
			wantErr: []string{"29.10.1", "6.2.1, 5.8.11"},
		},
		{
			name:    "a docker that is not N or N-1",
			engine:  enginecontract.Engine{Tool: enginefixture.Docker, Version: "27.0.0", Mode: enginefixture.Rootful},
			wantErr: []string{"27.0.0", "29.10.1, 28.5.2"},
		},
		{
			name:    "a tool the versions file does not list",
			engine:  enginecontract.Engine{Tool: enginefixture.Tool("nerdctl"), Version: "1.0.0", Mode: enginefixture.Rootful},
			wantErr: []string{"nerdctl"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enginecontract.CheckSupported(tt.engine, supported(t))

			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("CheckSupported() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("CheckSupported() = nil, want an error")
			}
			for _, w := range tt.wantErr {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("CheckSupported() = %q, want it to contain %q", err, w)
				}
			}
		})
	}
}

func TestBanner(t *testing.T) {
	tests := []struct {
		engine enginecontract.Engine
		mode   enginecontract.Mode
		want   string
	}{
		{
			enginecontract.Engine{Tool: enginefixture.Podman, Version: "6.2.1", Mode: enginefixture.Rootless},
			enginecontract.Record,
			"engine-contract: podman 6.2.1 rootless, run 20261127-a1b2c3d4, mode record",
		},
		{
			enginecontract.Engine{Tool: enginefixture.Docker, Version: "28.5.2", Mode: enginefixture.Rootful},
			enginecontract.Verify,
			"engine-contract: docker 28.5.2 rootful, run 20261127-a1b2c3d4, mode verify",
		},
	}
	for _, tt := range tests {
		if got := enginecontract.Banner(tt.engine, "20261127-a1b2c3d4", tt.mode); got != tt.want {
			t.Errorf("Banner() = %q, want %q", got, tt.want)
		}
	}
}

func TestVersionsFile(t *testing.T) {
	t.Run("the variable names the file, as it is in the test VM", func(t *testing.T) {
		got, err := enginecontract.VersionsFile(env(enginecontract.VersionsEnv, "/tmp/ddc-x/versions.env"))
		if err != nil || got != "/tmp/ddc-x/versions.env" {
			t.Errorf("VersionsFile() = %q, %v, want the value of the variable", got, err)
		}
	})

	t.Run("without it, the file of the repository", func(t *testing.T) {
		got, err := enginecontract.VersionsFile(env())
		if err != nil {
			t.Fatalf("VersionsFile() error = %v", err)
		}
		if !strings.HasSuffix(filepath.ToSlash(got), "tools/engine-vms/versions.env") {
			t.Fatalf("VersionsFile() = %q, want tools/engine-vms/versions.env", got)
		}
		data, err := os.ReadFile(got)
		if err != nil {
			t.Fatalf("the file VersionsFile() names cannot be read: %v", err)
		}
		if _, err := enginefixture.ParseVersions(data); err != nil {
			t.Errorf("the file VersionsFile() names is not a versions file: %v", err)
		}
	})
}
