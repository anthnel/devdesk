package agentusage

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadMeta(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		want      meta
		wantState State
	}{
		{"finished true", `{"agentType":"coder","finished":true,"spawnDepth":1}`, meta{AgentType: "coder", SpawnDepth: 1}, StateFinished},
		{"finished false", `{"agentType":"tester","finished":false,"spawnDepth":2}`, meta{AgentType: "tester", SpawnDepth: 2}, StateUnfinished},
		{"finished absent", `{"agentType":"coder","spawnDepth":1}`, meta{AgentType: "coder", SpawnDepth: 1}, StateUnknown},
		{"finished null", `{"agentType":"coder","finished":null}`, meta{AgentType: "coder"}, StateUnknown},
		{"unknown fields", `{"agentType":"x","finished":true,"novel":{"a":[1]},"launchedWithDefaults":true,"worktreeBranch":"b"}`, meta{AgentType: "x"}, StateFinished},
		{"empty object", `{}`, meta{}, StateUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "agent-a.meta.json")
			writeFile(t, p, tt.content)
			got, err := readMeta(p)
			if err != nil {
				t.Fatalf("readMeta: %v", err)
			}
			if got.AgentType != tt.want.AgentType || got.SpawnDepth != tt.want.SpawnDepth {
				t.Fatalf("meta = %+v, want %+v", got, tt.want)
			}
			if st := got.state(); st != tt.wantState {
				t.Fatalf("state = %d, want %d", st, tt.wantState)
			}
		})
	}
}

func TestReadMetaFinishedPointer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "m.json")
	writeFile(t, p, `{"finished":false}`)
	got, err := readMeta(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Finished == nil || *got.Finished {
		t.Fatalf("Finished = %v, want pointer to false", got.Finished)
	}
}

func TestReadMetaInvalid(t *testing.T) {
	for name, content := range map[string]string{
		"not json":  `hello ` + sentinel,
		"truncated": `{"agentType":"` + sentinel,
		"empty":     ``,
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "m.json")
			writeFile(t, p, content)
			_, err := readMeta(p)
			if err == nil {
				t.Fatal("want an error")
			}
			if strings.Contains(err.Error(), sentinel) {
				t.Fatalf("error leaks file content: %v", err)
			}
		})
	}
}

func TestReadMetaMissingFile(t *testing.T) {
	_, err := readMeta(filepath.Join(t.TempDir(), "absent.meta.json"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want errors.Is os.ErrNotExist", err)
	}
}

func TestReadMetaNeverReadsDescriptionOrWorktree(t *testing.T) {
	p := filepath.Join(t.TempDir(), "m.json")
	writeFile(t, p, `{"agentType":"coder","finished":true,"spawnDepth":1,`+
		`"description":"`+sentinel+`","worktreePath":"/x/`+sentinel+`"}`)
	got, err := readMeta(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.AgentType, sentinel) {
		t.Fatalf("sentinel leaked: %+v", got)
	}
	// Even a wrongly typed description must not matter: it is never decoded.
	writeFile(t, p, `{"agentType":"coder","description":{"a":"`+sentinel+`"},"worktreePath":[1]}`)
	if _, err := readMeta(p); err != nil {
		t.Fatalf("undeclared fields of any type must be skipped: %v", err)
	}
}

// An unexpected type for agentType or spawnDepth costs that field only.
func TestReadMetaTolerantTypes(t *testing.T) {
	for name, content := range map[string]string{
		"agentType number":    `{"agentType":5,"finished":true,"spawnDepth":2}`,
		"agentType object":    `{"agentType":{"a":"` + sentinel + `"},"finished":true,"spawnDepth":2}`,
		"spawnDepth string":   `{"agentType":"coder","finished":true,"spawnDepth":"2"}`,
		"spawnDepth fraction": `{"agentType":"coder","finished":true,"spawnDepth":1.5}`,
		"both wrong":          `{"agentType":[1],"finished":true,"spawnDepth":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "m.json")
			writeFile(t, p, content)
			got, err := readMeta(p)
			if err != nil {
				t.Fatalf("readMeta: %v", err)
			}
			if got.state() != StateFinished {
				t.Fatalf("state = %d, want finished kept", got.state())
			}
			wantDepth := 0
			if strings.HasPrefix(name, "agentType") && name != "both wrong" {
				wantDepth = 2
			}
			if got.SpawnDepth != wantDepth {
				t.Fatalf("SpawnDepth = %d, want %d", got.SpawnDepth, wantDepth)
			}
			if strings.HasPrefix(name, "agentType") && got.AgentType != "" {
				t.Fatalf("AgentType = %q, want empty", got.AgentType)
			}
		})
	}
}

func TestReadMetaSizeLimit(t *testing.T) {
	pad := func(n int) string {
		base := `{"agentType":"coder","finished":true,"x":"`
		return base + strings.Repeat("a", n-len(base)-2) + `"}`
	}
	t.Run("exactly the limit is read", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "m.json")
		writeFile(t, p, pad(maxMetaBytes))
		if _, err := readMeta(p); err != nil {
			t.Fatalf("readMeta: %v", err)
		}
	})
	t.Run("over the limit is refused without content", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "m.json")
		content := pad(maxMetaBytes + 1)
		content = strings.Replace(content, `"x":"`, `"x":"`+sentinel, 1)
		writeFile(t, p, content)
		_, err := readMeta(p)
		if err == nil {
			t.Fatal("want an error")
		}
		if strings.Contains(err.Error(), sentinel) {
			t.Fatalf("error leaks file content: %v", err)
		}
	})
}

// A finished flag of an unexpected type costs that field only: agentType and
// spawnDepth are kept and the state is unknown.
func TestReadMetaFinishedWrongType(t *testing.T) {
	for name, finished := range map[string]string{
		"string": `"` + sentinel + `"`, "number": `1`, "object": `{"a":1}`, "array": `[true]`,
	} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "m.json")
			writeFile(t, p, `{"agentType":"coder","finished":`+finished+`,"spawnDepth":2}`)
			got, err := readMeta(p)
			if err != nil {
				t.Fatalf("readMeta: %v", err)
			}
			if got.Finished != nil || got.state() != StateUnknown {
				t.Fatalf("Finished = %v state %d, want nil / unknown", got.Finished, got.state())
			}
			if got.AgentType != "coder" || got.SpawnDepth != 2 {
				t.Fatalf("meta = %+v, want agentType and spawnDepth kept", got)
			}
		})
	}
}
