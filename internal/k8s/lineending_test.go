package k8s

import "testing"

func TestLineEndingAtReadsTheEndingOfThatLine(t *testing.T) {
	tests := []struct {
		name    string
		content string
		line    int
		want    string
	}{
		{"LF file", "a\nb\nc\n", 2, "\n"},
		{"CRLF file", "a\r\nb\r\nc\r\n", 2, "\r\n"},
		{"stray CRLF elsewhere in an LF file", "a\r\nb\nc\n", 2, "\n"},
		{"stray LF elsewhere in a CRLF file", "a\nb\r\nc\r\n", 2, "\r\n"},
		{"last line without ending falls back on the file", "a\r\nb", 2, "\r\n"},
		{"unreadable line falls back on the file", "a\r\nb\r\n", 9, "\r\n"},
		{"unreadable line, LF file", "a\nb\n", 0, "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LineEndingAt([]byte(tt.content), tt.line); got != tt.want {
				t.Errorf("LineEndingAt(%q, %d) = %q, want %q", tt.content, tt.line, got, tt.want)
			}
		})
	}
}
