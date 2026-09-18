package template

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sample(slug, name string) Entry {
	return Entry{
		Slug: slug, Name: name, Tags: []string{"Java", "spring-boot"},
		Source: Source{Kind: KindGit, URL: "https://github.com/acme/" + slug + ".git", Ref: "main"},
	}
}

func TestAMissingCatalogIsEmpty(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "templates.yaml"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if got := s.List(); len(got) != 0 {
		t.Errorf("List() = %v, want empty", got)
	}
}

func TestPutSurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "templates.yaml")
	s, _ := Open(path)
	if err := s.Put(sample("b-api", "B API")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := s.Put(sample("a-lib", "A Lib")); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	got := reopened.List()
	if len(got) != 2 || got[0].Slug != "a-lib" || got[1].Slug != "b-api" {
		t.Fatalf("List() = %+v, want a-lib then b-api (sorted by name)", got)
	}
	if want := []string{"java", "spring-boot"}; !reflect.DeepEqual(got[0].Tags, want) {
		t.Errorf("tags = %v, want %v (normalized on the way in)", got[0].Tags, want)
	}
}

func TestPutReplacesTheSameSlug(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "templates.yaml"))
	_ = s.Put(sample("api", "Old"))
	_ = s.Put(sample("api", "New"))

	if got := s.List(); len(got) != 1 || got[0].Name != "New" {
		t.Errorf("List() = %+v, want one entry named New", got)
	}
}

func TestAnInvalidEntryIsRefusedAndNothingIsWritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.yaml")
	s, _ := Open(path)
	bad := sample("api", "API")
	bad.Source.URL = "ext::sh -c id"

	if err := s.Put(bad); err == nil {
		t.Fatal("Put() accepted an ext:: URL")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a refused Put created the catalog file")
	}
	if len(s.List()) != 0 {
		t.Error("a refused Put changed the in-memory catalog")
	}
}

func TestDelete(t *testing.T) {
	s, _ := Open(filepath.Join(t.TempDir(), "templates.yaml"))
	_ = s.Put(sample("api", "API"))

	if err := s.Delete("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete(unknown) = %v, want ErrNotFound", err)
	}
	if err := s.Delete("api"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := s.Get("api"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() after Delete = %v, want ErrNotFound", err)
	}
}

// A hand-edited or shared catalog is not trusted: an entry that would not pass
// Put is set aside on load — never fetchable — without taking the others down.
func TestOpenSetsADangerousEntryAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.yaml")
	raw := "templates:\n" +
		"- slug: evil\n  name: Evil\n  source:\n    kind: git\n    url: \"ext::sh -c id\"\n" +
		"- slug: fine\n  name: Fine\n  source:\n    kind: local\n    path: /x\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v, want the valid entry usable", err)
	}
	if _, err := s.Get("evil"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(evil) error = %v, want ErrNotFound", err)
	}
	if _, err := s.Get("fine"); err != nil {
		t.Errorf("Get(fine) error = %v", err)
	}
	if len(s.Problems()) != 1 {
		t.Fatalf("Problems() = %v, want one", s.Problems())
	}

	// Saving another entry must not delete the one that was set aside.
	if err := s.Put(Entry{Slug: "other", Name: "Other", Source: Source{Kind: KindLocal, Path: "/y"}}); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	if !strings.Contains(string(saved), "ext::sh -c id") {
		t.Errorf("the rejected entry was dropped from the file:\n%s", saved)
	}
}

func TestOpenSetsADuplicateSlugAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.yaml")
	entry := "- slug: dup\n  name: %s\n  source:\n    kind: local\n    path: /x\n"
	raw := "templates:\n" + fmt.Sprintf(entry, "First") + fmt.Sprintf(entry, "Second")
	_ = os.WriteFile(path, []byte(raw), 0o600)

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("dup")
	if got.Name != "First" || len(s.Problems()) != 1 {
		t.Errorf("Get(dup) = %q, problems = %v; want the first kept and one problem", got.Name, s.Problems())
	}
}

func TestOpenRefusesBrokenYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.yaml")
	_ = os.WriteFile(path, []byte("templates: [unclosed"), 0o600)
	if _, err := Open(path); err == nil {
		t.Fatal("Open() accepted broken YAML")
	}
}
