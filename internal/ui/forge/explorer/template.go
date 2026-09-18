package explorer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anthnel/devdesk/internal/forge"
	"github.com/anthnel/devdesk/internal/template"
)

// templateFetchTimeout bounds fetching a template: a fetch that cannot finish
// must fail the creation before the repository exists, not hang it.
const templateFetchTimeout = 2 * time.Minute

// templateFetcher returns what fetches the files of the template chosen for a
// repository, or a function that returns none when no template was chosen.
//
// Everything the fetch needs is read here, in Update, and carried in the
// closure — it runs on another goroutine and must not reach back into the model
// (Rule 110).
func (m Model) templateFetcher(slug string) func(context.Context) ([]forge.FileChange, error) {
	if slug == "" {
		return func(context.Context) ([]forge.FileChange, error) { return nil, nil }
	}

	path := m.templatesPath
	cfg := m.config
	secrets := m.shared.Secrets.Storage

	return func(ctx context.Context) ([]forge.FileChange, error) {
		if path == "" {
			var err error
			if path, err = template.DefaultPath(); err != nil {
				return nil, err
			}
		}

		store, err := template.Open(path)
		if err != nil {
			return nil, err
		}
		entry, err := store.Get(slug)
		if errors.Is(err, template.ErrNotFound) {
			return nil, fmt.Errorf("template %q is no longer in the catalog", slug)
		}
		if err != nil {
			return nil, err
		}

		ctx, cancel := context.WithTimeout(ctx, templateFetchTimeout)
		defer cancel()
		files, err := template.Fetch(ctx, entry.Source, template.CredentialsFor(cfg, secrets, entry.Source))
		if err != nil {
			return nil, fmt.Errorf("template %q: %w", slug, err)
		}
		return fileChanges(files), nil
	}
}

// fileChanges turns a template's files into the creations of an initial commit.
func fileChanges(files []template.File) []forge.FileChange {
	changes := make([]forge.FileChange, len(files))
	for i, f := range files {
		changes[i] = forge.FileChange{
			Action:     forge.FileCreate,
			Path:       f.Path,
			Content:    f.Content,
			Executable: f.Executable,
		}
	}
	return changes
}
