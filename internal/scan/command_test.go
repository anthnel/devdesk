package scan

import (
	"path/filepath"
	"strings"
	"testing"
)

// The displayed command is what the user copies when a scan fails and they want
// to reproduce it by hand, so it has to match what was run — see D19.

// ── Trivy ────────────────────────────────────────────────────────────────────

func TestTrivyScansADirectoryAsAFilesystem(t *testing.T) {
	cmd := GetTrivyCommand("/repos/devdesk", TargetDirectory, false, ToolSourceBinary, "", "", false, false)

	for _, want := range []string{"trivy", "fs", "--format json", "--scanners vuln", "/repos/devdesk"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("the command is missing %q:\n%s", want, cmd)
		}
	}
}

// License mode is the same fs scan with a different scanner; asking for both at
// once would double the run time for one of them.
func TestLicenseModeSwapsTheScanner(t *testing.T) {
	cmd := GetTrivyCommand("/repos/devdesk", TargetDirectory, true, ToolSourceBinary, "", "", false, false)

	if !strings.Contains(cmd, "--scanners license") {
		t.Errorf("license mode did not select the license scanner:\n%s", cmd)
	}
	if strings.Contains(cmd, "--scanners vuln") {
		t.Errorf("license mode also asked for vulnerabilities:\n%s", cmd)
	}
}

func TestTrivyScansAnImageByName(t *testing.T) {
	cmd := GetTrivyCommand("api:v1", TargetImage, false, ToolSourceBinary, "", "", false, false)

	if !strings.Contains(cmd, "image") || !strings.Contains(cmd, "api:v1") {
		t.Errorf("the image command is wrong:\n%s", cmd)
	}
	// An image scan has no filesystem scanner to select.
	if strings.Contains(cmd, "--scanners") {
		t.Errorf("an image scan selected a scanner:\n%s", cmd)
	}
}

// In Docker mode the target is mounted read-only and the container sees it at a
// fixed path, so the argument passed to trivy is /scan, not the host path.
func TestDockerModeMountsTheDirectoryReadOnly(t *testing.T) {
	cmd := GetTrivyCommand("/repos/devdesk", TargetDirectory, false, ToolSourceDocker, "", "", false, false)

	if !strings.Contains(cmd, "-v /repos/devdesk:/scan:ro") {
		t.Errorf("the target is not mounted read-only:\n%s", cmd)
	}
	if !strings.HasSuffix(cmd, " /scan") {
		t.Errorf("trivy was pointed at the host path rather than the mount:\n%s", cmd)
	}
	if !strings.Contains(cmd, DefaultTrivyImage) {
		t.Errorf("no image named; the default should be filled in:\n%s", cmd)
	}
}

// Scanning an image from a container needs the host's Docker socket — unless a
// Trivy server is doing the work, in which case handing the socket over would
// be a needless grant.
func TestTheDockerSocketIsMountedOnlyWhenItIsNeeded(t *testing.T) {
	local := GetTrivyCommand("api:v1", TargetImage, false, ToolSourceDocker, "", "", false, false)
	if !strings.Contains(local, "/var/run/docker.sock") {
		t.Errorf("an image scan without a server has no socket to inspect the image with:\n%s", local)
	}

	served := GetTrivyCommand("api:v1", TargetImage, false, ToolSourceDocker, "", "https://trivy:4954", false, false)
	if strings.Contains(served, "/var/run/docker.sock") {
		t.Errorf("the socket was mounted although a server does the work:\n%s", served)
	}
}

func TestAConfiguredImageOverridesTheDefault(t *testing.T) {
	cmd := GetTrivyCommand("/repos", TargetDirectory, false, ToolSourceDocker, "mirror.local/trivy:0.50", "", false, false)

	if !strings.Contains(cmd, "mirror.local/trivy:0.50") {
		t.Errorf("the configured image was not used:\n%s", cmd)
	}
	if strings.Contains(cmd, DefaultTrivyImage) {
		t.Errorf("the default image was used as well:\n%s", cmd)
	}
}

func TestOptionalTrivyFlagsAppearOnlyWhenAsked(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		flag string
		want bool
	}{
		{"server off", GetTrivyCommand("/r", TargetDirectory, false, ToolSourceBinary, "", "", false, false), "--server", false},
		{"server on", GetTrivyCommand("/r", TargetDirectory, false, ToolSourceBinary, "", "https://trivy:4954", false, false), "--server https://trivy:4954", true},
		{"ignore-unfixed off", GetTrivyCommand("/r", TargetDirectory, false, ToolSourceBinary, "", "", false, false), "--ignore-unfixed", false},
		{"ignore-unfixed on", GetTrivyCommand("/r", TargetDirectory, false, ToolSourceBinary, "", "", true, false), "--ignore-unfixed", true},
		{"ignore-eol off", GetTrivyCommand("/r", TargetDirectory, false, ToolSourceBinary, "", "", false, false), "--ignore-status", false},
		{"ignore-eol on", GetTrivyCommand("/r", TargetDirectory, false, ToolSourceBinary, "", "", false, true), "--ignore-status end_of_life", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strings.Contains(tt.cmd, tt.flag); got != tt.want {
				t.Errorf("contains %q = %v, want %v:\n%s", tt.flag, got, tt.want, tt.cmd)
			}
		})
	}
}

// An unsupported target type has no command; the caller logs the empty string
// rather than a half-built one.
func TestAnUnsupportedTargetTypeHasNoCommand(t *testing.T) {
	if got := GetTrivyCommand("x", TargetType("registry"), false, ToolSourceBinary, "", "", false, false); got != "" {
		t.Errorf("GetTrivyCommand for an unknown target type = %q, want empty", got)
	}
	if got := GetTrivyMisconfigCommand("x", TargetType("registry"), ToolSourceBinary, "", "", false); got != "" {
		t.Errorf("GetTrivyMisconfigCommand for an unknown target type = %q, want empty", got)
	}
	if got := GetSBOMCommand("x", TargetType("registry"), ToolSourceBinary, "", "", ""); got != "" {
		t.Errorf("GetSBOMCommand for an unknown target type = %q, want empty", got)
	}
}

// D19 was the two builders drifting apart. They are now one, so the shown
// command is the executed one by construction — this pins that.
func TestTheShownCommandIsTheExecutedOne(t *testing.T) {
	tc, err := trivyArgs("/repos", TargetDirectory, false, ToolSourceBinary, "", "https://trivy:4954", true, true)
	if err != nil {
		t.Fatalf("building failed: %v", err)
	}

	shown := GetTrivyCommand("/repos", TargetDirectory, false, ToolSourceBinary, "", "https://trivy:4954", true, true)
	if shown != tc.String() {
		t.Errorf("shown:\n%s\nexecuted:\n%s", shown, tc.String())
	}
}

// ── Misconfiguration and SBOM ────────────────────────────────────────────────

// Misconfiguration scanning reads configuration files, so unlike the license
// scanner it applies to images as well as directories.
func TestMisconfigScanningAppliesToBothTargetTypes(t *testing.T) {
	dir := GetTrivyMisconfigCommand("/repos", TargetDirectory, ToolSourceBinary, "", "", false)
	img := GetTrivyMisconfigCommand("api:v1", TargetImage, ToolSourceBinary, "", "", false)

	for name, cmd := range map[string]string{"directory": dir, "image": img} {
		if !strings.Contains(cmd, "--scanners misconfig") {
			t.Errorf("the %s command does not select the misconfig scanner:\n%s", name, cmd)
		}
	}
	if !strings.Contains(dir, " fs ") || !strings.Contains(img, " image ") {
		t.Errorf("the subcommands are wrong:\n%s\n%s", dir, img)
	}
}

// The misconfig scan carries the same optional flags as the vulnerability one,
// and it used to be the scan with no display counterpart at all (D19).
func TestOptionalMisconfigFlagsAppearOnlyWhenAsked(t *testing.T) {
	bare := GetTrivyMisconfigCommand("/repos", TargetDirectory, ToolSourceBinary, "", "", false)
	if strings.Contains(bare, "--server") || strings.Contains(bare, "--ignore-status") {
		t.Errorf("flags appeared that were not asked for:\n%s", bare)
	}

	full := GetTrivyMisconfigCommand("/repos", TargetDirectory, ToolSourceBinary, "", "https://trivy:4954", true)
	for _, want := range []string{"--server https://trivy:4954", "--ignore-status end_of_life"} {
		if !strings.Contains(full, want) {
			t.Errorf("the command is missing %q:\n%s", want, full)
		}
	}
}

// SBOM generation had no display counterpart either, so its command was never
// shown at all.
func TestTheSBOMCommandIsShownLikeTheOthers(t *testing.T) {
	cmd := GetSBOMCommand("/repos", TargetDirectory, ToolSourceBinary, "", "", "/out")

	for _, want := range []string{"trivy", "fs", "--format cyclonedx", "--output"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("the command is missing %q:\n%s", want, cmd)
		}
	}

	served := GetSBOMCommand("api:v1", TargetImage, ToolSourceDocker, "", "https://trivy:4954", "/out")
	if !strings.Contains(served, "--server https://trivy:4954") {
		t.Errorf("the server was not passed:\n%s", served)
	}
	// A server does the work, so there is no image to inspect through the socket.
	if strings.Contains(served, "/var/run/docker.sock") {
		t.Errorf("the socket was mounted although a server does the work:\n%s", served)
	}

	// The server reaches the binary form too — it is the same flag, and the
	// two forms are built by the same builder.
	binary := GetSBOMCommand("/repos", TargetDirectory, ToolSourceBinary, "", "https://trivy:4954", "")
	if !strings.Contains(binary, "--server https://trivy:4954") {
		t.Errorf("the server was not passed to the binary form:\n%s", binary)
	}
}

// An image has no directory to write beside, so with nowhere configured the
// SBOM lands in the working directory — which has to be mounted for the
// container to reach it.
func TestAnImageSBOMWithNoOutputDirectoryUsesTheWorkingDirectory(t *testing.T) {
	tc, hostPath, err := sbomArgs("api:v1", TargetImage, ToolSourceDocker, "", "", "")
	if err != nil {
		t.Fatalf("building failed: %v", err)
	}

	if hostPath != "sbom-api_v1.json" {
		t.Errorf("host path = %q, want it relative to the working directory", hostPath)
	}
	if !strings.Contains(tc.String(), "-v .:"+containerOutputPath) {
		t.Errorf("the working directory was not mounted:\n%s", tc)
	}
}

// An image reference is not a legal filename, so the SBOM name is sanitised
// while staying identifiable.
func TestTheSBOMFileNameIsDerivedFromTheTarget(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		targetType TargetType
		want       string
	}{
		{"a directory", "/repos/devdesk", TargetDirectory, "sbom-report.json"},
		{"a plain image", "api:v1", TargetImage, "sbom-api_v1.json"},
		{"a registry-qualified image", "registry.example.com/team/api:v1", TargetImage,
			"sbom-registry.example.com_team_api_v1.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sbomFileName(tt.target, tt.targetType); got != tt.want {
				t.Errorf("sbomFileName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The SBOM has to land where the caller will look for it, which is not the path
// Trivy is given when it runs in a container.
func TestTheSBOMPathIsTheHostPathNotTheContainerPath(t *testing.T) {
	tests := []struct {
		name       string
		targetType TargetType
		target     string
		outputDir  string
		want       string
	}{
		{"beside the scanned directory", TargetDirectory, "/repos/devdesk", "", "/repos/devdesk/sbom-report.json"},
		{"in the configured directory", TargetDirectory, "/repos/devdesk", "/out", "/out/sbom-report.json"},
		{"an image with a configured directory", TargetImage, "api:v1", "/out", "/out/sbom-api_v1.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc, hostPath, err := sbomArgs(tt.target, tt.targetType, ToolSourceDocker, "", "", tt.outputDir)
			if err != nil {
				t.Fatalf("building failed: %v", err)
			}
			if filepath.ToSlash(hostPath) != tt.want {
				t.Errorf("host path = %q, want %q", hostPath, tt.want)
			}
			if strings.Contains(tc.String(), " --output "+hostPath+" ") {
				t.Errorf("trivy was given the host path rather than the mount:\n%s", tc)
			}
		})
	}
}

// A directory SBOM has to be written back into a mount that is not read-only,
// unlike every other directory scan.
func TestTheSBOMOutputMountIsWritable(t *testing.T) {
	tc, _, err := sbomArgs("/repos/devdesk", TargetDirectory, ToolSourceDocker, "", "", "")
	if err != nil {
		t.Fatalf("building failed: %v", err)
	}

	if strings.Contains(tc.String(), "/repos/devdesk:/scan:ro") {
		t.Errorf("the SBOM cannot be written to a read-only mount:\n%s", tc)
	}
	if !strings.Contains(tc.String(), "-v /repos/devdesk:/scan") {
		t.Errorf("the target was not mounted:\n%s", tc)
	}
}

// ── Gitleaks ─────────────────────────────────────────────────────────────────

// Gitleaks writes its report to a path, so the command redirects it to stdout —
// which is a different pseudo-file inside a container than outside.
func TestGitleaksReportsToStdout(t *testing.T) {
	binary := GetGitleaksCommand("/repos/devdesk", ToolSourceBinary, "", false, "")
	if !strings.Contains(binary, "--report-path /dev/stdout") {
		t.Errorf("the binary command does not capture the report:\n%s", binary)
	}

	docker := GetGitleaksCommand("/repos/devdesk", ToolSourceDocker, "", false, "")
	if !strings.Contains(docker, "--report-path /dev/fd/1") {
		t.Errorf("the docker command does not capture the report:\n%s", docker)
	}
}

// The repository's own .gitleaksignore has to be honoured, or every finding the
// user has already dismissed comes back on the next scan.
func TestTheIgnoreFileIsAlwaysPassed(t *testing.T) {
	binary := GetGitleaksCommand("/repos/devdesk", ToolSourceBinary, "", false, "")
	if !strings.Contains(binary, "--gitleaks-ignore-path /repos/devdesk") {
		t.Errorf("the ignore path was not passed:\n%s", binary)
	}

	docker := GetGitleaksCommand("/repos/devdesk", ToolSourceDocker, "", false, "")
	if !strings.Contains(docker, "--gitleaks-ignore-path /scan") {
		t.Errorf("the ignore path was not pointed at the mount:\n%s", docker)
	}
}

// History scanning is the expensive mode, so --no-git is the default and the
// flag is dropped when the user asks for history.
func TestHistoryIsOptedIntoByDroppingNoGit(t *testing.T) {
	shallow := GetGitleaksCommand("/repos", ToolSourceBinary, "", false, "")
	if !strings.Contains(shallow, "--no-git") {
		t.Errorf("the default scan did not skip git history:\n%s", shallow)
	}

	full := GetGitleaksCommand("/repos", ToolSourceBinary, "", true, "")
	if strings.Contains(full, "--no-git") {
		t.Errorf("history was asked for but git was still skipped:\n%s", full)
	}
}

func TestACustomGitleaksConfigIsPassedThrough(t *testing.T) {
	without := GetGitleaksCommand("/repos", ToolSourceBinary, "", false, "")
	if strings.Contains(without, "--config") {
		t.Errorf("a config flag appeared with none configured:\n%s", without)
	}

	with := GetGitleaksCommand("/repos", ToolSourceBinary, "", false, "/etc/gitleaks.toml")
	if !strings.Contains(with, "--config /etc/gitleaks.toml") {
		t.Errorf("the configured rules file was not passed:\n%s", with)
	}
}

func TestGitleaksDockerModeMountsTheTargetReadOnly(t *testing.T) {
	cmd := GetGitleaksCommand("/repos/devdesk", ToolSourceDocker, "", false, "")

	if !strings.Contains(cmd, "-v /repos/devdesk:/scan:ro") {
		t.Errorf("the target is not mounted read-only:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--source /scan") {
		t.Errorf("gitleaks was pointed at the host path rather than the mount:\n%s", cmd)
	}
	if !strings.Contains(cmd, DefaultGitleaksImage) {
		t.Errorf("no image named; the default should be filled in:\n%s", cmd)
	}
}
