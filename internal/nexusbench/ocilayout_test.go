package nexusbench

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The tests below pin the OCI image layout the bench writes before it copies the
// data set with skopeo (DEVDESK-505, step 3): the bytes depend on the images
// alone, every blob is what index.json says it is, and nothing in the layer or
// the config carries a date, a user or a compression.

const (
	mediaIndex    = "application/vnd.oci.image.index.v1+json"
	mediaManifest = "application/vnd.oci.image.manifest.v1+json"
	mediaConfig   = "application/vnd.oci.image.config.v1+json"
	mediaLayerTar = "application/vnd.oci.image.layer.v1.tar"

	refNameAnnotation = "org.opencontainers.image.ref.name"
	sourceLabel       = "org.devdesk.nexus-bench.source"
)

var refForm = regexp.MustCompile(`^img-[0-9]{2,}$`)

type ociDescriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations"`
}

type ociIndex struct {
	SchemaVersion int             `json:"schemaVersion"`
	MediaType     string          `json:"mediaType"`
	Manifests     []ociDescriptor `json:"manifests"`
}

type ociManifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	MediaType     string          `json:"mediaType"`
	Config        ociDescriptor   `json:"config"`
	Layers        []ociDescriptor `json:"layers"`
}

type ociConfig struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Config       struct {
		Labels map[string]string `json:"Labels"`
	} `json:"config"`
	RootFS struct {
		Type    string   `json:"type"`
		DiffIDs []string `json:"diff_ids"`
	} `json:"rootfs"`
}

// writeDatasetLayout writes the images of D-I (wantDataset, so that the
// generator is tested whatever Dataset() returns) into a fresh directory.
func writeDatasetLayout(t *testing.T) (dir string, images []DatasetImage, refs []string) {
	t.Helper()
	images = append([]DatasetImage(nil), wantDataset...)
	dir = t.TempDir()
	refs, err := WriteLayout(dir, images)
	if err != nil {
		t.Fatalf("WriteLayout: %v", err)
	}
	return dir, images, refs
}

// readTree returns every regular file under dir, by slash-separated relative path.
func readTree(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func sha256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// blobPath is where an OCI layout keeps the blob of a digest.
func blobPath(digest string) string {
	algo, hexa, _ := strings.Cut(digest, ":")
	return "blobs/" + algo + "/" + hexa
}

func decodeJSON(t *testing.T, name string, data []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("%s is not valid JSON: %v\n%s", name, err, data)
	}
}

// blobOf returns the blob a descriptor points at, after checking that it exists
// and that its size and sha256 are the ones the descriptor states.
func blobOf(t *testing.T, tree map[string][]byte, d ociDescriptor) []byte {
	t.Helper()
	if !digestForm.MatchString(d.Digest) {
		t.Fatalf("descriptor digest %q is not sha256:<64 hex>", d.Digest)
	}
	data, ok := tree[blobPath(d.Digest)]
	if !ok {
		t.Fatalf("the layout holds no blob %s for %s", blobPath(d.Digest), d.MediaType)
	}
	if int64(len(data)) != d.Size {
		t.Errorf("blob %s is %d bytes, its descriptor says %d", d.Digest, len(data), d.Size)
	}
	if got := sha256Of(data); got != d.Digest {
		t.Errorf("blob %s hashes to %s", d.Digest, got)
	}
	return data
}

// layoutIndex reads and decodes index.json of a written layout.
func layoutIndex(t *testing.T, tree map[string][]byte) ociIndex {
	t.Helper()
	raw, ok := tree["index.json"]
	if !ok {
		t.Fatal("the layout has no index.json")
	}
	var idx ociIndex
	decodeJSON(t, "index.json", raw, &idx)
	return idx
}

func TestWriteLayoutIsDeterministic(t *testing.T) {
	images := wantDataset
	dirA, dirB := t.TempDir(), t.TempDir()
	refsA, err := WriteLayout(dirA, images)
	if err != nil {
		t.Fatalf("first WriteLayout: %v", err)
	}
	refsB, err := WriteLayout(dirB, images)
	if err != nil {
		t.Fatalf("second WriteLayout: %v", err)
	}
	if !reflect.DeepEqual(refsA, refsB) {
		t.Errorf("references differ between two calls: %v and %v", refsA, refsB)
	}
	a, b := readTree(t, dirA), readTree(t, dirB)
	if len(a) == 0 {
		t.Fatal("the layout is empty")
	}
	if len(a) != len(b) {
		t.Errorf("%d files then %d files", len(a), len(b))
	}
	for name, data := range a {
		other, ok := b[name]
		if !ok {
			t.Errorf("%s is missing from the second layout", name)
		} else if !bytes.Equal(data, other) {
			t.Errorf("%s differs between two calls", name)
		}
	}
}

// The bytes depend on the images alone: the Digest and PullThrough fields are
// results and seed behavior, not content.
func TestWriteLayoutIgnoresDigestAndPullThrough(t *testing.T) {
	images := withoutDigest(wantDataset)
	for i := range images {
		images[i].Digest = "sha256:" + strings.Repeat("0", 64)
	}
	blank := withoutDigest(wantDataset)
	for i := range blank {
		blank[i].PullThrough = !blank[i].PullThrough
	}
	dirA, dirB := t.TempDir(), t.TempDir()
	if _, err := WriteLayout(dirA, images); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteLayout(dirB, blank); err != nil {
		t.Fatal(err)
	}
	if a, b := readTree(t, dirA), readTree(t, dirB); !reflect.DeepEqual(a, b) {
		t.Error("the layout changes with the Digest or PullThrough fields of its input")
	}
}

// Content depends on the image alone: a layout holding one image gives that
// image the digest it has in the full layout and the one imageDigest computes.
func TestWriteLayoutDigestOfOneImageDoesNotDependOnTheOthers(t *testing.T) {
	fullDir, full, _ := writeDatasetLayout(t)
	fullIdx := layoutIndex(t, readTree(t, fullDir))
	if len(fullIdx.Manifests) != len(full) {
		t.Fatalf("%d manifests for %d images", len(fullIdx.Manifests), len(full))
	}
	for i, img := range full {
		t.Run(describeImage(img), func(t *testing.T) {
			dir := t.TempDir()
			refs, err := WriteLayout(dir, []DatasetImage{img})
			if err != nil {
				t.Fatal(err)
			}
			if len(refs) != 1 {
				t.Fatalf("%d references for one image", len(refs))
			}
			idx := layoutIndex(t, readTree(t, dir))
			if len(idx.Manifests) != 1 {
				t.Fatalf("%d manifests for one image", len(idx.Manifests))
			}
			got := idx.Manifests[0].Digest
			if want := fullIdx.Manifests[i].Digest; got != want {
				t.Errorf("manifest digest alone = %s, in the full layout = %s", got, want)
			}
			if want := imageDigest(img); got != want {
				t.Errorf("manifest digest = %s, imageDigest = %s", got, want)
			}
		})
	}
}

func TestWriteLayoutHasTheOCILayoutFile(t *testing.T) {
	dir, _, _ := writeDatasetLayout(t)
	raw, ok := readTree(t, dir)["oci-layout"]
	if !ok {
		t.Fatal("the layout has no oci-layout file")
	}
	var marker struct {
		ImageLayoutVersion string `json:"imageLayoutVersion"`
	}
	decodeJSON(t, "oci-layout", raw, &marker)
	if marker.ImageLayoutVersion != "1.0.0" {
		t.Errorf("imageLayoutVersion = %q, want 1.0.0", marker.ImageLayoutVersion)
	}
}

// TestWriteLayoutIndex: index.json lists one OCI v1 manifest per image, in input
// order, named img-NN (a ':' would break skopeo's oci:<dir>:<ref> syntax), and
// every blob it reaches exists with the size and sha256 it states.
func TestWriteLayoutIndex(t *testing.T) {
	dir, images, refs := writeDatasetLayout(t)
	tree := readTree(t, dir)
	idx := layoutIndex(t, tree)

	if idx.SchemaVersion != 2 {
		t.Errorf("index schemaVersion = %d, want 2", idx.SchemaVersion)
	}
	if idx.MediaType != "" && idx.MediaType != mediaIndex {
		t.Errorf("index mediaType = %q, want %q", idx.MediaType, mediaIndex)
	}
	if len(idx.Manifests) != len(images) || len(refs) != len(images) {
		t.Fatalf("%d manifests and %d references for %d images", len(idx.Manifests), len(refs), len(images))
	}

	seenRefs := map[string]bool{}
	reachable := map[string]bool{}
	for i, d := range idx.Manifests {
		id := describeImage(images[i])
		if d.MediaType != mediaManifest {
			t.Errorf("%s: manifest mediaType = %q, want %q", id, d.MediaType, mediaManifest)
		}
		ref := d.Annotations[refNameAnnotation]
		if !refForm.MatchString(ref) {
			t.Errorf("%s: %s = %q, want img-NN", id, refNameAnnotation, ref)
		}
		if strings.Contains(ref, ":") {
			t.Errorf("%s: reference %q contains ':'", id, ref)
		}
		if refs[i] != ref {
			t.Errorf("%s: WriteLayout returned %q, index.json names it %q", id, refs[i], ref)
		}
		if seenRefs[ref] {
			t.Errorf("reference %q is used twice", ref)
		}
		seenRefs[ref] = true
		if want := imageDigest(images[i]); d.Digest != want {
			t.Errorf("%s: manifest digest %s, imageDigest %s", id, d.Digest, want)
		}

		raw := blobOf(t, tree, d)
		reachable[blobPath(d.Digest)] = true
		var m ociManifest
		decodeJSON(t, "manifest of "+id, raw, &m)
		if m.SchemaVersion != 2 {
			t.Errorf("%s: manifest schemaVersion = %d, want 2", id, m.SchemaVersion)
		}
		if m.MediaType != "" && m.MediaType != mediaManifest {
			t.Errorf("%s: manifest mediaType field = %q, want %q", id, m.MediaType, mediaManifest)
		}
		if m.Config.MediaType != mediaConfig {
			t.Errorf("%s: config mediaType = %q, want %q", id, m.Config.MediaType, mediaConfig)
		}
		blobOf(t, tree, m.Config)
		reachable[blobPath(m.Config.Digest)] = true
		if len(m.Layers) == 0 {
			t.Errorf("%s: no layer", id)
		}
		for _, l := range m.Layers {
			blobOf(t, tree, l)
			reachable[blobPath(l.Digest)] = true
		}
	}

	// Nothing but the marker, the index and the blobs they reach.
	for name := range tree {
		switch {
		case name == "oci-layout" || name == "index.json":
		case strings.HasPrefix(name, "blobs/sha256/"):
			if !reachable[name] {
				t.Errorf("%s is not reachable from index.json", name)
			}
		default:
			t.Errorf("unexpected file %s in the layout", name)
		}
	}
}

// layerOf returns the single layer blob of the image at position i of the index.
func layerOf(t *testing.T, tree map[string][]byte, idx ociIndex, i int) (ociManifest, []byte) {
	t.Helper()
	var m ociManifest
	decodeJSON(t, "manifest", tree[blobPath(idx.Manifests[i].Digest)], &m)
	if len(m.Layers) != 1 {
		t.Fatalf("image %d has %d layers, want 1", i, len(m.Layers))
	}
	return m, tree[blobPath(m.Layers[0].Digest)]
}

// sourceOf is the value of the source label and of the /bench/<source>/ folder.
func sourceOf(l Location) string {
	if l == wantHosted {
		return "hosted"
	}
	return "upstream"
}

// TestWriteLayoutLayerIsAnUncompressedReproducibleTar: the layer is a plain tar
// (H-LAYER keeps gzip as the fallback), entries sorted, no date, root owner, and
// it holds the image's /bench/<source>/<name>-<tag>.txt.
func TestWriteLayoutLayerIsAnUncompressedReproducibleTar(t *testing.T) {
	dir, images, _ := writeDatasetLayout(t)
	tree := readTree(t, dir)
	idx := layoutIndex(t, tree)
	if len(idx.Manifests) != len(images) {
		t.Fatalf("%d manifests for %d images", len(idx.Manifests), len(images))
	}

	for i, img := range images {
		t.Run(describeImage(img), func(t *testing.T) {
			m, layer := layerOf(t, tree, idx, i)
			if m.Layers[0].MediaType != mediaLayerTar {
				t.Errorf("layer mediaType = %q, want %q", m.Layers[0].MediaType, mediaLayerTar)
			}
			if bytes.HasPrefix(layer, []byte{0x1f, 0x8b}) {
				t.Error("the layer starts with the gzip magic number")
			}

			var names []string
			var fileModes []int64
			var files []string
			tr := tar.NewReader(bytes.NewReader(layer))
			for {
				h, err := tr.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatalf("the layer is not a tar archive: %v", err)
				}
				names = append(names, h.Name)
				if h.ModTime.Unix() != 0 || !h.AccessTime.IsZero() || !h.ChangeTime.IsZero() {
					t.Errorf("%s: carries a date (mtime %v, atime %v, ctime %v)", h.Name, h.ModTime, h.AccessTime, h.ChangeTime)
				}
				if h.Uid != 0 || h.Gid != 0 {
					t.Errorf("%s: owner %d:%d, want 0:0", h.Name, h.Uid, h.Gid)
				}
				if h.Uname != "" || h.Gname != "" {
					t.Errorf("%s: owner names %q:%q, want none", h.Name, h.Uname, h.Gname)
				}
				if h.Typeflag == tar.TypeReg {
					fileModes = append(fileModes, h.Mode)
					files = append(files, strings.TrimPrefix(h.Name, "/"))
				}
			}
			if !sort.StringsAreSorted(names) {
				t.Errorf("entries are not sorted: %v", names)
			}
			for _, mode := range fileModes {
				if mode != fileModes[0] {
					t.Errorf("regular files have different modes: %v", fileModes)
					break
				}
			}

			// One file under /bench/<source>/, named after the image.
			prefix := "bench/" + sourceOf(img.Location) + "/"
			var found []string
			for _, f := range files {
				if strings.HasPrefix(f, prefix) {
					found = append(found, f)
				}
			}
			if len(found) != 1 {
				t.Fatalf("regular files under /%s: %v, want exactly one (all files: %v)", prefix, found, files)
			}
			if !strings.HasSuffix(found[0], "-"+img.Tag+".txt") {
				t.Errorf("%s does not end with -%s.txt", found[0], img.Tag)
			}
			rest := strings.TrimPrefix(found[0], prefix)
			if !strings.Contains(rest, img.Name) && !strings.Contains(rest, strings.ReplaceAll(img.Name, "/", "-")) {
				t.Errorf("%s does not name the image %s", found[0], img.Name)
			}
		})
	}
}

// TestWriteLayoutConfigKeyIsLabelsAsTheImageSpecSpellsIt reads the raw config
// JSON: Go decodes keys without regard to case, so only the bytes can tell that
// the key is the OCI image-spec "Labels" and not "labels", which Nexus and every
// other registry tool would not read as the labels of the image.
func TestWriteLayoutConfigKeyIsLabelsAsTheImageSpecSpellsIt(t *testing.T) {
	dir, images, _ := writeDatasetLayout(t)
	tree := readTree(t, dir)
	idx := layoutIndex(t, tree)
	for i, img := range images {
		t.Run(describeImage(img), func(t *testing.T) {
			m, _ := layerOf(t, tree, idx, i)
			var cfg map[string]any
			decodeJSON(t, "config", tree[blobPath(m.Config.Digest)], &cfg)
			inner, ok := cfg["config"].(map[string]any)
			if !ok {
				t.Fatalf("the config has no config object: %s", tree[blobPath(m.Config.Digest)])
			}
			if _, ok := inner["Labels"]; !ok {
				t.Errorf(`the config object has no "Labels" key: %v`, inner)
			}
			if _, ok := inner["labels"]; ok {
				t.Errorf(`the config object has a lower case "labels" key: %v`, inner)
			}
			labels, _ := inner["Labels"].(map[string]any)
			if got, want := labels[sourceLabel], sourceOf(img.Location); got != want {
				t.Errorf("Labels[%s] = %v, want %q", sourceLabel, got, want)
			}
		})
	}
}

// TestGzipBytes covers the H-LAYER fallback, which no image uses while the
// layers stay uncompressed: the same input gives the same bytes (no name, no
// date in the header), and the output decompresses to the input.
func TestGzipBytes(t *testing.T) {
	inputs := map[string][]byte{
		"empty": {},
		"small": []byte("nexus-bench data set\n"),
		"large": bytes.Repeat([]byte("0123456789abcdef"), 10_000),
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			first, err := gzipBytes(in)
			if err != nil {
				t.Fatalf("gzipBytes: %v", err)
			}
			second, err := gzipBytes(in)
			if err != nil {
				t.Fatalf("gzipBytes: %v", err)
			}
			if !bytes.Equal(first, second) {
				t.Error("the same input gave two different outputs")
			}
			if len(first) < 10 || first[0] != 0x1f || first[1] != 0x8b {
				t.Fatalf("the output is not gzip: % x", first[:min(len(first), 10)])
			}
			if first[3] != 0 {
				t.Errorf("header flags = %#x, want 0: no file name, no comment, no extra field", first[3])
			}
			if !bytes.Equal(first[4:8], []byte{0, 0, 0, 0}) {
				t.Errorf("header mtime = % x, want zero", first[4:8])
			}
			zr, err := gzip.NewReader(bytes.NewReader(first))
			if err != nil {
				t.Fatalf("gzip.NewReader: %v", err)
			}
			got, err := io.ReadAll(zr)
			if err != nil {
				t.Fatalf("decompress: %v", err)
			}
			if !bytes.Equal(got, in) {
				t.Errorf("the output decompresses to %d bytes, want the %d bytes of the input", len(got), len(in))
			}
		})
	}
}

// TestWriteLayoutConfig: linux/amd64, no creation date, the source label, and
// the diff id of the one uncompressed layer is the layer digest.
func TestWriteLayoutConfig(t *testing.T) {
	dir, images, _ := writeDatasetLayout(t)
	tree := readTree(t, dir)
	idx := layoutIndex(t, tree)
	if len(idx.Manifests) != len(images) {
		t.Fatalf("%d manifests for %d images", len(idx.Manifests), len(images))
	}

	for i, img := range images {
		t.Run(describeImage(img), func(t *testing.T) {
			m, _ := layerOf(t, tree, idx, i)
			raw := tree[blobPath(m.Config.Digest)]

			var cfg ociConfig
			decodeJSON(t, "config", raw, &cfg)
			if cfg.Architecture != "amd64" || cfg.OS != "linux" {
				t.Errorf("platform = %s/%s, want linux/amd64", cfg.OS, cfg.Architecture)
			}
			if bytes.Contains(raw, []byte(`"created"`)) {
				t.Errorf("the config carries a created date (a key named created):\n%s", raw)
			}
			if got, want := cfg.Config.Labels[sourceLabel], sourceOf(img.Location); got != want {
				t.Errorf("label %s = %q, want %q", sourceLabel, got, want)
			}
			if cfg.RootFS.Type != "layers" {
				t.Errorf("rootfs.type = %q, want layers", cfg.RootFS.Type)
			}
			if want := []string{m.Layers[0].Digest}; !reflect.DeepEqual(cfg.RootFS.DiffIDs, want) {
				t.Errorf("rootfs.diff_ids = %v, want %v (the layer is not compressed)", cfg.RootFS.DiffIDs, want)
			}
		})
	}
}

// TestWriteLayoutRefusesADirectoryThatIsNotEmpty: the layout is written into an
// empty directory and nowhere else, and a refusal writes nothing.
func TestWriteLayoutRefusesADirectoryThatIsNotEmpty(t *testing.T) {
	images := wantDataset

	t.Run("a file already there", func(t *testing.T) {
		dir := t.TempDir()
		keep := filepath.Join(dir, "keep.txt")
		if err := os.WriteFile(keep, []byte("mine"), 0o644); err != nil {
			t.Fatal(err)
		}
		refs, err := WriteLayout(dir, images)
		if err == nil {
			t.Fatalf("WriteLayout into a non-empty directory succeeded with %v", refs)
		}
		if refs != nil {
			t.Errorf("references %v returned with the error", refs)
		}
		tree := readTree(t, dir)
		if len(tree) != 1 || string(tree["keep.txt"]) != "mine" {
			t.Errorf("the refusal changed the directory: %v", tree)
		}
	})

	t.Run("a second layout over the first", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := WriteLayout(dir, images); err != nil {
			t.Fatalf("first WriteLayout: %v", err)
		}
		if _, err := WriteLayout(dir, images); err == nil {
			t.Error("WriteLayout over an existing layout succeeded")
		}
	})

	t.Run("a directory that does not exist", func(t *testing.T) {
		if _, err := WriteLayout(filepath.Join(t.TempDir(), "missing"), images); err == nil {
			t.Error("WriteLayout into a missing directory succeeded")
		}
	})
}
