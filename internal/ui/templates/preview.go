package templates

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/template"
	sharedcomponents "github.com/anthnel/devdesk/internal/ui/components"
	"github.com/anthnel/devdesk/internal/viewer"
)

// previewTimeout bounds a preview: a fetch that cannot finish should say so
// rather than leave the viewer waiting.
const previewTimeout = 2 * time.Minute

// previewSource lists the files a template would put in a new repository. It is
// a viewer.Source, so the read happens in the viewer's own Init and one place
// reports what went wrong (see viewer.OpenRequestMsg).
type previewSource struct {
	entry template.Entry
	creds template.Credentials
}

func (s previewSource) Name() string { return s.entry.Name + " — files" }

// Kind is plain: the listing is text with no structure worth deriving a view of.
func (s previewSource) Kind() viewer.Kind { return viewer.KindPlain }

func (s previewSource) Load() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), previewTimeout)
	defer cancel()

	files, err := template.Fetch(ctx, s.entry.Source, s.creds)
	if err != nil {
		return nil, err
	}
	return []byte(listing(s.entry, files)), nil
}

// listing formats what was fetched: a summary line, where it came from, then
// one line per file with its size and an `x` for the execute bit.
func listing(entry template.Entry, files []template.File) string {
	sorted := append([]template.File(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	total := 0
	for _, f := range sorted {
		total += len(f.Content)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s — %s, %s\n", entry.Name, sharedcomponents.Plural(len(sorted), "file", "files"), humanSize(total))
	fmt.Fprintf(&b, "%s\n\n", sourceSummary(entry.Source)+refSuffix(entry.Source))

	if len(sorted) == 0 {
		b.WriteString("This template has no files: a repository made from it would be empty.\n")
		return b.String()
	}
	for _, f := range sorted {
		mode := "-"
		if f.Executable {
			mode = "x"
		}
		fmt.Fprintf(&b, "%s  %9s  %s\n", mode, humanSize(len(f.Content)), f.Path)
	}
	return b.String()
}

func refSuffix(s template.Source) string {
	if s.Ref == "" {
		return ""
	}
	return " @ " + s.Ref
}

func humanSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
