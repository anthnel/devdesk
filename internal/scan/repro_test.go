package scan

import (
	"errors"
	"os/exec"
	"testing"
)

// DEVDESK-752 (step 1): the Repro line is built from argv tokens, never quoted.
// A token a shell would read as more than a word refuses the whole line.

// useLookPath replaces the PATH lookup for one test: found maps a tool name to
// the binary it resolves to; any other name is not on PATH.
func useLookPath(t *testing.T, found map[string]string) {
	t.Helper()
	previous := lookPath
	lookPath = func(name string) (string, error) {
		if p, ok := found[name]; ok {
			return p, nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { lookPath = previous })
}

// PlainWords is the one computation behind Run and Repro: it moved here from
// the finding detail, and its answers did not change.
func TestPlainWords(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{"words", "kustomize build kustomizations/dev", true},
		{"a flag with a value", "helm lint --set a=b charts/api", true},
		{"empty", "", true},
		{"a separator", "kustomize build a; rm -rf /", false},
		{"a pipe", "helm template x | kubeconform -", false},
		{"a variable", "cat $HOME", false},
		{"a quote", `echo "x"`, false},
		{"a backslash", `kubeconform C:\x`, false},
		{"a glob", "ls *.yaml", false},
		{"a tab", "a\tb", false},
		{"a line break", "a\nb", false},
		{"an escape sequence", "a\x1b[31mb", false},
		{"a zero-width space", "a\u200bb", false},
		// DEVDESK-752 (security F2): a tilde, a caret, a percent sign or an equal
		// sign at the head of a word is read by a shell (home directory, history or
		// glob negation, job, command path); inside a word it is a spelling real
		// names and versions carry.
		{"a tilde at the head of a word", "cat ~/secrets", false},
		{"a tilde user", "ls ~root", false},
		{"a tilde in a version", "apt-get install g++-12=1:12.2.0~rc1-2", true},
		{"brackets make a glob", "pip install pkg[extra]", false},
		{"a closing bracket", "ls a]", false},
		{"a caret at the head of a word", "echo ^a^b", false},
		{"a caret in a version range", "npm install semver@^6.3.1", true},
		{"a percent job at the head of a word", "kill %1", false},
		{"a percent inside a word", "echo a%b", true},
		{"an equal sign at the head of a word", "echo =ls", false},
		{"an equal sign alone", "echo = x", false},
		{"an equal sign at the head of the command", "=ls -l", false},
		{"an equal sign in a flag value", "kubeconform --load-restrictor=LoadRestrictionsNone x.yaml", true},
		{"a double equal sign in a pin", "pip install pkg==1.2", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlainWords(tt.cmd); got != tt.want {
				t.Errorf("PlainWords(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestReproLine(t *testing.T) {
	tests := []struct {
		name    string
		program string
		args    []string
		paths   []string
		want    string
		ok      bool
	}{
		{"the AC example", "kustomize", []string{"build"}, []string{"kustomizations/dev"}, "kustomize build kustomizations/dev", true},
		{"no argument", "helm", nil, nil, "helm", true},
		{"a flag with a value", "kubeconform", []string{"-kubernetes-version", "1.36.0"}, []string{"deploy/app.yaml"},
			"kubeconform -kubernetes-version 1.36.0 deploy/app.yaml", true},
		{"a path with a space", "kustomize", []string{"build"}, []string{"my overlay"}, "", false},
		{"a separator in a path", "kustomize", []string{"build"}, []string{"a;b"}, "", false},
		{"a tab in a path", "kustomize", []string{"build"}, []string{"x\ty"}, "", false},
		{"a control character", "kustomize", []string{"build"}, []string{"x\x1b[0m"}, "", false},
		{"an empty path", "kustomize", []string{"build"}, []string{""}, "", false},
		{"a Windows path", "kubeconform", nil, []string{`C:\x`}, "", false},
		{"a program with a space", "/opt/my tools/helm", []string{"lint"}, nil, "", false},
		{"an empty program", "", []string{"build"}, nil, "", false},
		// DEVDESK-752 (security F1): pasted, a path that starts with "-" is an option.
		{"a path that starts with a dash", "kubeconform", []string{"-strict"}, []string{"-schema-location=foo.yaml"}, "", false},
		{"a directory that starts with a dash", "kustomize", []string{"build"}, []string{"-x"}, "", false},
		{"a dash inside a path", "kustomize", []string{"build"}, []string{"k8s/-x/dev"}, "kustomize build k8s/-x/dev", true},
		{"a flag is not a path", "kubeconform", []string{"-strict"}, []string{"a.yaml"}, "kubeconform -strict a.yaml", true},
		{"a tilde path", "kubeconform", nil, []string{"~/x.yaml"}, "", false},
		{"a path that starts with an equal sign", "kubeconform", nil, []string{"=x.yaml"}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := reproLine(tt.program, tt.args, tt.paths...)
			if got != tt.want || ok != tt.ok {
				t.Errorf("reproLine(%q, %q) = (%q, %v), want (%q, %v)", tt.program, tt.args, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// The plain tool name when PATH leads to the same binary: that is what a person
// would type. Any other binary keeps its configured path, which is what ran.
func TestReproProgram(t *testing.T) {
	found := func(path string, err error) func(string) (string, error) {
		return func(string) (string, error) { return path, err }
	}
	tests := []struct {
		name   string
		binary string
		look   func(string) (string, error)
		want   string
	}{
		{"the same binary", "/usr/bin/helm", found("/usr/bin/helm", nil), "helm"},
		{"another binary", "/opt/helm-4/helm", found("/usr/bin/helm", nil), "/opt/helm-4/helm"},
		{"not on PATH", "/opt/helm-4/helm", found("", exec.ErrNotFound), "/opt/helm-4/helm"},
		{"a lookup that fails otherwise", "/opt/helm-4/helm", found("", errors.New("boom")), "/opt/helm-4/helm"},
		{"no configured binary", "", found("/usr/bin/helm", nil), "helm"},
		{"no configured binary and no PATH", "", found("", exec.ErrNotFound), "helm"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reproProgram(tt.binary, "helm", tt.look); got != tt.want {
				t.Errorf("reproProgram(%q) = %q, want %q", tt.binary, got, tt.want)
			}
		})
	}
}

func TestLocalRepro(t *testing.T) {
	useLookPath(t, map[string]string{"kustomize": "/usr/bin/kustomize"})
	tests := []struct {
		name     string
		tool     ToolSpec
		args     []string
		paths    []string
		wantLine string
		wantFrom ToolSource
	}{
		{"a binary found on PATH", ToolSpec{Source: ToolSourceBinary, Binary: "/usr/bin/kustomize"},
			[]string{"build"}, []string{"kustomizations/dev"}, "kustomize build kustomizations/dev", ToolSourceBinary},
		{"a binary with no configured path", ToolSpec{Source: ToolSourceBinary},
			[]string{"build"}, []string{"k8s/overlays/prod"}, "kustomize build k8s/overlays/prod", ToolSourceBinary},
		{"a binary somewhere else", ToolSpec{Source: ToolSourceBinary, Binary: "/opt/k/kustomize"},
			[]string{"build"}, []string{"dev"}, "/opt/k/kustomize build dev", ToolSourceBinary},
		// The tool ran here, but no plain line says how: where it ran is still said.
		{"a path with a space", ToolSpec{Source: ToolSourceBinary},
			[]string{"build"}, []string{"my overlay"}, "", ToolSourceBinary},
		{"a path that starts with a dash", ToolSpec{Source: ToolSourceBinary},
			[]string{"build"}, []string{"-x"}, "", ToolSourceBinary},
		// Q3: no Repro for a container, and the source says why there is none.
		{"a container", ToolSpec{Source: ToolSourceContainer, Image: "registry.k8s.io/kustomize/kustomize:v5.8.1"},
			[]string{"build"}, []string{"kustomizations/dev"}, "", ToolSourceContainer},
		{"no tool", ToolSpec{Source: ToolSourceNone}, []string{"build"}, []string{"dev"}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, from := localRepro(tt.tool, "kustomize", tt.args, tt.paths...)
			if line != tt.wantLine || from != tt.wantFrom {
				t.Errorf("localRepro = (%q, %q), want (%q, %q)", line, from, tt.wantLine, tt.wantFrom)
			}
		})
	}
}
