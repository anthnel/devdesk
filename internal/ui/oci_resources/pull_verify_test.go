package ociresources

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/config"
	"github.com/anthnel/devdesk/internal/docker"
	"github.com/anthnel/devdesk/internal/imagepull"
	"github.com/anthnel/devdesk/internal/jobs"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/trust"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/sigcol"
	"github.com/anthnel/devdesk/internal/ui/testutil"
	"github.com/anthnel/devdesk/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// unverifiedPull is what every test of this package pulls with unless it says
// otherwise: no registry, no cosign — the engine seam alone.
func unverifiedPull() imagepull.Deps {
	return imagepull.Deps{Pull: func(ctx context.Context, ref string) error { return pullImageContext(ctx, ref) }}
}

type answer struct{ verdict trust.Verdict }

func (a answer) Verify(context.Context, string, trust.Rule) (trust.Verdict, error) {
	return a.verdict, nil
}

func (answer) Identities(context.Context, string) ([]trust.Identity, error) { return nil, nil }

// verifyingPull installs a pull verified against rule, answering verdict, and
// records what reached the engine.
func verifyingPull(t *testing.T, verdict trust.Verdict, rule trust.Rule) *[]string {
	t.Helper()
	var calls []string
	prev := newPullDeps
	newPullDeps = func(*config.Config, *scan.Report) imagepull.Deps {
		return imagepull.Deps{
			Enabled:  true,
			Policy:   func() (trust.Policy, error) { return trust.Policy{Rules: []trust.Rule{rule}}, nil },
			Verifier: answer{verdict},
			Digest:   func(string) (string, error) { return "sha256:new", nil },
			Current:  func(string) string { return "" },
			Tag:      func(string, string) error { return nil },
		}
	}
	prevPull := pullImageContext
	pullImageContext = func(_ context.Context, ref string) error { calls = append(calls, ref); return nil }
	t.Cleanup(func() { newPullDeps, pullImageContext = prev, prevPull })
	return &calls
}

var corpRule = trust.Rule{Match: "registry.example.com/**", Mode: trust.ModeKey, Source: trust.SourceUser, Origin: "trust.yaml:4"}

// pullThroughTheView asks for a pull the way the browser does and feeds its
// completion back.
func pullThroughTheView(t *testing.T, ref string) (Model, RegistryPullCompleteMsg) {
	t.Helper()
	m := newTestModel(t)
	var done RegistryPullCompleteMsg
	for _, msg := range testutil.Msgs(pullOneImageCmd(ref, m.pullDeps())) {
		if d, ok := msg.(RegistryPullCompleteMsg); ok {
			done = d
		}
	}
	return feed(t, m, done), done
}

func TestARefusedPullSaysWhyNamingTheRule(t *testing.T) {
	calls := verifyingPull(t, trust.Unsigned, corpRule)
	m, done := pullThroughTheView(t, "registry.example.com/api:v1")
	if len(*calls) != 0 {
		t.Errorf("the engine was asked to pull: %q", *calls)
	}
	if m.footer.Level() != sharedcomponents.LevelError || !strings.Contains(m.footer.Text(), "trust.yaml:4") {
		t.Errorf("footer = %q (level %v)", m.footer.Text(), m.footer.Level())
	}
	// :jobs, and an agent that asked for the pull, read the reason too.
	if tr := done.Transition(); tr.State != jobs.ItemFailed || !strings.Contains(tr.Detail, "trust.yaml:4") {
		t.Errorf("transition = %+v", tr)
	}
}

func TestAWarnedPullSaysSoAfterPulling(t *testing.T) {
	calls := verifyingPull(t, trust.Failed, trust.Rule{Match: "registry.example.com/**", Mode: trust.ModeKeyless,
		Issuer: "i", Subject: "s", Source: trust.SourceBuiltin, Origin: "built-in: test"})
	m, _ := pullThroughTheView(t, "registry.example.com/api:v1")
	if len(*calls) != 1 || (*calls)[0] != "registry.example.com/api@sha256:new" {
		t.Errorf("pulled %q, want the verified digest", *calls)
	}
	if m.footer.Level() != sharedcomponents.LevelWarning || !strings.Contains(m.footer.Text(), "could not be verified") {
		t.Errorf("footer = %q (level %v)", m.footer.Text(), m.footer.Level())
	}
}

func TestAnUpdateIsVerifiedToo(t *testing.T) {
	calls := verifyingPull(t, trust.IdentityMismatch, corpRule)
	m := newTestModel(t)
	var done RegistryPullCompleteMsg
	for _, msg := range testutil.Msgs(updateImageCmd("registry.example.com/api:v2", imageFixtures()[0], m.pullDeps())) {
		if d, ok := msg.(RegistryPullCompleteMsg); ok {
			done = d
		}
	}
	if !isBlocked(done.Err) || len(*calls) != 0 {
		t.Fatalf("err %v, pulled %q", done.Err, *calls)
	}
	done.Replaces = "registry.example.com/api:v1"
	m = feed(t, m, done)
	if !strings.Contains(m.footer.Text(), "refused") {
		t.Errorf("footer = %q", m.footer.Text())
	}
}

// Every pull goes through imagepull: a second path would be a pull nobody
// verified. The engine's pull is named once, as the seam pullDeps hands to
// imagepull, and called nowhere.
func TestNoPullBypassesTheSignatureCheck(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(data)
		rel := filepath.ToSlash(path)
		// The walk starts at internal/, so paths read "../../docker/images.go".
		allowed := false
		for _, file := range []string{"/docker/images.go", "/imagepull/pull.go", "/imagepull/default.go", "/ui/oci_resources/image_update.go"} {
			allowed = allowed || strings.HasSuffix(rel, file)
		}
		if strings.Contains(src, "PullImageContext") && !allowed {
			t.Errorf("%s names docker.PullImageContext — pull through imagepull.Pull", rel)
		}
		if strings.Contains(src, "docker.PullImage(") {
			t.Errorf("%s calls docker.PullImage — pull through imagepull.Pull", rel)
		}
		if strings.Contains(rel, "/ui/oci_resources/") && strings.Contains(src, "pullImageContext(") {
			t.Errorf("%s calls the engine seam directly — it only feeds imagepull.Deps", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func headerValue(m Model, key string) (string, bool) {
	for _, h := range m.GetHeaderInfo("") {
		if h.Key == key {
			return h.Value, true
		}
	}
	return "", false
}

func TestTheHeaderSaysWhenSignaturesAreNotChecked(t *testing.T) {
	m := newTestModel(t)
	if v, ok := headerValue(m, "Signatures"); ok {
		t.Errorf("checks running as configured need no header field, got %q", v)
	}

	m = feed(t, m, TrustPolicyCheckedMsg{Err: errors.New("line 3: field isuer not found")})
	if v, _ := headerValue(m, "Signatures"); v != "trust.yaml invalid" {
		t.Errorf("Signatures = %q", v)
	}
	if m.footer.Level() != sharedcomponents.LevelError {
		t.Errorf("an invalid policy refuses every pull and must say so: footer %q", m.footer.Text())
	}

	cfg := testConfig()
	cfg.Scan.ImageVerification = config.ImageVerificationOff
	off := feed(t, New(cfg), tea.WindowSizeMsg{Width: 180, Height: 30})
	if v, _ := headerValue(off, "Signatures"); v != "off" {
		t.Errorf("Signatures = %q, want off", v)
	}
}

// ── The Sig column ───────────────────────────────────────────────────────────

func sigCellOf(t *testing.T, m Model, name string) string {
	t.Helper()
	for _, r := range m.imageTable.Items() {
		if r.RawName == name {
			return sigcol.Cell(r.Signature)
		}
	}
	t.Fatalf("no row for %s", name)
	return ""
}

var signedImages = []docker.Image{
	{ID: "a1", Repository: "gcr.io/distroless/static", Tag: "nonroot", RepoDigests: []string{"gcr.io/distroless/static@sha256:aaa"}},
	{ID: "b2", Repository: "myapp", Tag: "dev"}, // built here: no registry digest
}

func TestTheSigColumnChecksTheDigestOnDisk(t *testing.T) {
	var asked []string
	prev := newPullDeps
	newPullDeps = func(*config.Config, *scan.Report) imagepull.Deps {
		return imagepull.Deps{
			Enabled:  true,
			Policy:   func() (trust.Policy, error) { return trust.Policy{}, nil },
			Verifier: recordingVerifier{asked: &asked, verdict: trust.Verified},
			Digest:   func(string) (string, error) { t.Error("a local image was resolved at the registry"); return "", nil },
		}
	}
	t.Cleanup(func() { newPullDeps = prev })

	m := newTestModel(t)
	m, cmd := step(t, m, ImagesListMsg{Images: signedImages})
	if got := sigCellOf(t, m, "gcr.io/distroless/static:nonroot"); got != theme.IconHourglass {
		t.Errorf("before the answer: %q, want the hourglass", got)
	}
	if got := sigCellOf(t, m, "myapp:dev"); got != theme.IconHammer {
		t.Errorf("a local build: %q, want the hammer", got)
	}
	for _, msg := range testutil.Msgs(cmd) {
		if sig, ok := msg.(ImageSignatureCheckedMsg); ok {
			m = feed(t, m, sig)
		}
	}
	if got := sigCellOf(t, m, "gcr.io/distroless/static:nonroot"); got != theme.IconOK {
		t.Errorf("after: %q, want verified", got)
	}
	// The digest on disk, by the built-in distroless rule — not a tag.
	if len(asked) != 1 || asked[0] != "gcr.io/distroless/static@sha256:aaa" {
		t.Errorf("asked %q", asked)
	}

	// The same list again asks nothing: the digest has not changed.
	_, cmd = step(t, m, ImagesListMsg{Images: signedImages})
	for _, msg := range testutil.Msgs(cmd) {
		if _, ok := msg.(ImageSignatureCheckedMsg); ok {
			t.Error("an unchanged image was checked again")
		}
	}
}

func TestAStaleVerdictIsDropped(t *testing.T) {
	m := feed(t, newTestModel(t), ImagesListMsg{Images: signedImages})
	m.sigAsked["gcr.io/distroless/static:nonroot"] = sigAsk{Pinned: "gcr.io/distroless/static@sha256:new"}
	m = feed(t, m, ImageSignatureCheckedMsg{Name: "gcr.io/distroless/static:nonroot", Pinned: "gcr.io/distroless/static@sha256:aaa",
		Result: trust.Result{Verdict: trust.IdentityMismatch, Decision: trust.Block}})
	if _, ok := m.signatures["gcr.io/distroless/static:nonroot"]; ok {
		t.Error("a verdict about the digest before a pull was kept")
	}
}

func TestOffShowsNoSignature(t *testing.T) {
	cfg := testConfig()
	cfg.Scan.ImageVerification = config.ImageVerificationOff
	m := feed(t, New(cfg), tea.WindowSizeMsg{Width: 180, Height: 30})
	m, cmd := step(t, m, ImagesListMsg{Images: signedImages})
	for _, msg := range testutil.Msgs(cmd) {
		if _, ok := msg.(ImageSignatureCheckedMsg); ok {
			t.Error("off checked an image")
		}
	}
	if got := sigCellOf(t, m, "gcr.io/distroless/static:nonroot"); got != "-" {
		t.Errorf("cell = %q", got)
	}
}

type recordingVerifier struct {
	asked   *[]string
	verdict trust.Verdict
}

func (r recordingVerifier) Verify(_ context.Context, ref string, _ trust.Rule) (trust.Verdict, error) {
	*r.asked = append(*r.asked, ref)
	return r.verdict, nil
}

func (recordingVerifier) Identities(context.Context, string) ([]trust.Identity, error) {
	return nil, nil
}
