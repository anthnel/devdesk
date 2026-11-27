package enginecontract_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
	"github.com/anthnel/devdesk/internal/testutil/enginecontract"
)

// OC-09b2 (DEVDESK-807): Redaction.Anonymous reaches Record and Verify. A scenario that
// declares it records a message that names ids nobody owns (docker's refusal of a port), with
// stable placeholders, and the digest of the pinned image, which the argv holds, is public and
// stays: Record and Verify tell Redact about it the way they tell UnknownEngineIDs.

const anonImage = "docker.io/library/alpine@sha256:"

func anonExchange(endpoint, digest string) enginefixture.Exchange {
	return enginefixture.Exchange{
		Argv:   []string{"run", "--pull=never", "-d", "--name", "ddc-" + recRun + "-c2", anonImage + digest},
		Exit:   125,
		Stderr: []byte("driver failed programming external connectivity on endpoint ddc-" + recRun + "-c2 (" + endpoint + "): Bind failed\n"),
	}
}

func TestRecordKeepsTheDigestOfThePinnedImageAndAnonymizesTheEndpoint(t *testing.T) {
	digest, endpoint := recHex("the pinned image"), recHex("an endpoint")
	t.Setenv(enginecontract.ImageEnv, anonImage+digest)
	s, out := recordingSession(t, enginecontract.Record)

	rt := record(t, s, "launch", "port-taken", anonExchange(endpoint, digest), enginefixture.Redaction{RunID: recRun, Anonymous: 1})

	if !rt.quiet() {
		t.Fatalf("Record reported: %s", rt.text())
	}
	files, _ := filepath.Glob(filepath.Join(out, "launch", "*.cli"))
	if len(files) != 1 {
		t.Fatalf("%d fixture(s) written, want 1", len(files))
	}
	data, _ := os.ReadFile(files[0])
	text := string(data)
	if !strings.Contains(text, anonImage+digest) {
		t.Errorf("the digest of the pinned image was replaced: it is public")
	}
	if strings.Contains(text, endpoint) || !strings.Contains(text, enginefixture.PlaceholderID("anon-1")) {
		t.Errorf("the endpoint id is still there or its placeholder is not: the fixture must read the same at every recording")
	}
}

func TestRecordRefusesMoreUnknownIDsThanTheScenarioDeclared(t *testing.T) {
	digest := recHex("the pinned image")
	t.Setenv(enginecontract.ImageEnv, anonImage+digest)
	s, out := recordingSession(t, enginecontract.Record)
	ex := anonExchange(recHex("an endpoint"), digest)
	ex.Stderr = append(ex.Stderr, []byte(recHex("another one"))...)

	rt := record(t, s, "launch", "port-taken", ex, enginefixture.Redaction{RunID: recRun, Anonymous: 1})

	if len(rt.fatals) != 1 || !strings.Contains(rt.fatals[0], enginefixture.RuleUnknownEngineID) {
		t.Fatalf("fatals = %q, want one that names %s", rt.fatals, enginefixture.RuleUnknownEngineID)
	}
	if files, _ := filepath.Glob(filepath.Join(out, "launch", "*")); len(files) != 0 {
		t.Errorf("%d file(s) written for a refused recording", len(files))
	}
}

func TestVerifyAnonymizesWithTheSameDigestPublic(t *testing.T) {
	digest := recHex("the pinned image")
	t.Setenv(enginecontract.ImageEnv, anonImage+digest)
	r := enginefixture.Redaction{RunID: recRun, Anonymous: 1}
	redacted, _ := enginefixture.Redact(anonExchange(recHex("an endpoint"), digest), enginefixture.Redaction{RunID: recRun, Anonymous: 1, Public: []string{digest}})
	fixture := recorded("6.2.1", enginefixture.Rootless, "")
	fixture.Scenario, fixture.Name, fixture.Exchange = "port-taken", "port-taken--podman-6.2.1-rootless", redacted
	rt := &recT{TB: t}

	rt.run(func() {
		verifySession().VerifyAgainst(rt, []enginefixture.Fixture{fixture}, "port-taken", anonExchange(recHex("another endpoint"), digest), r, nil)
	})

	if !rt.quiet() {
		t.Errorf("a run with another endpoint id differs from the recording: %s", rt.text())
	}
}
