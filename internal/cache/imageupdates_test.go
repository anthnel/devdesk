package cache

import (
	"path/filepath"
	"testing"
	"time"
)

func TestImageUpdatesAreMergedNotReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image-updates.json")
	now := time.Now().UTC().Truncate(time.Second)
	if err := setImageUpdatesAt(path, map[string]ImageUpdateEntry{"alpine:latest": {Digest: "sha256:a", CheckedAt: now}}); err != nil {
		t.Fatal(err)
	}
	if err := setImageUpdatesAt(path, map[string]ImageUpdateEntry{"node:20.11.1": {NewerPatch: "20.11.2", CheckedAt: now}}); err != nil {
		t.Fatal(err)
	}
	got, err := readImageUpdateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["alpine:latest"].Digest != "sha256:a" || got["node:20.11.1"].NewerPatch != "20.11.2" {
		t.Errorf("entries = %+v", got)
	}
}
