package template

import (
	"reflect"
	"strings"
	"testing"
)

func TestEntryValidate(t *testing.T) {
	good := Entry{Slug: "spring-api", Name: "Spring API", Source: Source{Kind: KindGit, URL: "https://github.com/acme/spring.git"}}

	tests := []struct {
		name    string
		mutate  func(*Entry)
		wantErr string // empty means valid
	}{
		{"valid", func(*Entry) {}, ""},
		{"uppercase slug", func(e *Entry) { e.Slug = "Spring" }, "slug"},
		{"slug with a space", func(e *Entry) { e.Slug = "a b" }, "slug"},
		{"no name", func(e *Entry) { e.Name = " " }, "no name"},
		{"unknown kind", func(e *Entry) { e.Source.Kind = "svn" }, "unknown source kind"},
		{"scp form", func(e *Entry) { e.Source.URL = "git@github.com:acme/spring.git" }, ""},
		{"ssh url", func(e *Entry) { e.Source.URL = "ssh://git@host/acme/spring.git" }, ""},
		// The catalog can be shared: ext:: runs a command and file:// reads this machine.
		{"ext transport", func(e *Entry) { e.Source.URL = "ext::sh -c id" }, "not allowed"},
		{"file scheme", func(e *Entry) { e.Source.URL = "file:///etc" }, "not allowed"},
		{"option as url", func(e *Entry) { e.Source.URL = "--upload-pack=id" }, "option"},
		{"option as ref", func(e *Entry) { e.Source.Ref = "-x" }, "option"},
		{"no url", func(e *Entry) { e.Source.URL = "" }, "needs a URL"},
		{"local without a directory", func(e *Entry) { e.Source = Source{Kind: KindLocal} }, "directory"},
		{"local", func(e *Entry) { e.Source = Source{Kind: KindLocal, Path: "/src/tpl"} }, ""},
		{"oci without a tag", func(e *Entry) { e.Source = Source{Kind: KindOCI, URL: "https://r", Path: "a/b"} }, "tag"},
		{"oci", func(e *Entry) { e.Source = Source{Kind: KindOCI, URL: "https://r", Path: "a/b", Ref: "v1"} }, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := good
			tc.mutate(&e)
			err := e.Validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("Validate() = %v, want valid", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("Validate() = %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestNormalizeTags(t *testing.T) {
	got := NormalizeTags([]string{" Java ", "java", "", "Spring-Boot", "spring-boot"})
	if want := []string{"java", "spring-boot"}; !reflect.DeepEqual(got, want) {
		t.Errorf("NormalizeTags() = %v, want %v", got, want)
	}
}
