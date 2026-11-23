package security

import (
	"bytes"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/remediation"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-753, step 1 (AC1, AC2): the fix as it would land is computed once, and
// the preview and the confirmation read that one product. Nothing here touches
// the real HOME, the keyring or a process: each test works in t.TempDir() and
// feeds Update by hand (feed drops Cmds, so the preparation Cmd is run with run).

// ── The fixtures are the catalog ─────────────────────────────────────────────

// A rule added to the catalog without a fixture fails here, not silently.
func TestEveryCatalogRuleHasAChangeFixture(t *testing.T) {
	ids := remediation.CatalogIDs()
	if len(ids) == 0 {
		t.Fatal("remediation.CatalogIDs is empty")
	}
	have := map[string]bool{}
	for _, fx := range changeFixtures(t) {
		if have[fx.id] {
			t.Errorf("two fixtures for %s", fx.id)
		}
		have[fx.id] = true
	}
	for _, id := range ids {
		if !have[id] {
			t.Errorf("the catalog has %s, no change fixture does", id)
		}
	}
	for id := range have {
		if !slices.Contains(ids, id) {
			t.Errorf("a change fixture is about %s, which the catalog does not hold", id)
		}
	}
}

// The fixtures themselves are valid: the catalog fixes every one, with no help
// from the code under test.
func TestEveryChangeFixtureIsFixedByItsRule(t *testing.T) {
	for _, fx := range changeFixtures(t) {
		t.Run(fx.id, func(t *testing.T) {
			dir := fixtureOnDisk(t, fx)
			rule := ruleOf(t, fx)
			rel, reason := rule.FileFor(dir, fx.finding)
			if reason != "" || rel != fx.edited {
				t.Fatalf("FileFor = %q, %q; want %q", rel, reason, fx.edited)
			}
			edits, reason := rule.Fix([]byte(fx.files[rel]), fx.finding)
			if reason != "" || len(edits) == 0 {
				t.Fatalf("the rule declined the fixture: %q", reason)
			}
		})
	}
}

// ── changeLines: the display form of a patch.Diff ────────────────────────────

func TestChangeLines(t *testing.T) {
	tests := []struct {
		name string
		diff string
		want []string
	}{
		{"no diff", "", nil},
		{"only the headers", "--- f\n+++ f\n", nil},
		{"the two header lines are dropped and the hunk marker is indented",
			"--- Dockerfile\n+++ Dockerfile\n@@ line 2 @@\n-RUN a\n+RUN b\n",
			[]string{"  @@ line 2 @@", "-RUN a", "+RUN b"}},
		{"several hunks, in order",
			"--- f\n+++ f\n@@ line 2 @@\n-a\n+b\n@@ line 9 @@\n+c\n",
			[]string{"  @@ line 2 @@", "-a", "+b", "  @@ line 9 @@", "+c"}},
		// A removed line "-- x" reads "--- x" and an added "++ y" reads "+++ y":
		// only the first two lines of the diff are headers, so filtering on the
		// prefix would silently eat real changes.
		{"a changed line that looks like a header is kept",
			"--- f\n+++ f\n@@ line 1 @@\n--- x\n+++ y\n",
			[]string{"  @@ line 1 @@", "--- x", "+++ y"}},
		{"an added empty line", "--- f\n+++ f\n@@ line 3 @@\n+\n", []string{"  @@ line 3 @@", "+"}},
		{"no trailing empty line", "--- f\n+++ f\n@@ line 1 @@\n+a\n", []string{"  @@ line 1 @@", "+a"}},
		{"an escape sequence is removed whole",
			"--- f\n+++ f\n@@ line 1 @@\n+a\x1b[31mb\x1b[0m\n-c\x1b]0;title\x07d\n",
			[]string{"  @@ line 1 @@", "+ab", "-cd"}},
		{"a carriage return is dropped", "--- f\n+++ f\n@@ line 1 @@\n+a\r\n", []string{"  @@ line 1 @@", "+a"}},
		{"a tab becomes four spaces", "--- f\n+++ f\n@@ line 1 @@\n+\ta\n", []string{"  @@ line 1 @@", "+    a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := changeLines(tt.diff)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("changeLines(%q)\n got: %q\nwant: %q", tt.diff, got, tt.want)
			}
		})
	}
}

// AC1: the product is not capped and not cut. The section shows the first lines
// and says how many more; the product itself keeps them all.
func TestChangeLinesKeepsEveryLineAndEveryCell(t *testing.T) {
	long := strings.Repeat("x", 600)
	var b strings.Builder
	b.WriteString("--- f\n+++ f\n")
	for i := 1; i <= 120; i++ {
		b.WriteString("@@ line 1 @@\n+" + long + "\n")
	}
	got := changeLines(b.String())
	if len(got) != 240 {
		t.Fatalf("%d lines, want 240", len(got))
	}
	for _, l := range got {
		if strings.HasPrefix(l, "+") && l != "+"+long {
			t.Fatalf("a line was cut to %d cells", len(l))
		}
	}
}

// ── The product of one rule ──────────────────────────────────────────────────

// changeLineIsFormatted says whether a line of the preview starts the way AC1
// requires: a marker, or the two spaces of an indented hunk marker.
func changeLineIsFormatted(l string) bool {
	return strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") || strings.HasPrefix(l, "  ")
}

func TestEveryRuleYieldsAFormattedChange(t *testing.T) {
	for _, fx := range changeFixtures(t) {
		t.Run(fx.id, func(t *testing.T) {
			rule := ruleOf(t, fx)
			original := []byte(fx.files[fx.edited])

			change, declined, err := misconfigChangeOf(rule, fx.edited, original, fx.finding)
			if err != nil || declined != "" {
				t.Fatalf("declined %q, error %v", declined, err)
			}
			if change.File != fx.edited {
				t.Errorf("File = %q, want %q", change.File, fx.edited)
			}
			if len(change.Lines) == 0 {
				t.Fatal("no line")
			}
			for _, l := range change.Lines {
				if !changeLineIsFormatted(l) {
					t.Errorf("line %q starts with neither +, - nor two spaces", l)
				}
				if l == "--- "+fx.edited || l == "+++ "+fx.edited {
					t.Errorf("the diff header %q is in the lines", l)
				}
				if strings.ContainsAny(l, "\x1b\r\x07") {
					t.Errorf("line %q holds a control character", l)
				}
			}
			if !strings.HasPrefix(change.Lines[0], "  @@ line ") || !strings.HasSuffix(change.Lines[0], " @@") {
				t.Errorf("the first line is %q, want an indented hunk marker", change.Lines[0])
			}

			// The bytes ctrl+o writes are the catalog's own rewrite, and the lines
			// are the display form of its own diff: nothing else feeds them.
			edits, _ := rule.Fix(original, fx.finding)
			want, err := patch.Rewrite(original, edits)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(change.Updated, want) {
				t.Errorf("Updated differs from patch.Rewrite:\n got: %q\nwant: %q", change.Updated, want)
			}
			diff, err := patch.Diff(fx.edited, original, edits)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(change.Lines, changeLines(diff)) {
				t.Errorf("Lines are not changeLines(patch.Diff):\n got: %q\nwant: %q", change.Lines, changeLines(diff))
			}
			if got, want := change.Scope, rule.ScopeOf(want, edits); got.Line != want.Line || got.EndLine != want.EndLine || !slices.Equal(got.Rules, want.Rules) {
				t.Errorf("Scope = %+v, want %+v", got, want)
			}
		})
	}
}

// A rule that declines is an answer, with its reason and no line (AC1).
func TestAChangeThatDeclinesHasAReasonAndNoLines(t *testing.T) {
	src := "FROM debian:12\nRUN apt-get install --no-install-recommends -y curl\n"
	fx := dockerfileFixture("AVD-DS-0029", src, 2, 2)
	change, declined, err := misconfigChangeOf(ruleOf(t, fx), "Dockerfile", []byte(src), fx.finding)
	if err != nil {
		t.Fatalf("error %v, want a decline", err)
	}
	if declined != remediation.ReasonAlreadyFixed {
		t.Errorf("declined = %q, want %q", declined, remediation.ReasonAlreadyFixed)
	}
	if len(change.Lines) != 0 || len(change.Updated) != 0 {
		t.Errorf("a declined change carries %d lines and %d bytes", len(change.Lines), len(change.Updated))
	}
}

// ── computeMisconfigFix: the one computation behind the preview and ctrl+o ───

func TestComputeMisconfigFixCarriesTheChangeOfEveryRule(t *testing.T) {
	for _, fx := range changeFixtures(t) {
		t.Run(fx.id, func(t *testing.T) {
			dir := fixtureOnDisk(t, fx)
			msg := computeMisconfigFix(dir, fx.finding, time.Time{})
			if msg.Err != nil {
				t.Fatalf("error: %v", msg.Err)
			}
			rule := ruleOf(t, fx)
			change, _, _ := misconfigChangeOf(rule, fx.edited, []byte(fx.files[fx.edited]), fx.finding)
			if len(msg.Lines) == 0 || !slices.Equal(msg.Lines, change.Lines) {
				t.Errorf("Lines = %q, want %q", msg.Lines, change.Lines)
			}
			if msg.File.File != fx.edited || !bytes.Equal(msg.File.Updated, change.Updated) {
				t.Errorf("File = %q, %d bytes; want %q, %d bytes", msg.File.File, len(msg.File.Updated), fx.edited, len(change.Updated))
			}
			if string(msg.File.Original) != fx.files[fx.edited] {
				t.Errorf("Original is not the file as it was read")
			}
			if msg.FindingFile != fx.finding.File || msg.Rule != fx.finding.ID {
				t.Errorf("FindingFile, Rule = %q, %q; want %q, %q", msg.FindingFile, msg.Rule, fx.finding.File, fx.finding.ID)
			}
			// The body is the title, the warning, then the same lines.
			if !strings.Contains(msg.Body, strings.Join(change.Lines, "\n")) {
				t.Errorf("the body does not hold the preview lines:\n%s", msg.Body)
			}
			if !strings.HasPrefix(msg.Body, rule.Title) {
				t.Errorf("the body does not start with the rule's title %q:\n%s", rule.Title, msg.Body)
			}
			if rule.Warning != "" && !strings.Contains(msg.Body, "Warning: "+rule.Warning) {
				t.Errorf("the body lost the rule's warning:\n%s", msg.Body)
			}
			for _, h := range []string{"--- " + fx.edited, "+++ " + fx.edited} {
				if strings.Contains(msg.Body, h) {
					t.Errorf("the body still carries the diff header %q:\n%s", h, msg.Body)
				}
			}
		})
	}
}

func TestComputeMisconfigFixOfADecliningRuleHasAReasonAndNoLines(t *testing.T) {
	fx := dockerfileFixture("AVD-DS-0029", "FROM debian:12\nRUN apt-get install --no-install-recommends -y curl\n", 2, 2)
	dir := fixtureOnDisk(t, fx)
	msg := computeMisconfigFix(dir, fx.finding, time.Time{})
	var d errDeclined
	if !errors.As(msg.Err, &d) || d.reason != remediation.ReasonAlreadyFixed {
		t.Fatalf("Err = %v, want a decline: %q", msg.Err, remediation.ReasonAlreadyFixed)
	}
	if len(msg.Lines) != 0 || msg.Body != "" {
		t.Errorf("a decline carries %d lines and a body %q", len(msg.Lines), msg.Body)
	}
}

// ── AC2: what the modal shows is what is written ─────────────────────────────

// diffRows counts the lines that read as part of a diff.
func diffRows(lines []string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "+") || strings.HasPrefix(l, "-") || strings.HasPrefix(l, "  @@") {
			n++
		}
	}
	return n
}

// indexOfBlock is where block starts in lines, or -1.
func indexOfBlock(lines, block []string) int {
	for i := 0; i+len(block) <= len(lines); i++ {
		if slices.Equal(lines[i:i+len(block)], block) {
			return i
		}
	}
	return -1
}

// For every rule of the catalog: the text of the modal holds exactly the lines
// of the preview, then the file and its git advice; and once the answer is yes,
// the bytes on disk are the bytes the preview was computed with.
func TestTheModalShowsExactlyThePreviewLinesAndTheFileWrittenIsUpdated(t *testing.T) {
	for _, fx := range changeFixtures(t) {
		t.Run(fx.id, func(t *testing.T) {
			dir := fixtureOnDisk(t, fx)
			m := changeModel(t, dir, fx.finding, time.Time{})

			m, msg := previewed(t, m)
			if m.confirmModal == nil {
				t.Fatalf("no confirmation opened; footer: %q", m.footer.Text())
			}
			if len(msg.Lines) == 0 {
				t.Fatal("the preparation produced no preview line")
			}

			rows := modalLines(m)
			at := indexOfBlock(rows, msg.Lines)
			if at < 0 {
				t.Fatalf("the modal does not hold the preview lines:\n%q\nmodal:\n%s", msg.Lines, strings.Join(rows, "\n"))
			}
			if got, want := diffRows(rows), len(msg.Lines); got != want {
				t.Errorf("the modal has %d diff rows, the preview %d: something else is shown as a change", got, want)
			}
			// The file and its git advice follow the lines, as before (modalLines
			// leaves the blank rows out).
			tail := rows[at+len(msg.Lines):]
			if len(tail) == 0 || !strings.HasPrefix(tail[0], fx.edited+": ") {
				t.Errorf("after the lines, the modal reads %q; want %q", tail, fx.edited+": <git advice>")
			}
			// The text handed to the modal is the body of the preparation.
			if want := misconfigConfirmationText(msg.File, msg.Body); !strings.Contains(want, strings.Join(msg.Lines, "\n")) {
				t.Errorf("the confirmation text does not hold the lines joined by newlines:\n%s", want)
			}
			if got := readTree(t, dir, fx.edited); got != fx.files[fx.edited] {
				t.Fatalf("the file was written before the answer:\n%s", got)
			}

			// Yes: the write is the one the modal was about.
			m, cmd := step(t, m, sharedcomponents.ConfirmModalYesMsg{})
			written, ok := run(t, cmd).(MisconfigFixWrittenMsg)
			if !ok || written.Err != nil {
				t.Fatalf("the write failed: %+v", written)
			}
			_ = feed(t, m, written)
			if got := readTree(t, dir, fx.edited); got != string(msg.File.Updated) {
				t.Errorf("the file on disk is not Updated:\n got: %q\nwant: %q", got, msg.File.Updated)
			}
			// And it is what the catalog's rewrite gives, computed here on its own.
			edits, _ := ruleOf(t, fx).Fix([]byte(fx.files[fx.edited]), fx.finding)
			want, err := patch.Rewrite([]byte(fx.files[fx.edited]), edits)
			if err != nil || readTree(t, dir, fx.edited) != string(want) {
				t.Errorf("the file on disk is not patch.Rewrite of the original (err %v)", err)
			}
		})
	}
}

// What ctrl+o prepares from the tab and from the detail is one product: the
// Cmd both start is computeMisconfigFix.
func TestTheKeyPreparesTheSameMessageAsTheSharedComputation(t *testing.T) {
	fx := changeFixtures(t)[0]
	dir := fixtureOnDisk(t, fx)
	m := changeModel(t, dir, fx.finding, time.Time{})

	_, got := previewed(t, m)
	want := computeMisconfigFix(dir, fx.finding, time.Time{})

	if got.Err != nil || want.Err != nil {
		t.Fatalf("errors: key %v, shared %v", got.Err, want.Err)
	}
	if !slices.Equal(got.Lines, want.Lines) || got.Body != want.Body || !bytes.Equal(got.File.Updated, want.File.Updated) {
		t.Errorf("the key's preparation differs from computeMisconfigFix:\n key: %+v\nshared: %+v", got, want)
	}
	if len(want.Lines) == 0 {
		t.Error("the shared computation produced no line")
	}
	// From the detail, the same key gives the same text.
	detail := feed(t, m, testutil.Key("enter"))
	_, fromDetail := previewed(t, detail)
	if !slices.Equal(fromDetail.Lines, want.Lines) || fromDetail.Body != want.Body {
		t.Error("the detail's preparation differs from the shared computation")
	}
}

// ── Control characters, line endings ─────────────────────────────────────────

// A file is not trusted: what a line of it holds must not reach the terminal,
// neither in the preview nor in the confirmation. The bytes written are another
// matter: the fix edits the file, not a sanitized copy of it.
func TestAnEscapeSequenceInTheFileNeverReachesTheLinesOrTheModal(t *testing.T) {
	const hostile = "\x1b]0;pwned\x07\x1b[31mred\x1b[0m"
	src := "FROM debian:12\nRUN apt-get install -y curl # " + hostile + "\n"
	fx := dockerfileFixture("AVD-DS-0029", src, 2, 2)
	dir := fixtureOnDisk(t, fx)
	m := changeModel(t, dir, fx.finding, time.Time{})

	m, msg := previewed(t, m)
	if m.confirmModal == nil || msg.Err != nil {
		t.Fatalf("no confirmation: %v", msg.Err)
	}
	if len(msg.Lines) == 0 {
		t.Fatal("no preview line")
	}
	joined := strings.Join(msg.Lines, "\n")
	for _, bad := range []string{"\x1b", "\x07", "pwned"} {
		if strings.Contains(joined, bad) {
			t.Errorf("the preview lines hold %q: %q", bad, joined)
		}
	}
	if !strings.Contains(joined, "red") {
		t.Errorf("the visible text of the line was lost: %q", joined)
	}
	text := misconfigConfirmationText(msg.File, msg.Body)
	for _, bad := range []string{"\x1b", "\x07", "pwned"} {
		if strings.Contains(text, bad) {
			t.Errorf("the confirmation text holds %q:\n%q", bad, text)
		}
	}
	// What the modal renders adds styling of its own, but never an OSC or a bell.
	view := m.confirmModal.View()
	if strings.Contains(view, "\x1b]") || strings.Contains(view, "\x07") {
		t.Errorf("the rendered modal holds an OSC or a bell: %q", view)
	}
	if !strings.Contains(string(msg.File.Updated), hostile) {
		t.Error("Updated lost the raw bytes of the file: the fix must edit the file, not a sanitized copy")
	}
}

func TestACRLFFileGivesLinesWithNoCarriageReturn(t *testing.T) {
	src := "FROM debian:12\r\nRUN apt-get install curl\r\n"
	fx := dockerfileFixture("AVD-DS-0021", src, 2, 2)
	dir := fixtureOnDisk(t, fx)
	msg := computeMisconfigFix(dir, fx.finding, time.Time{})
	if msg.Err != nil || len(msg.Lines) == 0 {
		t.Fatalf("Err %v, %d lines", msg.Err, len(msg.Lines))
	}
	for _, l := range msg.Lines {
		if strings.Contains(l, "\r") {
			t.Errorf("line %q holds a carriage return", l)
		}
	}
	if strings.Contains(msg.Body, "\r") {
		t.Errorf("the body holds a carriage return: %q", msg.Body)
	}
	if !bytes.Contains(msg.File.Updated, []byte("\r\n")) || bytes.Contains(bytes.ReplaceAll(msg.File.Updated, []byte("\r\n"), nil), []byte("\n")) {
		t.Errorf("Updated lost the file's own line endings: %q", msg.File.Updated)
	}
}

// ── previewFailure: a failure is worded, never printed ───────────────────────

func TestPreviewFailureNamesTheFileAndNeverTheError(t *testing.T) {
	const rel = "deploy/app.yaml"
	secret := "open /home/someone/private/path: errno 13 raw detail"
	tests := []struct {
		name string
		err  error
	}{
		{"not found", &fs.PathError{Op: "open", Path: "/abs/" + rel, Err: fs.ErrNotExist}},
		{"permission", &fs.PathError{Op: "open", Path: "/abs/" + rel, Err: fs.ErrPermission}},
		{"anything else", errors.New(secret)},
	}
	seen := map[string]string{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := previewFailure(rel, tt.err)
			if got == "" {
				t.Fatal("empty reason")
			}
			if strings.Contains(got, "\n") || strings.Contains(got, "/abs/") || strings.Contains(got, "errno") || strings.Contains(got, "private") {
				t.Errorf("the reason shows the error: %q", got)
			}
			if prev, dup := seen[got]; dup {
				t.Errorf("%q is also the reason for %s", got, prev)
			}
			seen[got] = tt.name
		})
	}
	if got := previewFailure(rel, tests[1].err); got != rel+" cannot be read" {
		t.Errorf("permission: %q, want %q", got, rel+" cannot be read")
	}
	if got := previewFailure(rel, tests[0].err); !strings.Contains(got, rel) {
		t.Errorf("not found: %q does not name the file", got)
	}
	if got := previewFailure(rel, errors.New(secret)); got != "the fix could not be computed — check logs" {
		t.Errorf("other: %q", got)
	}
	// A wrapped error is still classified.
	wrapped := errors.Join(errors.New("context"), &fs.PathError{Op: "stat", Path: rel, Err: fs.ErrPermission})
	if got := previewFailure(rel, wrapped); got != rel+" cannot be read" {
		t.Errorf("wrapped permission: %q", got)
	}
}

// The footer line the declines of steps 1 and 2 share is worded in English and
// does not leak a path (Rule 128, Rule 129).
func TestTheStaleReasonIsAnEnglishSentenceWithNoPath(t *testing.T) {
	if reasonPreviewStale == "" || reasonPreviewStale == "SKELETON" {
		t.Fatal("reasonPreviewStale is not worded yet")
	}
	if want := "The file changed after the scan — re-scan to preview the fix"; reasonPreviewStale != want {
		t.Errorf("reasonPreviewStale = %q, want %q", reasonPreviewStale, want)
	}
}
