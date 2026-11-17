package nexusbench

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The tests below pin the fixed data set of DEVDESK-505 (decision D-I): which
// images the bench holds, where, and the manifest digest of each. Digests come
// from the deterministic OCI layout generator (ocilayout_test.go).

// goldenDatasetDigests lists "name:tag<TAB>location<TAB>digest", one line per
// image, in Dataset order. It is committed and reviewed by hand; after a
// deliberate change of the generator it is refreshed with:
//
//	go test ./internal/nexusbench -run TestDatasetDigestsGolden -update
//
// The test fails, with this command in its message, while the file is missing or
// differs.
const goldenDatasetDigests = "testdata/dataset-digests.golden"

// Locations as the data set spells them (the plan's Location values).
const (
	wantHosted   Location = "docker-hosted"
	wantUpstream Location = "upstream"
)

var digestForm = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// wantDataset is D-I, in order, without digests (those are the golden's job).
var wantDataset = []DatasetImage{
	{Name: "api", Tag: "1.0.0", Location: wantHosted},
	{Name: "api", Tag: "1.1.0", Location: wantHosted},
	{Name: "team/api", Tag: "1.0.0", Location: wantHosted},
	{Name: "tools/cli", Tag: "0.1.0", Location: wantHosted},
	{Name: "shared/base", Tag: "1.0", Location: wantHosted},
	{Name: "shared/base", Tag: "1.0", Location: wantUpstream},
	{Name: "mirror/cached", Tag: "1.0", Location: wantUpstream, PullThrough: true},
	{Name: "mirror/uncached", Tag: "1.0", Location: wantUpstream},
}

func withoutDigest(images []DatasetImage) []DatasetImage {
	out := make([]DatasetImage, len(images))
	for i, img := range images {
		img.Digest = ""
		out[i] = img
	}
	return out
}

func describeImage(img DatasetImage) string {
	return fmt.Sprintf("%s:%s@%s", img.Name, img.Tag, img.Location)
}

// TestDatasetContent: the images, their places and their order are exactly D-I.
func TestDatasetContent(t *testing.T) {
	got := withoutDigest(Dataset())
	if !reflect.DeepEqual(got, wantDataset) {
		t.Errorf("Dataset() (digests aside) =\n%v\nwant\n%v", got, wantDataset)
	}
}

// TestDatasetIsACopy: a caller that edits the result cannot change the next one.
func TestDatasetIsACopy(t *testing.T) {
	first := Dataset()
	if len(first) == 0 {
		t.Fatal("Dataset() is empty")
	}
	first[0].Name = "changed"
	first[0].PullThrough = true
	first[0].Digest = "sha256:x"
	if got := Dataset()[0]; got.Name == "changed" || got.PullThrough || got.Digest == "sha256:x" {
		t.Errorf("a second Dataset() call saw the edit of the first: %+v", got)
	}
}

// TestDatasetImagesAreWellFormed: what the push and the pull need to build a reference.
func TestDatasetImagesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, img := range Dataset() {
		id := describeImage(img)
		if img.Name == "" || img.Tag == "" {
			t.Errorf("%s: empty name or tag", id)
		}
		if strings.ContainsAny(img.Name+img.Tag, ": @") || strings.HasPrefix(img.Name, "/") || strings.HasSuffix(img.Name, "/") {
			t.Errorf("%s: the name and the tag must not carry ':', '@', a space or an edge slash", id)
		}
		if img.Location != wantHosted && img.Location != wantUpstream {
			t.Errorf("%s: location %q is neither %q nor %q", id, img.Location, wantHosted, wantUpstream)
		}
		if img.PullThrough && img.Location != wantUpstream {
			t.Errorf("%s: only an upstream image can be pulled through the proxy", id)
		}
		if seen[id] {
			t.Errorf("%s appears twice", id)
		}
		seen[id] = true
	}
}

// TestDatasetDigestsAreFilledFromTheGenerator: Dataset() carries the digest the
// generator computes, and it is a well-formed, distinct sha256 for every image.
func TestDatasetDigestsAreFilledFromTheGenerator(t *testing.T) {
	seen := map[string]string{}
	for _, img := range Dataset() {
		id := describeImage(img)
		if !digestForm.MatchString(img.Digest) {
			t.Errorf("%s: digest %q is not sha256:<64 hex>", id, img.Digest)
			continue
		}
		if want := imageDigest(withoutDigest([]DatasetImage{img})[0]); img.Digest != want {
			t.Errorf("%s: Digest = %s, imageDigest = %s", id, img.Digest, want)
		}
		if other, dup := seen[img.Digest]; dup {
			t.Errorf("%s and %s share the digest %s", id, other, img.Digest)
		}
		seen[img.Digest] = id
	}
	if len(seen) == 0 {
		t.Error("Dataset() holds no digest")
	}
}

// TestDatasetSharedBaseDiffersBetweenLocations: the same name and tag in the
// hosted repository and upstream hold different contents (D-I, CA5), so the
// group read says which member answered.
func TestDatasetSharedBaseDiffersBetweenLocations(t *testing.T) {
	var hosted, upstream []DatasetImage
	for _, img := range Dataset() {
		if img.Name == "shared/base" && img.Tag == "1.0" {
			if img.Location == wantHosted {
				hosted = append(hosted, img)
			} else {
				upstream = append(upstream, img)
			}
		}
	}
	if len(hosted) != 1 || len(upstream) != 1 {
		t.Fatalf("shared/base:1.0 is held %d time(s) hosted and %d time(s) upstream, want once each", len(hosted), len(upstream))
	}
	if hosted[0].Digest == "" || hosted[0].Digest == upstream[0].Digest {
		t.Errorf("the two shared/base:1.0 digests are %q (hosted) and %q (upstream), want two different digests", hosted[0].Digest, upstream[0].Digest)
	}
	if a, b := imageDigest(withoutDigest(hosted)[0]), imageDigest(withoutDigest(upstream)[0]); a == b {
		t.Errorf("imageDigest gives %s for both shared/base:1.0", a)
	}
}

// TestDatasetCoversTheAcceptanceCriteria: the guards a later edit of the table
// must not break.
func TestDatasetCoversTheAcceptanceCriteria(t *testing.T) {
	images := Dataset()

	t.Run("CA1 at least three hosted images, among them api and team/api", func(t *testing.T) {
		hosted := map[string]bool{}
		count := 0
		for _, img := range images {
			if img.Location == wantHosted {
				count++
				hosted[img.Name] = true
			}
		}
		if count < 3 {
			t.Errorf("%d hosted images, want at least 3", count)
		}
		for _, name := range []string{"api", "team/api"} {
			if !hosted[name] {
				t.Errorf("no hosted image named %q", name)
			}
		}
	})

	t.Run("CA3 one upstream image is pulled through, one is not", func(t *testing.T) {
		var through, notThrough int
		for _, img := range images {
			if img.Location != wantUpstream {
				continue
			}
			if img.PullThrough {
				through++
			} else {
				notThrough++
			}
		}
		if through < 1 || notThrough < 1 {
			t.Errorf("upstream images pulled through = %d, not pulled through = %d, want at least one of each", through, notThrough)
		}
	})

	t.Run("CA5 one name and tag live in both locations", func(t *testing.T) {
		where := map[string]map[Location]bool{}
		for _, img := range images {
			ref := img.Name + ":" + img.Tag
			if where[ref] == nil {
				where[ref] = map[Location]bool{}
			}
			where[ref][img.Location] = true
		}
		for _, locs := range where {
			if locs[wantHosted] && locs[wantUpstream] {
				return
			}
		}
		t.Errorf("no name:tag is held both hosted and upstream: %v", where)
	})
}

// datasetGolden renders the data set the way the golden file holds it.
func datasetGolden(images []DatasetImage) string {
	var b strings.Builder
	for _, img := range images {
		fmt.Fprintf(&b, "%s:%s\t%s\t%s\n", img.Name, img.Tag, img.Location, img.Digest)
	}
	return b.String()
}

// TestDatasetDigestsGolden pins the manifest digest of every image: a change of
// the generator (layout, tar, config, labels, contents) moves them, and Nexus,
// the upstream and the descriptor all depend on them being stable.
//
// After a deliberate change of the generator, refresh the file:
//
//	go test ./internal/nexusbench -run TestDatasetDigestsGolden -update
//
// then read the diff: it is the list of digests every later step compares to.
func TestDatasetDigestsGolden(t *testing.T) {
	images := Dataset()
	if len(images) == 0 {
		t.Fatal("Dataset() is empty: nothing to pin")
	}
	got := datasetGolden(images)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenDatasetDigests), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenDatasetDigests, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenDatasetDigests)
	if err != nil {
		t.Fatalf("%v\ngenerate it with: go test ./internal/nexusbench -run TestDatasetDigestsGolden -update", err)
	}
	if got != string(want) {
		t.Errorf("the data set digests differ from %s (run with -update and review the diff)\n--- got ---\n%s--- want ---\n%s", goldenDatasetDigests, got, want)
	}
}
