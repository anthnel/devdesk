package remediation

import (
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/scan"
)

func misconf(id, file string) scan.Finding {
	return scan.Finding{ID: id, Source: scan.SourceTrivyMisconfig, File: file}
}

// apply is what the view will do: look the rule up, compute the edits, and
// rewrite. A test that asserted on the edits alone could pass while producing a
// file nobody would accept.
func apply(t *testing.T, src string, f scan.Finding) (string, string) {
	t.Helper()
	rule, ok := FixFor(f)
	if !ok {
		t.Fatalf("no rule for %q", f.ID)
	}
	edits, reason := rule.Fix([]byte(src), f)
	if reason != "" {
		return "", reason
	}
	out, err := patch.Rewrite([]byte(src), edits)
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	return string(out), ""
}

// Trivy spells the same rule two ways depending on which field it lands in.
// Matching one spelling only would leave the catalog silently inert.
func TestARuleIsFoundWhicheverWayItsIDIsSpelled(t *testing.T) {
	for _, id := range []string{"AVD-DS-0002", "DS002", "ds002", "DS0002", "avd-ds-0002"} {
		if _, ok := FixFor(misconf(id, "Dockerfile")); !ok {
			t.Errorf("%q did not find the root-user rule", id)
		}
	}
}

// The catalog answers for misconfigurations. A finding of another category
// reaching it is not an error — the tab is shared — it simply has no rule.
func TestOnlyAMisconfigurationGetsARule(t *testing.T) {
	cve := scan.Finding{ID: "AVD-DS-0002", Source: scan.SourceTrivy}
	if _, ok := FixFor(cve); ok {
		t.Error("a vulnerability was offered a misconfiguration fix")
	}
}

// A rule outside the catalog is the ordinary case, not a defect: it is what the
// MCP server and a calling agent handle.
func TestARuleOutsideTheCatalogHasNoFix(t *testing.T) {
	if _, ok := FixFor(misconf("AVD-DS-0026", "Dockerfile")); ok {
		t.Error("an uncatalogued rule was offered a fix")
	}
}

// The account is created before it is used. A bare `USER 10001` switches to a
// uid with no passwd entry — no name, no home, no shell — and everything that
// asks the system who it is degrades at run time, far from this edit.
func TestTheAccountIsCreatedNotJustSwitchedTo(t *testing.T) {
	src := "FROM debian:12\nENTRYPOINT [\"/app\"]\n"
	got, reason := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	for _, want := range []string{
		"ARG UID=10001",
		"RUN adduser \\",
		"--disabled-password",
		`--gecos ""`,
		`--home "/nonexistent"`,
		`--shell "/usr/sbin/nologin"`,
		"--no-create-home",
		`--uid "${UID}"`,
		"USER appuser",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the block lacks %q:\n%s", want, got)
		}
	}
	// The switch comes after the account exists, never before.
	if strings.Index(got, "USER appuser") < strings.Index(got, "RUN adduser") {
		t.Errorf("USER precedes the adduser that creates the account:\n%s", got)
	}
}

func TestTheBlockGoesBeforeTheEntrypoint(t *testing.T) {
	src := "FROM debian:12\nRUN apt-get install -y curl\nENTRYPOINT [\"/app\"]\n"
	got, _ := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if !strings.Contains(got, "USER appuser\nENTRYPOINT") {
		t.Errorf("the block did not land before the ENTRYPOINT:\n%s", got)
	}
}

// After the CMD it would change nothing, which is the whole reason the position
// is computed rather than appended.
func TestTheBlockGoesBeforeTheCmdNotAfterIt(t *testing.T) {
	src := "FROM debian:12\nCMD [\"/app\"]\n"
	got, _ := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if !strings.Contains(got, "USER appuser\nCMD") {
		t.Errorf("the block did not land before the CMD:\n%s", got)
	}
}

// Only the final stage matters: that is the one that ships.
func TestTheBlockGoesInTheFinalStage(t *testing.T) {
	src := "FROM golang:1 AS build\nRUN go build\nCMD [\"nope\"]\n\nFROM debian:12\nCOPY --from=build /app /app\nENTRYPOINT [\"/app\"]\n"
	got, _ := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if strings.Contains(got, "USER appuser\nCMD [\"nope\"]") {
		t.Errorf("the block landed in the build stage:\n%s", got)
	}
	if !strings.Contains(got, "USER appuser\nENTRYPOINT") {
		t.Errorf("the block did not land in the final stage:\n%s", got)
	}
}

func TestWithNoCmdTheBlockGoesAtTheEnd(t *testing.T) {
	src := "FROM debian:12\nRUN apt-get install -y curl\n"
	got, _ := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if !strings.HasSuffix(got, "USER appuser\n") {
		t.Errorf("the block is not at the end:\n%s", got)
	}
	if !strings.HasPrefix(got, "FROM debian:12\nRUN apt-get install -y curl\nARG UID=") {
		t.Errorf("the original lines did not survive:\n%s", got)
	}
}

// A file with no final newline would otherwise get the block glued to the last
// line, producing `RUN apt-get install -y curlARG UID=10001`.
func TestAFileWithNoFinalNewlineGetsOne(t *testing.T) {
	src := "FROM debian:12\nRUN apt-get install -y curl"
	got, _ := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if strings.Contains(got, "curlARG") {
		t.Errorf("the block was glued to the last line:\n%s", got)
	}
	if !strings.Contains(got, "curl\nARG UID=") {
		t.Errorf("a newline was not inserted before the block:\n%q", got)
	}
}

// The inserted block follows the file's own line endings. Mixing LF into a CRLF
// file is the kind of change that shows up as a whole-file diff in review.
func TestTheBlockFollowsTheFilesLineEndings(t *testing.T) {
	src := "# build it\r\nFROM debian:12\r\nENTRYPOINT [\"/app\"]\r\n"
	got, _ := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("the block introduced a bare LF into a CRLF file:\n%q", got)
	}
	for _, want := range []string{"# build it\r\n", "FROM debian:12\r\n", "ENTRYPOINT [\"/app\"]\r\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("the rewrite lost %q:\n%q", want, got)
		}
	}
}

// Declining is a first-class answer, not a failure. Each of these would be an
// edit the user has to second-guess, which is worse than being sent to the
// agent path.
func TestAFixDeclinesRatherThanApproximates(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		file   string
		reason string
	}{
		{"not a Dockerfile", "user: root\n", "k8s/deploy.yaml", ReasonNotADockerfile},
		{"no stage at all", "# nothing here\n", "Dockerfile", ReasonNoStage},
		{"the final stage already has one", "FROM debian:12\nUSER 1000\nCMD [\"/app\"]\n", "Dockerfile", ReasonAlreadyFixed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, reason := apply(t, tt.src, misconf("AVD-DS-0002", tt.file))
			if reason != tt.reason {
				t.Errorf("reason = %q, want %q", reason, tt.reason)
			}
		})
	}
}

// The adduser flags are Debian's. Emitting them against busybox or a shell-less
// image produces a Dockerfile that fails at build, which is worse than offering
// nothing — so a base this cannot identify is declined.
func TestABaseWhoseDistributionIsUnknownIsDeclined(t *testing.T) {
	for _, base := range []string{
		"alpine:3.21",
		"golang:1.23-alpine",               // official, but the tag says busybox
		"gcr.io/distroless/static:nonroot", // no shell to run adduser in
		"registry.example.com/team/base:1", // somebody's own build, says nothing
		"myapp:1",                          // not an official image
	} {
		t.Run(base, func(t *testing.T) {
			src := "FROM " + base + "\nENTRYPOINT [\"/app\"]\n"
			_, reason := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
			if reason != ReasonUnknownBaseFamily {
				t.Errorf("reason = %q, want %q", reason, ReasonUnknownBaseFamily)
			}
		})
	}
}

func TestADebianFamilyBaseIsAccepted(t *testing.T) {
	for _, base := range []string{"debian:12", "ubuntu:24.04", "node:22", "python:3.13-slim", "golang:1.23"} {
		t.Run(base, func(t *testing.T) {
			src := "FROM " + base + "\nENTRYPOINT [\"/app\"]\n"
			_, reason := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
			if reason != "" {
				t.Errorf("declined %q", reason)
			}
		})
	}
}

// A USER in an earlier stage does not satisfy the rule for the final one, so it
// must not be read as already fixed.
func TestAUserInTheBuildStageIsNotTheFinalStagesUser(t *testing.T) {
	src := "FROM golang:1 AS build\nUSER 1000\nRUN go build\n\nFROM debian:12\nENTRYPOINT [\"/app\"]\n"
	got, reason := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if reason != "" {
		t.Fatalf("declined %q, but the final stage has no USER", reason)
	}
	if !strings.Contains(got, "USER appuser\nENTRYPOINT") {
		t.Errorf("the final stage did not get the block:\n%s", got)
	}
}
