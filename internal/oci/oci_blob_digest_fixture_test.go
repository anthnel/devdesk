package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Fixtures of DEVDESK-412 step 5 (the blob is checked against the digest of the
// manifest). They live apart from the tests so that package-level fixtures such
// as clientTestDigest can be computed without a *testing.T.

// fixtureTarGz returns a gzipped tar holding one file, Dockerfile, with body. The
// output is deterministic (gzip header without a modification time).
func fixtureTarGz(body string) []byte {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	if err := tw.WriteHeader(&tar.Header{Name: "Dockerfile", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		panic(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		panic(err)
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}
	if err := gzw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// clientFixtureArchive is the layer the registry fixtures serve by default.
func clientFixtureArchive() []byte {
	return fixtureTarGz("FROM scratch\n")
}

// sha256Digest is the OCI digest of b: "sha256:" and 64 lowercase hex digits.
func sha256Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// blobManifestFor is the manifest of tpl:v1 whose one layer has the given digest,
// whatever it looks like (the JSON is encoded, so any string is representable).
func blobManifestFor(digest string) string {
	out, err := json.Marshal(map[string]any{
		"layers": []map[string]string{{
			"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
			"digest":    digest,
		}},
	})
	if err != nil {
		panic(err)
	}
	return string(out)
}
