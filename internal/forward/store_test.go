package forward

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "sub", FileName))
}

func TestAMissingFileIsAnEmptyList(t *testing.T) {
	entries, err := newTestStore(t).Load()
	if err != nil {
		t.Fatalf("Load on a missing file: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("a missing file loaded %d entries, want none", len(entries))
	}
}

func TestEntriesSurviveARoundTrip(t *testing.T) {
	s := newTestStore(t)
	want := []Entry{
		{LocalPort: 5432, Target: "db.internal:5432"},
		{LocalPort: 8080, Target: "127.0.0.1:3000", Paused: true},
	}
	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip changed the entries:\n got %+v\nwant %+v", got, want)
	}
}

func TestSaveCreatesTheDirectoryPrivately(t *testing.T) {
	s := newTestStore(t)
	if err := s.Save(nil); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(filepath.Dir(s.Path()))
	if err != nil {
		t.Fatalf("the directory was not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory", filepath.Dir(s.Path()))
	}
}

func TestTheFileCarriesAVersion(t *testing.T) {
	s := newTestStore(t)
	if err := s.Save([]Entry{{LocalPort: 8080, Target: "h:1"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "version: 1") {
		t.Errorf("no version in the file:\n%s", data)
	}
}

func TestAnUnreadableFileIsRefusedAndLeftAlone(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(filepath.Dir(s.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	const garbage = "forwards: [this is: not yaml\n"
	if err := os.WriteFile(s.Path(), []byte(garbage), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := s.Load()
	if !errors.Is(err, ErrUnreadable) {
		t.Fatalf("Load = %v, want ErrUnreadable", err)
	}
	if !strings.Contains(err.Error(), s.Path()) {
		t.Errorf("the error does not name the file: %v", err)
	}
	if got, _ := os.ReadFile(s.Path()); string(got) != garbage {
		t.Errorf("Load touched the file it refused: %q", got)
	}
}

func TestSavingAfterAnUnreadableFileMovesItAside(t *testing.T) {
	s := newTestStore(t)
	if err := os.MkdirAll(filepath.Dir(s.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	const garbage = "forwards: [this is: not yaml\n"
	if err := os.WriteFile(s.Path(), []byte(garbage), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("Load = %v, want ErrUnreadable", err)
	}

	if err := s.Save([]Entry{{LocalPort: 8080, Target: "h:1"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	kept, err := os.ReadFile(s.Path() + ".unreadable")
	if err != nil {
		t.Fatalf("the unreadable file was not kept: %v", err)
	}
	if string(kept) != garbage {
		t.Errorf("the kept copy differs from the original: %q", kept)
	}
	if got, err := s.Load(); err != nil || len(got) != 1 {
		t.Errorf("after the move, Load = %v, %v; want the one saved entry", got, err)
	}
}

func TestASaveLeavesNoTemporaryFileBehind(t *testing.T) {
	s := newTestStore(t)
	for range 3 {
		if err := s.Save([]Entry{{LocalPort: 8080, Target: "h:1"}}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	names, err := os.ReadDir(filepath.Dir(s.Path()))
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].Name() != FileName {
		var got []string
		for _, n := range names {
			got = append(got, n.Name())
		}
		t.Errorf("the directory holds %v, want only %s", got, FileName)
	}
}
