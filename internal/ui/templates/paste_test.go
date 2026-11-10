package templates

import (
	"testing"

	"github.com/anthnel/devdesk/internal/ui/keymap"
	"github.com/anthnel/devdesk/internal/ui/testutil"
)

// DEVDESK-201 D3: a paste reaches the focused field of the entry form and the
// filter bar, newlines become spaces in a one-line field, and no key action
// fires (Rule 111). The catalog is a temp file (catalogWith), so nothing under
// the real HOME is touched.
func TestPasteReachesTheEntryFormFields(t *testing.T) {
	tests := []struct {
		name  string
		downs int
		paste string
		want  string
		read  func(f *entryForm) string
	}{
		{"name", 0, "Spring API", "Spring API", func(f *entryForm) string { return f.name.Value() }},
		{"name, multi-line", 0, "a\nb", "a b", func(f *entryForm) string { return f.name.Value() }},
		{"name, action letters", 0, "DEq?", "DEq?", func(f *entryForm) string { return f.name.Value() }},
		{"description", 1, "a short text", "a short text", func(f *entryForm) string { return f.description.Value() }},
		{"tags", 2, "java, spring", "java, spring", func(f *entryForm) string { return f.tags.Value() }},
		{"url", 4, "https://github.com/acme/x.git", "https://github.com/acme/x.git", func(f *entryForm) string { return f.url.Value() }},
		{"url, multi-line", 4, "a\nb", "a b", func(f *entryForm) string { return f.url.Value() }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := openedAt(t, catalogWith(t, entry("aaa", "Aaa")))
			m = send(t, m, testutil.Key(keymap.New))
			if m.form == nil {
				t.Fatal("the form did not open")
			}
			for range tc.downs {
				m = send(t, m, testutil.Key("down"))
			}
			m = send(t, m, testutil.Paste(tc.paste))

			if m.form == nil {
				t.Fatal("the paste closed the form")
			}
			if got := tc.read(m.form); got != tc.want {
				t.Errorf("field = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if m.confirmModal != nil {
				t.Error("the paste opened a confirmation")
			}
		})
	}
}

func TestPasteReachesTheFilterBar(t *testing.T) {
	tests := []struct{ name, paste, want string }{
		{"word", "spring", "spring"},
		{"multi-line", "a\nb", "a b"},
		{"action letters", "DNq", "DNq"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := openedAt(t, catalogWith(t, entry("aaa", "Aaa")))
			m = send(t, m, testutil.Key("/"))
			m = send(t, m, testutil.Paste(tc.paste))
			if got := m.table.FilterBar().SearchQuery(); got != tc.want {
				t.Errorf("search query = %q after pasting %q, want %q", got, tc.paste, tc.want)
			}
			if m.form != nil || m.confirmModal != nil {
				t.Error("the paste opened a form or a confirmation")
			}
		})
	}
}
