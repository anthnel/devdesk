package scan

import (
	"strings"
	"testing"
)

// The displayed command is what the user copies when a scan fails and they want
// to reproduce it by hand, so it has to match what was run — see D19.

// ── Trivy ────────────────────────────────────────────────────────────────────

func TestTrivyScansADirectoryAsAFilesystem(t *testing.T) {
	cmd := GetTrivyCommand("/repos/devdesk", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, "", false, false)

	for _, want := range []string{"trivy", "fs", "--format json", "--scanners vuln", "/repos/devdesk"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("the command is missing %q:\n%s", want, cmd)
		}
	}
}

// License mode is the same fs scan with a different scanner; asking for both at
// once would double the run time for one of them.
func TestLicenseModeSwapsTheScanner(t *testing.T) {
	cmd := GetTrivyCommand("/repos/devdesk", TargetDirectory, true, ToolSpec{Source: ToolSourceBinary}, "", false, false)

	if !strings.Contains(cmd, "--scanners license") {
		t.Errorf("license mode did not select the license scanner:\n%s", cmd)
	}
	if strings.Contains(cmd, "--scanners vuln") {
		t.Errorf("license mode also asked for vulnerabilities:\n%s", cmd)
	}
}

func TestTrivyScansAnImageByName(t *testing.T) {
	cmd := GetTrivyCommand("api:v1", TargetImage, false, ToolSpec{Source: ToolSourceBinary}, "", false, false)

	if !strings.Contains(cmd, "image") || !strings.Contains(cmd, "api:v1") {
		t.Errorf("the image command is wrong:\n%s", cmd)
	}
	// Trivy's default scanners for an image are "vuln,secret". Leaving the flag
	// off ran a secret scan whose output nothing read, and would now report each
	// secret twice, the secret stage having been given its own invocation.
	if !strings.Contains(cmd, "--scanners vuln") {
		t.Errorf("an image scan did not limit itself to vulnerabilities:\n%s", cmd)
	}
}

func TestTrivyScansForSecrets(t *testing.T) {
	for _, tc := range []struct {
		name       string
		targetType TargetType
		subcommand string
	}{
		{"a directory", TargetDirectory, "fs"},
		{"an image", TargetImage, "image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := GetTrivySecretCommand("target", tc.targetType, ToolSpec{Source: ToolSourceBinary}, "")

			if !strings.Contains(cmd, tc.subcommand) {
				t.Errorf("the %s secret scan does not use %q:\n%s", tc.name, tc.subcommand, cmd)
			}
			if !strings.Contains(cmd, "--scanners secret") {
				t.Errorf("the secret scanner was not selected:\n%s", cmd)
			}
			// end_of_life describes a package's support window and says nothing
			// about a secret sitting in a file.
			if strings.Contains(cmd, "--ignore-status") {
				t.Errorf("a secret scan carried a package-status filter:\n%s", cmd)
			}
		})
	}
}

// In Docker mode the target is mounted read-only and the container sees it at a
// fixed path, so the argument passed to trivy is /scan, not the host path.
func TestDockerModeMountsTheDirectoryReadOnly(t *testing.T) {
	cmd := GetTrivyCommand("/repos/devdesk", TargetDirectory, false, ToolSpec{Source: ToolSourceContainer}, "", false, false)

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

// Scanning an image from a container needs the host engine's socket — unless a
// Trivy server is doing the work, in which case handing the socket over would
// be a needless grant.
//
// The socket comes from the spec rather than from a constant (§3.67): the
// container side stays /var/run/docker.sock, because that is where Trivy looks,
// but the host side is whatever the engine exposes.
func TestTheEngineSocketIsMountedOnlyWhenItIsNeeded(t *testing.T) {
	withSocket := ToolSpec{Source: ToolSourceContainer, HostSocket: "/var/run/docker.sock"}

	local := GetTrivyCommand("api:v1", TargetImage, false, withSocket, "", false, false)
	if !strings.Contains(local, "/var/run/docker.sock:/var/run/docker.sock:ro") {
		t.Errorf("an image scan without a server has no socket to inspect the image with:\n%s", local)
	}

	served := GetTrivyCommand("api:v1", TargetImage, false, withSocket, "https://trivy:4954", false, false)
	if strings.Contains(served, "docker.sock") {
		t.Errorf("the socket was mounted although a server does the work:\n%s", served)
	}
}

// A podman socket is somewhere else, and the container side does not follow it:
// Trivy looks at /var/run/docker.sock whatever ran it.
func TestAnEngineSocketElsewhereIsMountedWhereTrivyLooks(t *testing.T) {
	spec := ToolSpec{Source: ToolSourceContainer, HostSocket: "/run/user/1000/podman/podman.sock"}

	cmd := GetTrivyCommand("api:v1", TargetImage, false, spec, "", false, false)

	if !strings.Contains(cmd, "/run/user/1000/podman/podman.sock:/var/run/docker.sock:ro") {
		t.Errorf("the podman socket was not mounted where Trivy looks for one:\n%s", cmd)
	}
}

// Rootless podman has no socket unless `podman system service` is running. No
// path is invented for it: an absent socket produces no mount, so the refusal
// stays something DependencyStatus.ImageScanBlocked can explain before the
// keypress (Rule 130) rather than a container that fails on startup.
func TestNoSocketIsMountedWhenTheEngineHasNone(t *testing.T) {
	cmd := GetTrivyCommand("api:v1", TargetImage, false, ToolSpec{Source: ToolSourceContainer}, "", false, false)

	if strings.Contains(cmd, ".sock") {
		t.Errorf("a socket was mounted although the engine exposes none:\n%s", cmd)
	}
}

func TestAConfiguredImageOverridesTheDefault(t *testing.T) {
	cmd := GetTrivyCommand("/repos", TargetDirectory, false, ToolSpec{Source: ToolSourceContainer, Image: "mirror.local/trivy:0.50"}, "", false, false)

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
		{"server off", GetTrivyCommand("/r", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, "", false, false), "--server", false},
		{"server on", GetTrivyCommand("/r", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, "https://trivy:4954", false, false), "--server https://trivy:4954", true},
		{"ignore-unfixed off", GetTrivyCommand("/r", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, "", false, false), "--ignore-unfixed", false},
		{"ignore-unfixed on", GetTrivyCommand("/r", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, "", true, false), "--ignore-unfixed", true},
		{"ignore-eol off", GetTrivyCommand("/r", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, "", false, false), "--ignore-status", false},
		{"ignore-eol on", GetTrivyCommand("/r", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, "", false, true), "--ignore-status end_of_life", true},
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
	if got := GetTrivyCommand("x", TargetType("registry"), false, ToolSpec{Source: ToolSourceBinary}, "", false, false); got != "" {
		t.Errorf("GetTrivyCommand for an unknown target type = %q, want empty", got)
	}
	if got := GetTrivyMisconfigCommand("x", TargetType("registry"), ToolSpec{Source: ToolSourceBinary}, "", false); got != "" {
		t.Errorf("GetTrivyMisconfigCommand for an unknown target type = %q, want empty", got)
	}
}

// D19 was the two builders drifting apart. They are now one, so the shown
// command is the executed one by construction — this pins that.
func TestTheShownCommandIsTheExecutedOne(t *testing.T) {
	tc, err := trivyArgs("/repos", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, "https://trivy:4954", true, true)
	if err != nil {
		t.Fatalf("building failed: %v", err)
	}

	shown := GetTrivyCommand("/repos", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, "https://trivy:4954", true, true)
	if shown != tc.String() {
		t.Errorf("shown:\n%s\nexecuted:\n%s", shown, tc.String())
	}
}

// ── Misconfiguration ─────────────────────────────────────────────────────────

// Misconfiguration scanning reads configuration files, so unlike the license
// scanner it applies to images as well as directories.
func TestMisconfigScanningAppliesToBothTargetTypes(t *testing.T) {
	dir := GetTrivyMisconfigCommand("/repos", TargetDirectory, ToolSpec{Source: ToolSourceBinary}, "", false)
	img := GetTrivyMisconfigCommand("api:v1", TargetImage, ToolSpec{Source: ToolSourceBinary}, "", false)

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
	bare := GetTrivyMisconfigCommand("/repos", TargetDirectory, ToolSpec{Source: ToolSourceBinary}, "", false)
	if strings.Contains(bare, "--server") || strings.Contains(bare, "--ignore-status") {
		t.Errorf("flags appeared that were not asked for:\n%s", bare)
	}

	full := GetTrivyMisconfigCommand("/repos", TargetDirectory, ToolSpec{Source: ToolSourceBinary}, "https://trivy:4954", true)
	for _, want := range []string{"--server https://trivy:4954", "--ignore-status end_of_life"} {
		if !strings.Contains(full, want) {
			t.Errorf("the command is missing %q:\n%s", want, full)
		}
	}
}

// ── Gitleaks ─────────────────────────────────────────────────────────────────

// Gitleaks writes its report to a path, so the command redirects it to stdout —
// which is a different pseudo-file inside a container than outside.
func TestGitleaksReportsToStdout(t *testing.T) {
	binary := GetGitleaksCommand("/repos/devdesk", ToolSpec{Source: ToolSourceBinary}, false, "")
	if !strings.Contains(binary, "--report-path /dev/stdout") {
		t.Errorf("the binary command does not capture the report:\n%s", binary)
	}

	docker := GetGitleaksCommand("/repos/devdesk", ToolSpec{Source: ToolSourceContainer}, false, "")
	if !strings.Contains(docker, "--report-path /dev/fd/1") {
		t.Errorf("the docker command does not capture the report:\n%s", docker)
	}
}

// The repository's own .gitleaksignore has to be honoured, or every finding the
// user has already dismissed comes back on the next scan.
func TestTheIgnoreFileIsAlwaysPassed(t *testing.T) {
	binary := GetGitleaksCommand("/repos/devdesk", ToolSpec{Source: ToolSourceBinary}, false, "")
	if !strings.Contains(binary, "--gitleaks-ignore-path /repos/devdesk") {
		t.Errorf("the ignore path was not passed:\n%s", binary)
	}

	docker := GetGitleaksCommand("/repos/devdesk", ToolSpec{Source: ToolSourceContainer}, false, "")
	if !strings.Contains(docker, "--gitleaks-ignore-path /scan") {
		t.Errorf("the ignore path was not pointed at the mount:\n%s", docker)
	}
}

// History scanning is the expensive mode, so --no-git is the default and the
// flag is dropped when the user asks for history.
func TestHistoryIsOptedIntoByDroppingNoGit(t *testing.T) {
	shallow := GetGitleaksCommand("/repos", ToolSpec{Source: ToolSourceBinary}, false, "")
	if !strings.Contains(shallow, "--no-git") {
		t.Errorf("the default scan did not skip git history:\n%s", shallow)
	}

	full := GetGitleaksCommand("/repos", ToolSpec{Source: ToolSourceBinary}, true, "")
	if strings.Contains(full, "--no-git") {
		t.Errorf("history was asked for but git was still skipped:\n%s", full)
	}
}

func TestACustomGitleaksConfigIsPassedThrough(t *testing.T) {
	for _, source := range []ToolSource{ToolSourceBinary, ToolSourceContainer} {
		without := GetGitleaksCommand("/repos", ToolSpec{Source: source}, false, "")
		if strings.Contains(without, "--config") {
			t.Errorf("%s: a config flag appeared with none configured:\n%s", source, without)
		}
	}

	binary := GetGitleaksCommand("/repos", ToolSpec{Source: ToolSourceBinary}, false, "/etc/gitleaks.toml")
	if !strings.Contains(binary, "--config /etc/gitleaks.toml") {
		t.Errorf("the configured rules file was not passed:\n%s", binary)
	}
}

// D56: this test existed and exercised the binary mode only, and it checked
// that the flag was **present** rather than that the path was **reachable** —
// "--config /etc/gitleaks.toml" passes on both sides and means the same thing
// on neither. In Docker mode the host path went straight into the container,
// where the file is not, and gitleaks died before reading a byte.
func TestGitleaksDockerModeMountsTheConfigAndPointsAtTheMount(t *testing.T) {
	cmd := GetGitleaksCommand("/repos", ToolSpec{Source: ToolSourceContainer}, false, "/home/me/rules.toml")

	if !strings.Contains(cmd, "-v /home/me/rules.toml:"+gitleaksConfigMount+":ro") {
		t.Errorf("the rules file is not mounted:\n%s", cmd)
	}
	if !strings.Contains(cmd, "--config "+gitleaksConfigMount) {
		t.Errorf("gitleaks was pointed at the host path rather than the mount:\n%s", cmd)
	}
	if strings.Contains(cmd, "--config /home/me/rules.toml") {
		t.Errorf("the host path reached the container:\n%s", cmd)
	}

	// Everything after the image name is gitleaks' own argv, so a -v placed
	// there would be an argument to the scanner rather than to docker.
	if strings.Index(cmd, "-v /home/me") > strings.Index(cmd, DefaultGitleaksImage) {
		t.Errorf("the mount was declared after the image name:\n%s", cmd)
	}
}

// The mount point has to be somewhere nothing else can be. The target lands at
// /scan, so no file of the scanned repository shares the container root with
// it — that is the whole reason the name is what it is.
func TestTheConfigMountCannotCollideWithTheScannedTarget(t *testing.T) {
	if strings.HasPrefix(gitleaksConfigMount, containerScanPath+"/") {
		t.Errorf("the config is mounted inside the target: %q", gitleaksConfigMount)
	}
}

func TestGitleaksDockerModeMountsTheTargetReadOnly(t *testing.T) {
	cmd := GetGitleaksCommand("/repos/devdesk", ToolSpec{Source: ToolSourceContainer}, false, "")

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

// ── The Trivy server address ─────────────────────────────────────────────────

// Trivy parses this as a URL and fails the whole scan when it cannot, with a
// message naming neither DevDesk nor the setting the value came from:
//
//	FATAL flag error: unable to convert flags to options:
//	invalid server address format: parse ":": missing protocol scheme
//
// A bare ":" is one keystroke away, because a ":" typed while the field has
// focus is a character rather than the command line (§3.7), and the field is
// persisted on every option toggle — so it breaks every later scan from every
// view until it is found in the config file.
func TestAnUnusableTrivyServerIsRefusedBeforeTrivySeesIt(t *testing.T) {
	cases := []string{
		":",
		"trivy-server",     // no scheme
		"https://",         // no host
		"http://:",         // neither
		"not a url at all", // spaces
	}
	for _, addr := range cases {
		_, err := trivyArgs("/r", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, addr, false, false)
		if err == nil {
			t.Errorf("trivyArgs accepted %q as a server address", addr)
			continue
		}
		if !strings.Contains(err.Error(), "scan.trivy_server") {
			t.Errorf("err = %v, want it to name the setting to fix", err)
		}
	}
}

// Surrounding space is not an address. It has to mean "unset" rather than
// become something Trivy refuses.
func TestAnAllSpaceTrivyServerMeansClientServerModeIsOff(t *testing.T) {
	tc, err := trivyArgs("/r", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, "   ", false, false)
	if err != nil {
		t.Fatalf("an all-space address was treated as a failure: %v", err)
	}
	if strings.Contains(tc.String(), "--server") {
		t.Errorf("the flag was passed anyway:\n%s", tc)
	}
}

func TestAUsableTrivyServerIsTrimmedAndPassed(t *testing.T) {
	tc, err := trivyArgs("/r", TargetDirectory, false, ToolSpec{Source: ToolSourceBinary}, "  https://trivy:4954  ", false, false)
	if err != nil {
		t.Fatalf("trivyArgs: %v", err)
	}
	if !strings.Contains(tc.String(), "--server https://trivy:4954") {
		t.Errorf("the address was not trimmed before being passed:\n%s", tc)
	}
}

// Every builder that takes the address has to refuse it the same way, or the
// scan fails on whichever stage was not checked.
func TestEveryTrivyBuilderRefusesAnUnusableServer(t *testing.T) {
	if _, err := trivyMisconfigArgs("/r", TargetDirectory, ToolSpec{Source: ToolSourceBinary}, ":", false); err == nil {
		t.Error("the misconfiguration builder accepted an unusable server address")
	}
	if _, err := trivySecretArgs("/r", TargetDirectory, ToolSpec{Source: ToolSourceBinary}, ":"); err == nil {
		t.Error("the secret builder accepted an unusable server address")
	}
}

// The form uses this to refuse the value while the user is still looking at the
// field, so it has to agree with what the builders do.
func TestValidateTrivyServerAgreesWithTheBuilders(t *testing.T) {
	if err := ValidateTrivyServer(":"); err == nil {
		t.Error("ValidateTrivyServer accepted an address the builders refuse")
	}
	for _, ok := range []string{"", "   ", "https://trivy:4954", "http://127.0.0.1:4954"} {
		if err := ValidateTrivyServer(ok); err != nil {
			t.Errorf("ValidateTrivyServer(%q) = %v, want it accepted", ok, err)
		}
	}
}
