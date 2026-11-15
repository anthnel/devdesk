//go:build linux

package wslenv

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeScript writes an executable shell script and returns its path.
func writeScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "wslinfo")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunWslinfo(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		want    string
		wantErr bool
	}{
		{"success", `[ "$1" = "--networking-mode" ] && echo nat`, "nat", false},
		{"non zero exit", `echo nat; exit 3`, "", true},
		{"output over 64 bytes", `i=0; while [ $i -lt 200 ]; do printf 'aaaaaaaaaa'; i=$((i+1)); done`, strings.Repeat("a", 64), false},
		{"empty environment", `[ -z "$HOME" ] && [ -z "$WSL_DISTRO_NAME" ] && echo clean`, "clean", false},
	}
	t.Setenv("HOME", "/tmp")
	t.Setenv("WSL_DISTRO_NAME", "x")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runWslinfo(context.Background(), writeScript(t, tt.script))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunWslinfoMissingBinary(t *testing.T) {
	if _, err := runWslinfo(context.Background(), filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("err = nil for a missing binary")
	}
}

func TestRunWslinfoContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := runWslinfo(ctx, writeScript(t, `while :; do :; done`))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Errorf("took %v, want the short context to cut it", d)
	}
}
