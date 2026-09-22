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

// misconfLines is misconf with the line range a real Trivy report carries for
// a misconfiguration — required by any rule whose fix has to find the
// offending instruction rather than the Dockerfile's structure alone.
func misconfLines(id, file string, line, endLine int) scan.Finding {
	f := misconf(id, file)
	f.Line, f.EndLine = line, endLine
	return f
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
	for _, id := range []string{"AVD-DS-0029", "DS029", "ds029", "DS0029", "avd-ds-0029"} {
		if _, ok := FixFor(misconf(id, "Dockerfile")); !ok {
			t.Errorf("%q did not find the apt-get rule", id)
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

// AVD-DS-0029: 'apt-get' missing '--no-install-recommends'.

func TestTheFlagIsAddedRightAfterInstall(t *testing.T) {
	src := "FROM debian:12\nRUN apt-get install -y curl\n"
	got, reason := apply(t, src, misconfLines("AVD-DS-0029", "Dockerfile", 2, 2))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if !strings.Contains(got, "apt-get install --no-install-recommends -y curl") {
		t.Errorf("the flag was not added where expected:\n%s", got)
	}
}

// The flag satisfies apt-get wherever it sits relative to the rest of the
// command's own flags, so a file that already has it — in any of the
// positions Trivy itself accepts — must not be rewritten a second time.
func TestAptGetAlreadyFixedIsDeclined(t *testing.T) {
	for _, src := range []string{
		"FROM debian:12\nRUN apt-get install --no-install-recommends -y curl\n",
		"FROM debian:12\nRUN apt-get install -y --no-install-recommends curl\n",
		"FROM debian:12\nRUN apt-get install -y curl --no-install-recommends\n",
	} {
		_, reason := apply(t, src, misconfLines("AVD-DS-0029", "Dockerfile", 2, 2))
		if reason != ReasonAlreadyFixed {
			t.Errorf("reason = %q, want %q for:\n%s", reason, ReasonAlreadyFixed, src)
		}
	}
}

// A RUN Trivy reports as one multi-line instruction (backslash continuations)
// is read from its first line to its last, not just the line the flag lands
// on.
func TestAMultiLineRunGetsTheFlag(t *testing.T) {
	src := "FROM debian:12\n" +
		"RUN apt-get update && apt-get install -y \\\n" +
		"    curl \\\n" +
		"    git \\\n" +
		"    && rm -rf /var/lib/apt/lists/*\n"
	got, reason := apply(t, src, misconfLines("AVD-DS-0029", "Dockerfile", 2, 5))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if !strings.Contains(got, "apt-get install --no-install-recommends -y \\\n") {
		t.Errorf("the flag was not added to the multi-line install:\n%s", got)
	}
}

// A RUN chaining two apt-get installs with `&&` is fixed per invocation: the
// one that already has the flag is not what makes the other one "fixed".
func TestEachChainedInstallIsCheckedOnItsOwn(t *testing.T) {
	src := "FROM debian:12\nRUN apt-get install -y curl && apt-get install --no-install-recommends -y git\n"
	got, reason := apply(t, src, misconfLines("AVD-DS-0029", "Dockerfile", 2, 2))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if !strings.Contains(got, "apt-get install --no-install-recommends -y curl") {
		t.Errorf("the first, unfixed install did not get the flag:\n%s", got)
	}
	if strings.Count(got, recommendsFlag) != 2 {
		t.Errorf("the already-fixed install was rewritten:\n%s", got)
	}
}

// The reported lines no longer holding an apt-get install is what a stale
// finding — the file changed since the scan — looks like, and it is declined
// rather than guessed at.
func TestNoAptGetInstallInTheReportedLinesIsDeclined(t *testing.T) {
	src := "FROM debian:12\nRUN echo hi\n"
	_, reason := apply(t, src, misconfLines("AVD-DS-0029", "Dockerfile", 2, 2))
	if reason != ReasonNoAptGetInstall {
		t.Errorf("reason = %q, want %q", reason, ReasonNoAptGetInstall)
	}
}

// A finding cached before EndLine was recorded (§ EndLine's own doc comment)
// still names a Line, and a single apt-get install is one physical line: the
// fix must not decline just because EndLine reads zero.
func TestAZeroEndLineFallsBackToTheSingleLine(t *testing.T) {
	src := "FROM debian:12\nRUN apt-get install -y curl\n"
	got, reason := apply(t, src, misconfLines("AVD-DS-0029", "Dockerfile", 2, 0))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if !strings.Contains(got, "apt-get install --no-install-recommends -y curl") {
		t.Errorf("the flag was not added:\n%s", got)
	}
}

func TestAptGetFixDeclinesOnANonDockerfile(t *testing.T) {
	src := "user: root\n"
	_, reason := apply(t, src, misconfLines("AVD-DS-0029", "k8s/deploy.yaml", 1, 1))
	if reason != ReasonNotADockerfile {
		t.Errorf("reason = %q, want %q", reason, ReasonNotADockerfile)
	}
}

// AVD-DS-0021: 'apt-get' missing '-y'.

func TestYIsAddedRightAfterInstall(t *testing.T) {
	src := "FROM debian:12\nRUN apt-get install curl\n"
	got, reason := apply(t, src, misconfLines("AVD-DS-0021", "Dockerfile", 2, 2))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if !strings.Contains(got, "apt-get install -y curl") {
		t.Errorf("the flag was not added:\n%s", got)
	}
}

// apt-get accepts '-y' bundled into any short flag cluster, so an
// already-fixed check that only looked for the literal substring "-y" would
// miss '-qy' and add a redundant second flag.
func TestYFlagIsRecognizedInAnyForm(t *testing.T) {
	for _, src := range []string{
		"FROM debian:12\nRUN apt-get install -y curl\n",
		"FROM debian:12\nRUN apt-get install -qy curl\n",
		"FROM debian:12\nRUN apt-get install -yq curl\n",
		"FROM debian:12\nRUN apt-get install --yes curl\n",
		"FROM debian:12\nRUN apt-get install --assume-yes curl\n",
	} {
		_, reason := apply(t, src, misconfLines("AVD-DS-0021", "Dockerfile", 2, 2))
		if reason != ReasonAlreadyFixed {
			t.Errorf("reason = %q, want %q for:\n%s", reason, ReasonAlreadyFixed, src)
		}
	}
}

// AVD-DS-0025: 'apk add' is missing '--no-cache'.

func TestNoCacheIsAddedRightAfterApkAdd(t *testing.T) {
	src := "FROM alpine:3\nRUN apk add curl\n"
	got, reason := apply(t, src, misconfLines("AVD-DS-0025", "Dockerfile", 2, 2))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if !strings.Contains(got, "apk add --no-cache curl") {
		t.Errorf("the flag was not added:\n%s", got)
	}
}

func TestApkAlreadyFixedIsDeclined(t *testing.T) {
	src := "FROM alpine:3\nRUN apk add --no-cache curl\n"
	_, reason := apply(t, src, misconfLines("AVD-DS-0025", "Dockerfile", 2, 2))
	if reason != ReasonAlreadyFixed {
		t.Errorf("reason = %q, want %q", reason, ReasonAlreadyFixed)
	}
}

// AVD-DS-0015/0019/0020/0027: <package-manager> clean missing.

func TestPackageManagerCleanIsAppendedAtTheEnd(t *testing.T) {
	tests := []struct {
		id, src, want string
	}{
		{"AVD-DS-0015", "FROM centos:7\nRUN yum install -y curl\n", "yum install -y curl && yum clean all"},
		{"AVD-DS-0019", "FROM fedora:40\nRUN dnf install -y curl\n", "dnf install -y curl && dnf clean all"},
		{"AVD-DS-0027", "FROM registry.access.redhat.com/ubi9-micro\nRUN microdnf install curl\n", "microdnf install curl && microdnf clean all"},
		{"AVD-DS-0020", "FROM opensuse/leap\nRUN zypper install curl\n", "zypper install curl && zypper clean"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got, reason := apply(t, tt.src, misconfLines(tt.id, "Dockerfile", 2, 2))
			if reason != "" {
				t.Fatalf("declined: %s", reason)
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("the cleanup was not appended:\n%s", got)
			}
		})
	}
}

// The rule only counts a cleanup as satisfying it when it is the very last
// thing the RUN does — placed anywhere else, a re-scan would still flag it —
// so a RUN that already ends that way is left alone rather than doubled up.
func TestPackageManagerCleanAlreadyLastIsDeclined(t *testing.T) {
	src := "FROM centos:7\nRUN yum install -y curl && yum clean all\n"
	_, reason := apply(t, src, misconfLines("AVD-DS-0015", "Dockerfile", 2, 2))
	if reason != ReasonAlreadyFixed {
		t.Errorf("reason = %q, want %q", reason, ReasonAlreadyFixed)
	}
}

func TestPackageManagerCleanDeclinesWithNoMatchingInstall(t *testing.T) {
	src := "FROM centos:7\nRUN echo hi\n"
	_, reason := apply(t, src, misconfLines("AVD-DS-0015", "Dockerfile", 2, 2))
	if reason != ReasonNoYumInstall {
		t.Errorf("reason = %q, want %q", reason, ReasonNoYumInstall)
	}
}

// AVD-DS-0005: ADD instead of COPY.

func TestAddBecomesCopy(t *testing.T) {
	src := "FROM debian:12\nADD app.jar /app/app.jar\n"
	got, reason := apply(t, src, misconfLines("AVD-DS-0005", "Dockerfile", 2, 2))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if !strings.Contains(got, "COPY app.jar /app/app.jar") {
		t.Errorf("ADD was not rewritten to COPY:\n%s", got)
	}
}

// Trivy's own check already excludes a tar archive, a remote URL or a git
// source from AVD-DS-0005 — those are exactly the cases where ADD does
// something COPY cannot — so a finding reaching this fix should never
// exhibit one. The re-check exists for when the file changed since the scan.
func TestAddIsDeclinedWhenItExtractsOrFetches(t *testing.T) {
	for _, src := range []string{
		"FROM debian:12\nADD app.tar.gz /app/\n",
		"FROM debian:12\nADD https://example.com/app /app\n",
		"FROM debian:12\nADD git@example.com:org/repo.git /src\n",
	} {
		_, reason := apply(t, src, misconfLines("AVD-DS-0005", "Dockerfile", 2, 2))
		if reason != ReasonAddExtractsOrFetches {
			t.Errorf("reason = %q, want %q for:\n%s", reason, ReasonAddExtractsOrFetches, src)
		}
	}
}

func TestAddFixDeclinesWhenTheLineIsNotAnAdd(t *testing.T) {
	src := "FROM debian:12\nCOPY app /app\n"
	_, reason := apply(t, src, misconfLines("AVD-DS-0005", "Dockerfile", 2, 2))
	if reason != ReasonNotAnAddInstruction {
		t.Errorf("reason = %q, want %q", reason, ReasonNotAnAddInstruction)
	}
}

// AVD-DS-0011: COPY with more than two arguments not ending with slash.

func TestCopyGetsATrailingSlash(t *testing.T) {
	src := "FROM debian:12\nCOPY a b dest\n"
	got, reason := apply(t, src, misconfLines("AVD-DS-0011", "Dockerfile", 2, 2))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if !strings.Contains(got, "COPY a b dest/\n") {
		t.Errorf("the slash was not added:\n%s", got)
	}
}

func TestCopyWithTwoArgsIsDeclined(t *testing.T) {
	src := "FROM debian:12\nCOPY a dest\n"
	_, reason := apply(t, src, misconfLines("AVD-DS-0011", "Dockerfile", 2, 2))
	if reason != ReasonCopyHasTooFewArgs {
		t.Errorf("reason = %q, want %q", reason, ReasonCopyHasTooFewArgs)
	}
}

func TestCopyAlreadyEndingInSlashIsDeclined(t *testing.T) {
	src := "FROM debian:12\nCOPY a b dest/\n"
	_, reason := apply(t, src, misconfLines("AVD-DS-0011", "Dockerfile", 2, 2))
	if reason != ReasonAlreadyFixed {
		t.Errorf("reason = %q, want %q", reason, ReasonAlreadyFixed)
	}
}

// A --from/--chown flag is not one of the arguments the ">2" count is about,
// and must not be mistaken for the destination either.
func TestCopyFlagsAreNotCountedAsArguments(t *testing.T) {
	src := "FROM debian:12 AS build\nFROM debian:12\nCOPY --from=build a b dest\n"
	got, reason := apply(t, src, misconfLines("AVD-DS-0011", "Dockerfile", 3, 3))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if !strings.Contains(got, "COPY --from=build a b dest/\n") {
		t.Errorf("the slash was not added after the real destination:\n%s", got)
	}
}

// AVD-DS-0022: Deprecated MAINTAINER used.

func TestMaintainerBecomesALabel(t *testing.T) {
	src := "FROM debian:12\nMAINTAINER Jane Doe <jane@example.com>\n"
	got, reason := apply(t, src, misconfLines("AVD-DS-0022", "Dockerfile", 2, 2))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	if !strings.Contains(got, `LABEL maintainer="Jane Doe <jane@example.com>"`) {
		t.Errorf("MAINTAINER was not rewritten to LABEL:\n%s", got)
	}
}

func TestMaintainerFixDeclinesWhenTheLineIsNotAMaintainer(t *testing.T) {
	src := "FROM debian:12\nLABEL maintainer=\"Jane\"\n"
	_, reason := apply(t, src, misconfLines("AVD-DS-0022", "Dockerfile", 2, 2))
	if reason != ReasonNotAMaintainerInstruction {
		t.Errorf("reason = %q, want %q", reason, ReasonNotAMaintainerInstruction)
	}
}
