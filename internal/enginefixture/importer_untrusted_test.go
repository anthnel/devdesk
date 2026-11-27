package enginefixture_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/enginefixture"
)

// OC-09a3 (DEVDESK-944), security finding B1: the files of a batch come out of a VM.
// When one does not parse, Import says which rule it breaks, in the form
// "<file>: invalid format (<rule>)", and never copies a header value into the error.

func TestImportDoesNotEchoTheValuesOfAFileThatDoesNotParse(t *testing.T) {
	good := impPodmanFx("containers", "good", "2026-11-27", "x\n")
	tests := []struct {
		name   string
		mutate func(string) string
		secret string
		rule   string
	}{
		{"a tool that is a secret", func(s string) string { return strings.Replace(s, "# tool: podman", "# tool: hunter2-secret", 1) }, "hunter2-secret", "tool <value> is neither docker nor podman"},
		{"a mode that is a secret", func(s string) string { return strings.Replace(s, "# mode: rootless", "# mode: hunter2-secret", 1) }, "hunter2-secret", "mode <value> is neither rootful nor rootless"},
		{"a version that is a secret", func(s string) string { return strings.Replace(s, "# version: 6.2.1", "# version: hunter2-secret", 1) }, "hunter2-secret", "version <value> is not a dotted version number"},
		{"a date that is a secret", func(s string) string {
			return strings.Replace(s, "# recorded: 2026-11-27", "# recorded: hunter2-secret", 1)
		}, "hunter2-secret", "recorded <value> is not a date"},
		{"a header that is a secret", func(s string) string { return strings.Replace(s, "# scenario:", "# hunter2-secret: x\n# scenario:", 1) }, "hunter2-secret", "unknown header <value>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, into := t.TempDir(), t.TempDir()
			impPutBytes(t, from, "containers", good.Name+".cli", []byte(tt.mutate(string(enginefixture.Format(good)))))

			_, err := impImport(from, into)

			if err == nil {
				t.Fatal("Import = nil, want an error")
			}
			msg := err.Error()
			if strings.Contains(msg, tt.secret) {
				t.Errorf("the error repeats a value of the file: %s", msg)
			}
			for _, want := range []string{"containers/" + good.Name + ".cli", "invalid format", tt.rule} {
				if !strings.Contains(msg, want) {
					t.Errorf("the error does not contain %q: %s", want, msg)
				}
			}
		})
	}
}

// Parse itself keeps its messages for a file of the repository, and exposes the rule.
func TestParseErrorCarriesARuleWithoutValues(t *testing.T) {
	_, err := enginefixture.Parse("c", "a--podman-6.2.1-rootless.cli", []byte("# tool: hunter2\n\n"))
	var pe *enginefixture.ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v (%T), want a *ParseError", err, err)
	}
	if pe.File != "a--podman-6.2.1-rootless.cli" || pe.Rule == "" || strings.Contains(pe.Rule, "hunter2") {
		t.Errorf("ParseError = %+v, want the file and a rule with no value", pe)
	}
	if !strings.Contains(err.Error(), "a--podman-6.2.1-rootless.cli") {
		t.Errorf("the message does not name the file: %v", err)
	}
}
