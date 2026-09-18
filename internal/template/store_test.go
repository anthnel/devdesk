package template

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
// Put is refused on load, naming the file.
func TestOpenRefusesADangerousEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.yaml")
	raw := "templates:\n- slug: evil\n  name: Evil\n  source:\n    kind: git\n    url: \"ext::sh -c id\"\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("Open() loaded an entry with an ext:: URL")
	}
}

func TestOpenRefusesBrokenYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "templates.yaml")
	_ = os.WriteFile(path, []byte("templates: [unclosed"), 0o600)
	if _, err := Open(path); err == nil {
		t.Fatal("Open() accepted broken YAML")
	}
}
