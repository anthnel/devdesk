package oci

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"testing"
	"unicode"
)

// DEVDESK-412 step 5 (decision Q5 = a): the blob is ALWAYS checked against the
// digest the manifest names, same origin or redirected. The digest is validated
// ("sha256:" and 64 lowercase hex digits, nothing else) before any blob request;
// the compressed stream is hashed as received, and what the tar reader leaves
// unread is drained (under blobCap) and hashed too. A mismatch returns
// ErrBlobDigestMismatch and no entry. Server A is the registry, B the host the
// blob is redirected to.

// hexOf is the 64 hex digits of a "sha256:<hex>" digest.
func hexOf(digest string) string { return strings.TrimPrefix(digest, "sha256:") }

// wantNoDigestOrURL fails when err repeats a complete digest, a URL or a control
// character. A short prefix of a digest would be fine; a full one is noise at best.
func wantNoDigestOrURL(t *testing.T, err error, digests ...string) {
	t.Helper()
	wantNoLeak(t, err, "http://", "https://", "/blob")
	for _, d := range digests {
		if h := hexOf(d); strings.Contains(err.Error(), h) {
			t.Errorf("error %q repeats a complete digest (%s)", err, h)
		}
	}
	for _, r := range err.Error() {
		if unicode.IsControl(r) {
			t.Errorf("error %q holds the control character %U", err, r)
		}
	}
}

// redirectedBlob starts A (manifest naming digest, blob redirected) and B (serves
// served for every request), and returns a client for A with both logs.
func redirectedBlob(t *testing.T, digest string, served []byte) (c *Client, aHits, bHits *hitLog) {
	t.Helper()
	aHits, bHits = &hitLog{}, &hitLog{}
	b := archiveHost(t, false, bHits, served)
	a := blobRegistryFor(t, false, aHits, digest, redirectTo(b.URL+"/blob?"+blobSignedQuery))
	return NewClient(a.URL, clientTestUser, clientTestPass), aHits, bHits
}

func TestParseLayerDigest(t *testing.T) {
	good := strings.Repeat("0123456789abcdef", 4)
	for _, tc := range []struct {
		name, in string
		wantHex  string // "" = an error is expected
	}{
		{"well formed", "sha256:" + good, good},
		{"all zeros", "sha256:" + strings.Repeat("0", 64), strings.Repeat("0", 64)},
		{"empty", "", ""},
		{"prefix only", "sha256:", ""},
		{"no prefix", good, ""},
		{"sha512", "sha512:" + strings.Repeat("a", 128), ""},
		{"sha512 with a 64 digit value", "sha512:" + good, ""},
		{"upper case algorithm", "SHA256:" + good, ""},
		{"upper case hex", "sha256:" + strings.ToUpper(good), ""},
		{"one upper case digit", "sha256:A" + good[1:], ""},
		{"63 digits", "sha256:" + good[:63], ""},
		{"65 digits", "sha256:" + good + "a", ""},
		{"non hex digit", "sha256:" + good[:63] + "g", ""},
		{"path traversal", "sha256:../x", ""},
		{"path traversal inside the digest", "sha256:" + good[:55] + "/../../x", ""},
		{"slash", "sha256:" + good[:63] + "/", ""},
		{"trailing newline", "sha256:" + good + "\n", ""},
		{"leading space", " sha256:" + good, ""},
		{"query", "sha256:" + good + "?x=1", ""},
		{"escape character", "sha256:\x1b[31m" + good[:56], ""},
		{"second colon", "sha256:sha256:" + good, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLayerDigest(tc.in)
			if tc.wantHex == "" {
				if err == nil {
					t.Fatalf("parseLayerDigest(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if err != nil || got != tc.wantHex {
				t.Fatalf("parseLayerDigest(%q) = %q, %v, want %q", tc.in, got, err, tc.wantHex)
			}
		})
	}
}

// A well-formed digest that matches is the baseline: same origin and redirected.
func TestBlobDigestMatchIsAccepted(t *testing.T) {
	archive := clientFixtureArchive()

	t.Run("same origin", func(t *testing.T) {
		var aHits hitLog
		a := blobRegistryFor(t, false, &aHits, sha256Digest(archive), func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(archive)
		})
		tpl, err := NewClient(a.URL, clientTestUser, clientTestPass).DownloadTemplate("tpl", "v1")
		wantFixtureEntries(t, tpl, err)
	})

	t.Run("redirected to another origin", func(t *testing.T) {
		c, _, bHits := redirectedBlob(t, sha256Digest(archive), archive)
		tpl, err := c.DownloadTemplate("tpl", "v1")
		wantFixtureEntries(t, tpl, err)
		if bHits.count() != 1 {
			t.Errorf("the other origin received %d requests, want 1", bHits.count())
		}
	})

	t.Run("with limits", func(t *testing.T) {
		c, _, _ := redirectedBlob(t, sha256Digest(archive), archive)
		c.Limits = Limits{Files: 10, Bytes: 1 << 10}
		tpl, err := c.DownloadTemplate("tpl", "v1")
		wantFixtureEntries(t, tpl, err)
	})
}

// A blob that is not the one the manifest names is refused, with no entry, even
// when it is a perfectly readable archive and even when it comes from another
// host (the case that made the check necessary).
func TestBlobDigestMismatchIsRefused(t *testing.T) {
	archive := clientFixtureArchive()
	want := sha256Digest(archive)

	// The same compressed stream with one byte of the gzip header's
	// modification time changed: still a valid archive with the same files.
	headerByte := append([]byte(nil), archive...)
	headerByte[4] ^= 0x01

	// A tar with one byte of content changed, validly compressed.
	contentByte := fixtureTarGz("FROM scratcg\n")

	// The manifest names the digest of the decompressed tar (a diff ID), not
	// of the compressed layer: the comparison is on the stream as received.
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	tarBytes, err := io.ReadAll(io.LimitReader(zr, 1<<20))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		manifest string // digest the manifest names
		served   []byte
	}{
		{"one content byte changed", want, contentByte},
		{"one byte of the gzip header changed", want, headerByte},
		{"digest of the decompressed tar", sha256Digest(tarBytes), archive},
		{"another valid digest", sha256Digest([]byte("something else")), archive},
	} {
		t.Run("redirected/"+tc.name, func(t *testing.T) {
			c, _, bHits := redirectedBlob(t, tc.manifest, tc.served)
			tpl, err := c.DownloadTemplate("tpl", "v1")

			if !errors.Is(err, ErrBlobDigestMismatch) {
				t.Fatalf("err = %v, want errors.Is ErrBlobDigestMismatch", err)
			}
			if tpl != nil {
				t.Errorf("template = %+v, want nil", tpl)
			}
			if bHits.count() != 1 {
				t.Errorf("the other origin received %d requests, want 1 (the blob was fetched, then refused)", bHits.count())
			}
			wantNoDigestOrURL(t, err, tc.manifest, sha256Digest(tc.served))
		})
	}

	t.Run("same origin", func(t *testing.T) {
		var aHits hitLog
		a := blobRegistryFor(t, false, &aHits, want, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(contentByte)
		})
		tpl, err := NewClient(a.URL, clientTestUser, clientTestPass).DownloadTemplate("tpl", "v1")
		if !errors.Is(err, ErrBlobDigestMismatch) {
			t.Fatalf("err = %v, want errors.Is ErrBlobDigestMismatch", err)
		}
		if tpl != nil {
			t.Errorf("template = %+v, want nil", tpl)
		}
	})

	t.Run("with limits", func(t *testing.T) {
		c, _, _ := redirectedBlob(t, want, contentByte)
		c.Limits = Limits{Files: 10, Bytes: 1 << 10}
		tpl, err := c.DownloadTemplate("tpl", "v1")
		if !errors.Is(err, ErrBlobDigestMismatch) {
			t.Fatalf("err = %v, want errors.Is ErrBlobDigestMismatch", err)
		}
		if tpl != nil {
			t.Errorf("template = %+v, want nil", tpl)
		}
	})
}

// A digest that is not "sha256:" and 64 lowercase hex digits is refused after the
// manifest was read and before any blob request, whatever it would have led to.
func TestBlobDigestMalformedIsRefusedBeforeAnyBlobRequest(t *testing.T) {
	archive := clientFixtureArchive()
	good := hexOf(sha256Digest(archive))

	for _, tc := range []struct{ name, digest string }{
		{"sha512", "sha512:" + strings.Repeat("ab", 64)},
		{"path traversal", "sha256:../x"},
		{"path traversal up to the root", "../../etc/passwd"},
		{"upper case hex", "sha256:" + strings.ToUpper(good)},
		{"one upper case digit", "sha256:A" + good[1:]},
		{"63 digits", "sha256:" + good[:63]},
		{"65 digits", "sha256:" + good + "0"},
		{"very long", "sha256:" + strings.Repeat("a", 4096)},
		{"non hex characters", "sha256:" + strings.Repeat("g", 64)},
		{"empty", ""},
		{"prefix only", "sha256:"},
		{"no algorithm", good},
		{"slash inside", "sha256:" + good[:32] + "/" + good[33:]},
		{"query", "sha256:" + good + "?sig=SECRET"},
		{"newline", "sha256:" + good + "\n"},
		{"escape sequence", "sha256:\x1b[31m" + good[:56]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, aHits, bHits := redirectedBlob(t, tc.digest, archive)

			tpl, err := c.DownloadTemplate("tpl", "v1")

			if err == nil {
				t.Fatalf("template = %+v, want an error", tpl)
			}
			if errors.Is(err, ErrBlobDigestMismatch) {
				t.Errorf("err = %v: a malformed digest is not a mismatch (nothing was compared)", err)
			}
			if tpl != nil {
				t.Errorf("template = %+v, want nil", tpl)
			}
			if n := aHits.count(); n != 1 {
				t.Errorf("registry received %d requests, want 1 (the manifest only)", n)
			}
			if n := bHits.count(); n != 0 {
				t.Errorf("the other origin received %d requests, want 0", n)
			}
			wantNoDigestOrURL(t, err)
			if len(tc.digest) > 71 && strings.Contains(err.Error(), tc.digest) {
				t.Errorf("error echoes the whole malformed digest (%d bytes)", len(tc.digest))
			}
		})
	}
}

// The tar reader stops at the end of the archive and leaves what follows the gzip
// stream unread. Those bytes are part of the blob: they are hashed too, so a
// registry cannot append anything to a layer whose digest it named.
func TestBlobDigestCoversBytesAfterTheGzipStream(t *testing.T) {
	archive := clientFixtureArchive()
	withTail := append(append([]byte(nil), archive...), []byte("TRAILING-BYTES-NOT-IN-THE-DIGEST")...)

	t.Run("tail not covered by the digest", func(t *testing.T) {
		c, _, _ := redirectedBlob(t, sha256Digest(archive), withTail)
		tpl, err := c.DownloadTemplate("tpl", "v1")
		if !errors.Is(err, ErrBlobDigestMismatch) {
			t.Fatalf("err = %v, want errors.Is ErrBlobDigestMismatch", err)
		}
		if tpl != nil {
			t.Errorf("template = %+v, want nil", tpl)
		}
	})

	t.Run("one byte after the stream, same origin", func(t *testing.T) {
		var aHits hitLog
		a := blobRegistryFor(t, false, &aHits, sha256Digest(archive), func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(append(append([]byte(nil), archive...), 0))
		})
		_, err := NewClient(a.URL, clientTestUser, clientTestPass).DownloadTemplate("tpl", "v1")
		if !errors.Is(err, ErrBlobDigestMismatch) {
			t.Fatalf("err = %v, want errors.Is ErrBlobDigestMismatch", err)
		}
	})

	t.Run("tail covered by the digest", func(t *testing.T) {
		// What counts is the stream as received: the digest of archive+tail
		// is the right one for that stream.
		c, _, _ := redirectedBlob(t, sha256Digest(withTail), withTail)
		tpl, err := c.DownloadTemplate("tpl", "v1")
		wantFixtureEntries(t, tpl, err)
	})
}

// Draining the rest of the stream is bounded by blobCap like the rest of the
// read: a tail that would make the blob exceed it is refused as too large, even
// though its digest is the one the manifest names.
func TestBlobDigestDrainIsBoundedByTheByteCap(t *testing.T) {
	archive := clientFixtureArchive()
	lim := Limits{Files: 10, Bytes: 1 << 10}

	tail := make([]byte, 1200<<10) // 1.2 MiB of seeded, incompressible bytes
	_, _ = rand.New(rand.NewSource(412)).Read(tail)
	blob := append(append([]byte(nil), archive...), tail...)
	if int64(len(blob)) <= blobCap(lim) || len(blob) > 2<<20 {
		t.Fatalf("fixture is %d bytes, want it over blobCap (%d) and under 2 MiB", len(blob), blobCap(lim))
	}

	c, _, _ := redirectedBlob(t, sha256Digest(blob), blob)
	c.Limits = lim
	tpl, err := c.DownloadTemplate("tpl", "v1")

	if !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("err = %v, want errors.Is ErrArchiveTooLarge", err)
	}
	if errors.Is(err, ErrBlobDigestMismatch) {
		t.Errorf("err = %v: a blob over the cap is too large, not a mismatch", err)
	}
	if tpl != nil {
		t.Errorf("template = %+v, want nil", tpl)
	}
}
