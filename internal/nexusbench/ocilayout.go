package nexusbench

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// layerGzip is the H-LAYER fallback switch. false writes the layer as a plain tar
// (application/vnd.oci.image.layer.v1.tar); true compresses it with gzip
// (application/vnd.oci.image.layer.v1.tar+gzip), for a Nexus that refuses an
// uncompressed layer. Nothing else changes: the diff id stays the digest of the
// tar. With gzip the digests depend on the compress/gzip output of the Go version,
// so the golden has to be regenerated with a Go upgrade.
const layerGzip = false

const (
	mediaTypeManifest  = "application/vnd.oci.image.manifest.v1+json"
	mediaTypeConfig    = "application/vnd.oci.image.config.v1+json"
	mediaTypeLayerTar  = "application/vnd.oci.image.layer.v1.tar"
	mediaTypeLayerGzip = "application/vnd.oci.image.layer.v1.tar+gzip"

	annotationRefName = "org.opencontainers.image.ref.name"
	labelSource       = "org.devdesk.nexus-bench.source"

	layoutVersion = "1.0.0"
	layerFileMode = 0o644
	layerDirMode  = 0o755
)

// ociDescriptorJSON is an OCI content descriptor.
type ociDescriptorJSON struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

type ociManifestJSON struct {
	SchemaVersion int                 `json:"schemaVersion"`
	MediaType     string              `json:"mediaType"`
	Config        ociDescriptorJSON   `json:"config"`
	Layers        []ociDescriptorJSON `json:"layers"`
}

type ociIndexJSON struct {
	SchemaVersion int                 `json:"schemaVersion"`
	MediaType     string              `json:"mediaType"`
	Manifests     []ociDescriptorJSON `json:"manifests"`
}

// ociImageConfigJSON is the image configuration. It has no "created" key: a date
// would make the digest depend on the clock.
type ociImageConfigJSON struct {
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

// builtImage is an image with every blob it needs, in memory.
type builtImage struct {
	manifest ociDescriptorJSON
	blobs    []blob // manifest, config, layer
}

type blob struct {
	digest string
	data   []byte
}

// WriteLayout writes an OCI image layout holding every image of images under dir
// (which must exist and be empty) and returns the reference name of each image in
// the layout, in the same order. The bytes depend on the images alone.
func WriteLayout(dir string, images []DatasetImage) (refs []string, err error) {
	if err := requireEmptyDir(dir); err != nil {
		return nil, err
	}
	// Build everything first: a failure writes nothing.
	index := ociIndexJSON{SchemaVersion: 2, MediaType: "application/vnd.oci.image.index.v1+json"}
	var blobs []blob
	for i, img := range images {
		built, err := buildImage(img)
		if err != nil {
			return nil, fmt.Errorf("build %s:%s: %w", img.Name, img.Tag, err)
		}
		ref := fmt.Sprintf("img-%02d", i+1)
		desc := built.manifest
		desc.Annotations = map[string]string{annotationRefName: ref}
		index.Manifests = append(index.Manifests, desc)
		blobs = append(blobs, built.blobs...)
		refs = append(refs, ref)
	}
	indexJSON, err := json.Marshal(index)
	if err != nil {
		return nil, fmt.Errorf("encode index.json: %w", err)
	}

	if err := writeLayoutFiles(dir, blobs, indexJSON); err != nil {
		return nil, err
	}
	return refs, nil
}

// imageDigest returns the manifest digest of img, computed without writing anything.
func imageDigest(img DatasetImage) string {
	built, err := buildImage(img)
	if err != nil {
		// Only an unmarshalable value or a tar header refusal can fail, none of
		// which depends on the image: the table is fixed.
		panic(fmt.Sprintf("nexusbench: build %s:%s: %v", img.Name, img.Tag, err))
	}
	return built.manifest.Digest
}

func requireEmptyDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read layout directory: %w", err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("layout directory %s is not empty", dir)
	}
	return nil
}

func writeLayoutFiles(dir string, blobs []blob, indexJSON []byte) error {
	blobDir := filepath.Join(dir, "blobs", "sha256")
	if err := os.MkdirAll(blobDir, 0o755); err != nil {
		return fmt.Errorf("create blobs directory: %w", err)
	}
	for _, b := range blobs {
		_, hexa, _ := strings.Cut(b.digest, ":")
		if err := os.WriteFile(filepath.Join(blobDir, hexa), b.data, 0o644); err != nil {
			return fmt.Errorf("write blob %s: %w", b.digest, err)
		}
	}
	marker := []byte(`{"imageLayoutVersion":"` + layoutVersion + `"}`)
	if err := os.WriteFile(filepath.Join(dir, "oci-layout"), marker, 0o644); err != nil {
		return fmt.Errorf("write oci-layout: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.json"), indexJSON, 0o644); err != nil {
		return fmt.Errorf("write index.json: %w", err)
	}
	return nil
}

// sourceName is the value of the source label and the folder under /bench.
func sourceName(l Location) string {
	if l == LocationHosted {
		return "hosted"
	}
	return "upstream"
}

// buildImage builds the one-layer image of img: manifest, config and layer.
func buildImage(img DatasetImage) (builtImage, error) {
	source := sourceName(img.Location)
	tarBytes, err := buildTar(img, source)
	if err != nil {
		return builtImage{}, err
	}
	layerBytes, layerType := tarBytes, mediaTypeLayerTar
	if layerGzip {
		if layerBytes, err = gzipBytes(tarBytes); err != nil {
			return builtImage{}, err
		}
		layerType = mediaTypeLayerGzip
	}

	var cfg ociImageConfigJSON
	cfg.Architecture, cfg.OS = "amd64", "linux"
	cfg.Config.Labels = map[string]string{labelSource: source}
	cfg.RootFS.Type = "layers"
	cfg.RootFS.DiffIDs = []string{digestOf(tarBytes)}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		return builtImage{}, fmt.Errorf("encode config: %w", err)
	}

	layer := blob{digestOf(layerBytes), layerBytes}
	config := blob{digestOf(cfgBytes), cfgBytes}
	manifest := ociManifestJSON{
		SchemaVersion: 2,
		MediaType:     mediaTypeManifest,
		Config:        ociDescriptorJSON{MediaType: mediaTypeConfig, Digest: config.digest, Size: int64(len(config.data))},
		Layers:        []ociDescriptorJSON{{MediaType: layerType, Digest: layer.digest, Size: int64(len(layer.data))}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return builtImage{}, fmt.Errorf("encode manifest: %w", err)
	}
	m := blob{digestOf(manifestBytes), manifestBytes}
	return builtImage{
		manifest: ociDescriptorJSON{MediaType: mediaTypeManifest, Digest: m.digest, Size: int64(len(m.data))},
		blobs:    []blob{m, config, layer},
	}, nil
}

// buildTar writes the layer: /bench/<source>/<name>-<tag>.txt and its two parent
// folders, sorted, without date, owner or name, with fixed modes.
func buildTar(img DatasetImage, source string) ([]byte, error) {
	file := strings.ReplaceAll(img.Name, "/", "-") + "-" + img.Tag + ".txt"
	content := fmt.Sprintf("nexus-bench data set\nsource: %s\nimage: %s:%s\n", source, img.Name, img.Tag)
	entries := []struct {
		name string
		dir  bool
		body string
	}{
		{name: "bench/", dir: true},
		{name: "bench/" + source + "/", dir: true},
		{name: "bench/" + source + "/" + file, body: content},
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}
		if e.dir {
			h.Typeflag, h.Mode = tar.TypeDir, layerDirMode
		} else {
			h.Typeflag, h.Mode, h.Size = tar.TypeReg, layerFileMode, int64(len(e.body))
		}
		if err := tw.WriteHeader(h); err != nil {
			return nil, fmt.Errorf("write tar header %s: %w", e.name, err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			return nil, fmt.Errorf("write tar entry %s: %w", e.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close tar: %w", err)
	}
	return buf.Bytes(), nil
}

// gzipBytes compresses data with a header that carries no name and no date.
func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return nil, errors.Join(errors.New("compress layer"), err)
	}
	if err := zw.Close(); err != nil {
		return nil, errors.Join(errors.New("compress layer"), err)
	}
	return buf.Bytes(), nil
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
