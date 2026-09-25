package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/trust"
)

// verdictsByImage answers with a fixed verdict per repository.
type verdictsByImage map[string]trust.Verdict

func (v verdictsByImage) Verify(_ context.Context, ref string, _ trust.Rule) (trust.Verdict, error) {
	if verdict, ok := v[trust.Repository(ref)]; ok {
		return verdict, nil
	}
	return trust.Verified, nil
}

func (verdictsByImage) Identities(context.Context, string) ([]trust.Identity, error) { return nil, nil }

func signatureRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func userRule(match string) trust.Rule {
	return trust.Rule{Match: match, Mode: trust.ModeKey, Source: trust.SourceUser, Origin: "trust.yaml:1"}
}

func checkDeps(v trust.Verifier, rules ...trust.Rule) trust.CheckDeps {
	return trust.CheckDeps{
		Policy:   func() (trust.Policy, error) { return trust.Policy{Rules: rules}, nil },
		Verifier: v,
		Digest:   func(string) (string, error) { return "sha256:abc", nil },
	}
}

func TestABaseImageThatViolatesItsRuleIsAFinding(t *testing.T) {
	dir := signatureRepo(t, map[string]string{
		"Dockerfile":     "FROM registry.corp.example/build:1 AS build\nRUN make\nFROM registry.corp.example/run:1\n",
		"api/Dockerfile": "FROM registry.corp.example/build:1\n",
	})
	v := verdictsByImage{
		"registry.corp.example/build": trust.IdentityMismatch,
		"registry.corp.example/run":   trust.Unsigned,
	}
	findings, errs := checkBaseImageSignatures(context.Background(), dir, checkDeps(v, userRule("registry.corp.example/*")))
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	got := map[string]Finding{}
	for _, f := range findings {
		got[f.File+":"+f.ID] = f
	}
	// Every stage counts, a build stage too, in every Dockerfile.
	for key, severity := range map[string]SeverityLevel{
		"Dockerfile:" + SignatureMismatchID:     SeverityCritical,
		"Dockerfile:" + SignatureUnsignedID:     SeverityHigh,
		"api/Dockerfile:" + SignatureMismatchID: SeverityCritical,
	} {
		f, ok := got[key]
		if !ok {
			t.Errorf("no finding %s among %v", key, findings)
			continue
		}
		if f.Severity != severity || f.Source != SourceSignature || !strings.Contains(f.Message, "trust.yaml:1") {
			t.Errorf("%s = %+v", key, f)
		}
		if Categorize(f) != CategoryMisconfiguration {
			t.Errorf("%s is not a misconfiguration", key)
		}
	}
	if f := got["Dockerfile:"+SignatureUnsignedID]; f.Line != 3 {
		t.Errorf("anchored on line %d, want the FROM's", f.Line)
	}
}

// A finding states a fact: nothing under no rule, nothing for a warning, and a
// check a user rule could not run is the stage's error.
func TestOnlyAProvenViolationIsAFinding(t *testing.T) {
	dir := signatureRepo(t, map[string]string{"Dockerfile": "FROM python:3.12\nFROM scratch\n"})

	findings, errs := checkBaseImageSignatures(context.Background(), dir, checkDeps(verdictsByImage{"docker.io/library/python": trust.Unsigned}))
	if len(findings) != 0 || len(errs) != 0 {
		t.Errorf("no rule: findings %v, errors %v", findings, errs)
	}

	unreachable := checkDeps(verdictsByImage{}, userRule("docker.io/library/*"))
	unreachable.Digest = func(string) (string, error) { return "", errors.New("registry unreachable") }
	findings, errs = checkBaseImageSignatures(context.Background(), dir, unreachable)
	if len(findings) != 0 || len(errs) != 1 || !strings.Contains(errs[0].Error(), "python:3.12") {
		t.Errorf("unreachable under a user rule: findings %v, errors %v", findings, errs)
	}
}

// inferredUnsigned is key mode's fail-closed answer to an exit code it cannot
// place — maybe a wrong key, maybe the network.
type inferredUnsigned struct{}

func (inferredUnsigned) Verify(context.Context, string, trust.Rule) (trust.Verdict, error) {
	return trust.Unsigned, trust.ErrUnproven
}

func (inferredUnsigned) Identities(context.Context, string) ([]trust.Identity, error) {
	return nil, nil
}

// It still blocks a pull, but it is not a fact about the image: the stage's
// error, once, not a HIGH finding.
func TestAnInferredUnsignedIsNotAFinding(t *testing.T) {
	dir := signatureRepo(t, map[string]string{"Dockerfile": "FROM registry.corp.example/run:1\nFROM registry.corp.example/run:1\n"})
	findings, errs := checkBaseImageSignatures(context.Background(), dir, checkDeps(inferredUnsigned{}, userRule("registry.corp.example/*")))
	if len(findings) != 0 || len(errs) != 1 || !strings.Contains(errs[0].Error(), "trust.yaml:1") {
		t.Errorf("findings %v, errors %v", findings, errs)
	}
}

func TestEachImageIsCheckedOnce(t *testing.T) {
	dir := signatureRepo(t, map[string]string{
		"a/Dockerfile": "FROM golang:1.23\n",
		"b/Dockerfile": "FROM golang:1.23\n",
	})
	var mu sync.Mutex
	calls := 0
	d := checkDeps(verdictsByImage{})
	d.Digest = func(string) (string, error) { mu.Lock(); calls++; mu.Unlock(); return "sha256:x", nil }
	_, _ = checkBaseImageSignatures(context.Background(), dir, d)
	if calls != 1 {
		t.Errorf("golang:1.23 resolved %d times", calls)
	}
}

func TestTheStageRunsOnlyWhenTheContextVerifies(t *testing.T) {
	categories := config.DefaultScanCategories()
	categories.Misconfig.Enabled = true
	on := &Scanner{options: ScanOptions{Categories: categories, ImageVerification: config.ImageVerificationOn}}
	off := &Scanner{options: ScanOptions{Categories: categories, ImageVerification: config.ImageVerificationOff}}
	if !on.checksSignatures(TargetDirectory) || on.checksSignatures(TargetImage) {
		t.Error("on: the stage must run on a directory, and only there")
	}
	if off.checksSignatures(TargetDirectory) {
		t.Error("off: the stage ran")
	}
}

func TestTagOf(t *testing.T) {
	for ref, want := range map[string]string{
		"python": "latest", "python:3.12": "3.12", "localhost:5000/app": "latest",
		"localhost:5000/app:1@sha256:x": "1", "gcr.io/distroless/static-debian12:nonroot": "nonroot",
	} {
		if got := tagOf(ref); got != want {
			t.Errorf("tagOf(%q) = %q, want %q", ref, got, want)
		}
	}
}
