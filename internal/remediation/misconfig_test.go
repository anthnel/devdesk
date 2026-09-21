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

func TestTheUserGoesBeforeTheEntrypoint(t *testing.T) {
	src := "FROM alpine:3.21\nRUN apk add curl\nENTRYPOINT [\"/app\"]\n"
	got, reason := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if reason != "" {
		t.Fatalf("declined: %s", reason)
	}
	want := "FROM alpine:3.21\nRUN apk add curl\nUSER 1000:1000\nENTRYPOINT [\"/app\"]\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// After the CMD it would change nothing, which is the whole reason the position
// is computed rather than appended.
func TestTheUserGoesBeforeTheCmdNotAfterIt(t *testing.T) {
	src := "FROM alpine:3.21\nCMD [\"/app\"]\n"
	got, _ := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if !strings.Contains(got, "USER 1000:1000\nCMD") {
		t.Errorf("the USER did not land before the CMD:\n%s", got)
	}
}

// Only the final stage matters: that is the one that ships.
func TestTheUserGoesInTheFinalStage(t *testing.T) {
	src := "FROM golang:1 AS build\nRUN go build\nCMD [\"nope\"]\n\nFROM alpine:3.21\nCOPY --from=build /app /app\nENTRYPOINT [\"/app\"]\n"
	got, _ := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if strings.Contains(got, "USER 1000:1000\nCMD [\"nope\"]") {
		t.Errorf("the USER landed in the build stage:\n%s", got)
	}
	if !strings.Contains(got, "USER 1000:1000\nENTRYPOINT") {
		t.Errorf("the USER did not land in the final stage:\n%s", got)
	}
}

func TestWithNoCmdTheUserGoesAtTheEnd(t *testing.T) {
	src := "FROM alpine:3.21\nRUN apk add curl\n"
	got, _ := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if want := "FROM alpine:3.21\nRUN apk add curl\nUSER 1000:1000\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A file with no final newline would otherwise get the instruction glued to the
// last line, producing `RUN apk add curlUSER 1000:1000`.
func TestAFileWithNoFinalNewlineGetsOne(t *testing.T) {
	src := "FROM alpine:3.21\nRUN apk add curl"
	got, _ := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if want := "FROM alpine:3.21\nRUN apk add curl\nUSER 1000:1000\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The edit changes what it inserts and nothing else: comments, CRLF and the
// rest of the file are byte-for-byte the same. This is what internal/patch
// guarantees, checked here on the one caller that inserts rather than replaces.
func TestTheInsertionTouchesNothingElse(t *testing.T) {
	src := "# build it\r\nFROM alpine:3.21\r\nRUN apk add curl\r\nENTRYPOINT [\"/app\"]\r\n"
	got, _ := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	for _, want := range []string{"# build it\r\n", "FROM alpine:3.21\r\n", "RUN apk add curl\r\n", "ENTRYPOINT [\"/app\"]\r\n"} {
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
		{"the final stage already has one", "FROM alpine:3.21\nUSER 1000\nCMD [\"/app\"]\n", "Dockerfile", ReasonAlreadyFixed},
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

// A USER in an earlier stage does not satisfy the rule for the final one, so it
// must not be read as already fixed.
func TestAUserInTheBuildStageIsNotTheFinalStagesUser(t *testing.T) {
	src := "FROM golang:1 AS build\nUSER 1000\nRUN go build\n\nFROM alpine:3.21\nENTRYPOINT [\"/app\"]\n"
	got, reason := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if reason != "" {
		t.Fatalf("declined %q, but the final stage has no USER", reason)
	}
	if !strings.Contains(got, "USER 1000:1000\nENTRYPOINT") {
		t.Errorf("the final stage did not get a USER:\n%s", got)
	}
}

// A uid, not a name: `USER nonroot` is only valid in an image that declares the
// account, which the Dockerfile does not say and this package cannot find out.
func TestTheUserIsANumericID(t *testing.T) {
	src := "FROM alpine:3.21\nCMD [\"/app\"]\n"
	got, _ := apply(t, src, misconf("AVD-DS-0002", "Dockerfile"))
	if !strings.Contains(got, "USER 1000:1000") {
		t.Errorf("the inserted user is not a numeric id:\n%s", got)
	}
}
