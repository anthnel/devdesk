package scan

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A candidate base image is read from its registry: the flag says so, and in
// container mode no engine socket is mounted, since nothing would use it.
func TestARemoteImageScanReadsFromTheRegistry(t *testing.T) {
	tc, err := trivyRemoteImageArgs("alpine:3.21", ToolSpec{Source: ToolSourceBinary}, "", false, false)
	if err != nil {
		t.Fatalf("building failed: %v", err)
	}
	got := tc.String()
	for _, want := range []string{"image", "--scanners vuln", "--image-src remote", "alpine:3.21"} {
		if !strings.Contains(got, want) {
			t.Errorf("command %q lacks %q", got, want)
		}
	}
}

func TestARemoteImageScanInAContainerMountsNoSocket(t *testing.T) {
	spec := ToolSpec{Source: ToolSourceContainer, Image: DefaultTrivyImage, HostSocket: "/var/run/docker.sock"}

	remote, err := trivyRemoteImageArgs("alpine:3.21", spec, "", false, false)
	if err != nil {
		t.Fatalf("building failed: %v", err)
	}
	if strings.Contains(remote.String(), "docker.sock") {
		t.Errorf("remote scan mounts the socket: %s", remote.String())
	}

	// The ordinary image scan never asks for a remote source.
	local, err := trivyArgs("alpine:3.21", TargetImage, false, spec, "", false, false)
	if err != nil {
		t.Fatalf("building failed: %v", err)
	}
	if strings.Contains(local.String(), "--image-src") {
		t.Errorf("a local image scan asks for a remote source: %s", local.String())
	}
}

func TestARemoteImageScanHonoursTheScanOptions(t *testing.T) {
	tc, err := trivyRemoteImageArgs("alpine:3.21", ToolSpec{Source: ToolSourceBinary}, "https://trivy:4954", true, true)
	if err != nil {
		t.Fatalf("building failed: %v", err)
	}
	got := tc.String()
	for _, want := range []string{"--server https://trivy:4954", "--ignore-unfixed", "--ignore-status end_of_life"} {
		if !strings.Contains(got, want) {
			t.Errorf("command %q lacks %q", got, want)
		}
	}
}

func TestScanRemoteImageCountsWhatTrivyFound(t *testing.T) {
	report := trivyReport(t, TrivyResult{
		Target: "alpine 3.21", Class: ClassOSPackages, Type: "alpine",
		Vulnerabilities: []TrivyVulnerability{
			{VulnerabilityID: "CVE-1", Severity: "CRITICAL", PkgName: "openssl", FixedVersion: "3.1.4"},
			{VulnerabilityID: "CVE-2", Severity: "HIGH", PkgName: "musl"},
			{VulnerabilityID: "CVE-3", Severity: "HIGH", PkgName: "zlib"},
		},
	})
	r := answering(t, report, nil)

	result, err := newScannerWithDeps(ScanOptions{}, everyTool()).ScanRemoteImage(context.Background(), "alpine:3.21")
	if err != nil {
		t.Fatalf("ScanRemoteImage failed: %v", err)
	}
	if result.Counts.Critical != 1 || result.Counts.High != 2 {
		t.Errorf("counts = %+v, want 1 critical and 2 high", result.Counts)
	}
	if result.Target != "alpine:3.21" || result.TargetType != TargetImage {
		t.Errorf("target = %q (%s)", result.Target, result.TargetType)
	}
	if r.count() != 1 {
		t.Errorf("ran %d commands, want the vulnerability stage alone", r.count())
	}
}

// Whatever stages the user enabled for their own scans, a candidate gets the
// vulnerability stage and nothing else.
func TestScanRemoteImageRunsNoOtherStage(t *testing.T) {
	r := answering(t, `{"Results":[]}`, nil)
	if _, err := newScannerWithDeps(everyStage(), everyTool()).ScanRemoteImage(context.Background(), "alpine:3.21"); err != nil {
		t.Fatalf("ScanRemoteImage failed: %v", err)
	}
	if got := r.commands(); len(got) != 1 || !strings.Contains(got[0], "--scanners vuln") {
		t.Errorf("commands = %v, want one vulnerability scan", got)
	}
}

func TestScanRemoteImageWithoutTrivy(t *testing.T) {
	_, err := newScannerWithDeps(ScanOptions{}, DependencyStatus{}).ScanRemoteImage(context.Background(), "alpine:3.21")
	if !errors.Is(err, ErrTrivyUnavailable) {
		t.Errorf("err = %v, want ErrTrivyUnavailable", err)
	}
}

func TestScanRemoteImageReportsAFailedScanAsAnError(t *testing.T) {
	answering(t, "", &exitError{Code: 2, Stderr: "FATAL"})
	if _, err := newScannerWithDeps(ScanOptions{}, everyTool()).ScanRemoteImage(context.Background(), "alpine:3.21"); err == nil {
		t.Error("a scan that produced no report succeeded")
	}
}
