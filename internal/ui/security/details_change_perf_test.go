package security

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// DEVDESK-753, lot 2, step 6 (H6): the preview is computed off Update, by the Cmd
// the detail starts, and it is fast enough that the section needs no loading
// state of its own beyond the footer's announcement. The threshold is wide on
// purpose, so the test is not flaky on a loaded runner: it catches a computation
// that became slow by an order of magnitude (a scan of the whole tree, a git
// command per line), not a few milliseconds.

// previewBudget is the median time the preview of the largest fixture may take.
const previewBudget = 100 * time.Millisecond

// largestChangeFixture is the fixture of the catalog whose change has the most
// lines (AVD-DS-0002 adds a user, a group and a switch: 11 lines).
func largestChangeFixture(t *testing.T) changeFixture {
	t.Helper()
	var best changeFixture
	most := -1
	for _, fx := range changeFixtures(t) {
		msg := computeMisconfigFix(fixtureOnDisk(t, fx), fx.finding, scannedAtFixed)
		if msg.Err != nil {
			t.Fatalf("%s: %v", fx.id, msg.Err)
		}
		if len(msg.Lines) > most {
			best, most = fx, len(msg.Lines)
		}
	}
	return best
}

// The Cmd the detail starts, run as the runtime runs it, on the largest change of
// the catalog: the median of 15 runs is far under 100 ms. The measure is
// recorded in docs/measurements/ by the documentation step.
func TestThePreviewOfTheLargestChangeIsFarUnderTheBudget(t *testing.T) {
	fx := largestChangeFixture(t)
	const runs = 15
	elapsed := make([]time.Duration, 0, runs)
	for range runs {
		s := openPreview(t, fx, -time.Hour)
		start := time.Now()
		msg, ok := run(t, s.cmd).(ChangePreviewMsg)
		took := time.Since(start)
		if !ok || msg.Prepared.Err != nil || len(msg.Prepared.Lines) == 0 {
			t.Fatalf("%s: answer %+v", fx.id, msg)
		}
		elapsed = append(elapsed, took)
	}
	slices.Sort(elapsed)
	if median := elapsed[len(elapsed)/2]; median > previewBudget {
		t.Errorf("the preview of %s takes %v (median of %d, worst %v), the budget is %v: it needs a loading state of its own (H6)",
			fx.id, median, runs, elapsed[len(elapsed)-1], previewBudget)
	}
}

// BenchmarkComputeMisconfigFix measures the shared computation on the largest
// fixture of the catalog (AVD-DS-0002), in a directory that is not a git
// repository. The case with git is measured by hand for the measurement note: a
// benchmark may not start a process of its own.
func BenchmarkComputeMisconfigFix(b *testing.B) {
	fx := dockerfileFixture("AVD-DS-0002", "FROM debian:12\nRUN apt-get install -y curl\nENTRYPOINT [\"/app\"]\n", 1, 3)
	dir := b.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(fx.files["Dockerfile"]), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.Chtimes(path, scannedAtFixed.Add(-time.Hour), scannedAtFixed.Add(-time.Hour)); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if msg := computeMisconfigFix(dir, fx.finding, scannedAtFixed); msg.Err != nil {
			b.Fatal(msg.Err)
		}
	}
}
