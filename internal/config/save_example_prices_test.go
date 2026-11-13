package config

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func seedContextFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path, err := GetContextPath(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureConfigDir(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func plainContext(t *testing.T) []byte {
	t.Helper()
	data, err := marshalContext(Default(), false)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Criterion 3: a file that carries the block keeps it, once, byte-stable.
func TestSaveContext_KeepsExampleBlock(t *testing.T) {
	setupTmpHome(t)
	path := seedContextFile(t, "default", append(plainContext(t), []byte("\n"+ExamplePricesBlock)...))

	for i := 0; i < 3; i++ {
		if err := SaveContext(Default(), "default"); err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(readFile(t, path), pinnedMarker); n != 1 {
			t.Fatalf("save %d: marker count = %d, want 1", i+1, n)
		}
	}
	first := readFile(t, path)
	if err := SaveContext(Default(), "default"); err != nil {
		t.Fatal(err)
	}
	if second := readFile(t, path); second != first {
		t.Errorf("two saves without change differ:\n%s\n---\n%s", first, second)
	}
	want := string(plainContext(t)) + "\n" + ExamplePricesBlock
	if first != want {
		t.Errorf("file = %q, want %q", first, want)
	}
}

func TestSaveContext_KeepsExampleBlock_CRLFMarker(t *testing.T) {
	setupTmpHome(t)
	crlf := strings.ReplaceAll(ExamplePricesBlock, "\n", "\r\n")
	path := seedContextFile(t, "default", append(plainContext(t), []byte("\n"+crlf)...))

	if err := SaveContext(Default(), "default"); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	if n := strings.Count(got, pinnedMarker); n != 1 {
		t.Fatalf("marker count = %d, want 1", n)
	}
	if !strings.HasSuffix(got, ExamplePricesBlock) {
		t.Errorf("block not rewritten from the constant: %q", got)
	}
}

// Criterion 7: an edit under an intact marker is replaced by the original text.
func TestSaveContext_RestoresEditedBlockUnderMarker(t *testing.T) {
	setupTmpHome(t)
	edited := strings.Replace(ExamplePricesBlock, "input: 3", "input: 99", 1) + "# my own note\n"
	path := seedContextFile(t, "default", append(plainContext(t), []byte("\n"+edited)...))

	if err := SaveContext(Default(), "default"); err != nil {
		t.Fatal(err)
	}
	want := string(plainContext(t)) + "\n" + ExamplePricesBlock
	if got := readFile(t, path); got != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

// Criterion 4: any price entry means no block, written or kept.
func TestSaveContext_PricesDropTheBlock(t *testing.T) {
	cases := map[string]map[string]ModelPrice{
		"complete":   {"m": {Input: Rate{state: RateSet, value: 3}}},
		"incomplete": {"m": {}},
		"invalid":    {"m": {Input: Rate{state: RateInvalid, raw: "5$"}}},
	}
	for name, prices := range cases {
		t.Run(name, func(t *testing.T) {
			setupTmpHome(t)
			path := seedContextFile(t, "default", append(plainContext(t), []byte("\n"+ExamplePricesBlock)...))

			cfg := Default()
			cfg.AgentUsage.Prices = prices
			if err := SaveContext(cfg, "default"); err != nil {
				t.Fatal(err)
			}
			got := readFile(t, path)
			if strings.Contains(got, pinnedMarker) {
				t.Errorf("block kept next to prices:\n%s", got)
			}
			loaded, err := LoadContext("default")
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded.AgentUsage.Prices) != 1 {
				t.Errorf("prices after reload = %d entries, want 1", len(loaded.AgentUsage.Prices))
			}

			// Prices emptied by hand afterwards: the block does not come back.
			if err := os.WriteFile(path, plainContext(t), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := SaveContext(Default(), "default"); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, path); strings.Contains(got, pinnedMarker) {
				t.Errorf("block came back:\n%s", got)
			}
		})
	}
}

// Criterion 7: no block unless the file already carries it.
func TestSaveContext_NoBlockWithoutMarker(t *testing.T) {
	t.Run("old context without block", func(t *testing.T) {
		setupTmpHome(t)
		path := seedContextFile(t, "default", plainContext(t))
		if err := SaveContext(Default(), "default"); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, path); got != string(plainContext(t)) {
			t.Errorf("file changed: %q", got)
		}
	})
	t.Run("block removed by hand", func(t *testing.T) {
		setupTmpHome(t)
		path := seedContextFile(t, "default", append(plainContext(t), []byte("\n"+ExamplePricesBlock)...))
		if err := SaveContext(Default(), "default"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, plainContext(t), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := SaveContext(Default(), "default"); err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, path); strings.Contains(got, pinnedMarker) {
			t.Errorf("block came back:\n%s", got)
		}
	})
	t.Run("file absent", func(t *testing.T) {
		setupTmpHome(t)
		if err := SaveContext(Default(), "fresh"); err != nil {
			t.Fatal(err)
		}
		path, _ := GetContextPath("fresh")
		if got := readFile(t, path); strings.Contains(got, pinnedMarker) {
			t.Errorf("block written to a new file:\n%s", got)
		}
	})
}

func TestSaveContext_UnreadableFileSavesWithoutBlock(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 0200 is not enforced here")
	}
	setupTmpHome(t)
	path := seedContextFile(t, "default", append(plainContext(t), []byte("\n"+ExamplePricesBlock)...))
	if err := os.Chmod(path, 0o200); err != nil {
		t.Fatal(err)
	}
	logs := captureLog(t)

	if err := SaveContext(Default(), "default"); err != nil {
		t.Fatalf("SaveContext must still save: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); strings.Contains(got, pinnedMarker) {
		t.Errorf("block written although the file could not be read:\n%s", got)
	}
	if !strings.Contains(logs.String(), "WARN [config]") {
		t.Errorf("read failure not logged, log = %q", logs.String())
	}
}

// With prices present the file is not read at all, so no warning either.
func TestSaveContext_PricesPresentDoesNotReadFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod 0200 is not enforced here")
	}
	setupTmpHome(t)
	path := seedContextFile(t, "default", plainContext(t))
	if err := os.Chmod(path, 0o200); err != nil {
		t.Fatal(err)
	}
	logs := captureLog(t)
	cfg := Default()
	cfg.AgentUsage.Prices = map[string]ModelPrice{"m": {}}
	if err := SaveContext(cfg, "default"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "WARN [config]") {
		t.Errorf("file was read although prices are set: %q", logs.String())
	}
}

// Criterion 5: loading a file with the block is silent and yields no price.
func TestLoadContext_WithExampleBlock(t *testing.T) {
	setupTmpHome(t)
	seedContextFile(t, "default", append(plainContext(t), []byte("\n"+ExamplePricesBlock)...))
	logs := captureLog(t)

	cfg, err := LoadContext("default")
	if err != nil {
		t.Fatalf("LoadContext: %v", err)
	}
	if len(cfg.AgentUsage.Prices) != 0 {
		t.Errorf("prices = %v, want none", cfg.AgentUsage.Prices)
	}
	if strings.Contains(logs.String(), "agent_usage.prices") || strings.Contains(logs.String(), "WARN") {
		t.Errorf("unexpected warning: %q", logs.String())
	}
}
