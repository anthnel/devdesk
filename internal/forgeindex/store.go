package forgeindex

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Path is where a context's index lives: ~/.devdesk/cache/forge/<context>.json.
// Empty when there is no home directory, and then nothing is read or written.
func Path(contextName string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".devdesk", "cache", "forge", fileName(contextName))
}

// fileName keeps a context name from reaching outside the directory. Context
// names are file names already (~/.devdesk/contexts/<name>.yaml), so this only
// guards against a separator nobody should have typed.
func fileName(contextName string) string {
	name := strings.NewReplacer("/", "_", `\`, "_", "..", "_").Replace(contextName)
	if name == "" {
		name = "default"
	}
	return name + ".json"
}

// Load reads an index. A missing file, an empty path and a file of another
// version all answer (nil, nil): there is simply nothing to show until the walk
// lands. A file that does not parse is an error, so it gets logged.
func Load(path string) (*Index, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ix Index
	if err := json.Unmarshal(raw, &ix); err != nil {
		return nil, err
	}
	if ix.Version != Version {
		return nil, nil
	}
	ix.reindex()
	return &ix, nil
}

// Save writes an index atomically: a temporary file in the same directory,
// then a rename, so a reader never sees half of one.
//
// The file lists what a token can see, which is private to its owner, hence
// 0700 on the directory and 0600 on the file.
func Save(path string, ix *Index) error {
	if path == "" || ix == nil {
		return nil
	}
	raw, err := json.Marshal(ix)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
