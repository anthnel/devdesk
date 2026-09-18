package template

import (
	"bytes"
	"context"
	"fmt"

	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/oci"
)

// Credentials are what fetching may need, resolved by the caller for the
// source's own host.
//
// This package never decides which host a secret may go to: that is the
// caller's, exactly as it is for git.Clone. A token handed in for one host and
// used on another is a leak, so a zero Credentials is the safe default.
type Credentials struct {
	Token    string // git over HTTPS
	Username string // OCI registry
	Password string // OCI registry
}

// Fetch returns the files of the template at src.
func Fetch(ctx context.Context, src Source, creds Credentials) ([]File, error) {
	if err := src.Validate(); err != nil {
		return nil, err
	}

	switch src.Kind {
	case KindGit:
		raw, err := git.ArchiveRemote(ctx, src.URL, src.Ref, src.Path, creds.Token)
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", src.URL, err)
		}
		return readArchive(raw)

	case KindLocal:
		raw, err := git.ArchiveLocal(ctx, src.Path, src.Ref, "")
		if err != nil {
			return nil, err
		}
		return readArchive(raw)

	default: // KindOCI, Validate has refused anything else
		return fetchOCI(ctx, src, creds)
	}
}

func readArchive(raw []byte) ([]File, error) {
	files, err := oci.ReadTar(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return fromArchive(files), nil
}

func fetchOCI(ctx context.Context, src Source, creds Credentials) ([]File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := oci.NewClient(src.URL, creds.Username, creds.Password)
	tmpl, err := client.DownloadTemplate(src.Path, src.Ref)
	if err != nil {
		return nil, fmt.Errorf("downloading %s:%s: %w", src.Path, src.Ref, err)
	}
	return fromArchive(tmpl.Entries), nil
}

func fromArchive(files []oci.ArchiveFile) []File {
	out := make([]File, len(files))
	for i, f := range files {
		out[i] = File{Path: f.Path, Content: f.Content, Executable: f.Executable}
	}
	return out
}
