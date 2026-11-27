package enginecontract_test

import (
	"os"
	"path/filepath"
	"testing"
)

// readOnly reads the one fixture a recording wrote under out/campaign.
func readOnly(t *testing.T, out, campaign string) []byte {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(out, campaign, "*.cli"))
	if len(files) != 1 {
		t.Fatalf("%d fixture(s) under %s, want 1", len(files), campaign)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	return data
}
