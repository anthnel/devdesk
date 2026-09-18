package template

import (
	"bytes"
	"context"
	"errors"
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

// The most a template may hold. A template is read whole into memory and sent
// to the forge in a single initial commit, so what it can be is bounded by the
// API, not by taste:
//
//   - GitLab takes every file in one request, so the size of the body decides
//     (base64 makes it a third larger than the files).
//   - GitHub takes one request per file, one after the other, so the count
//     decides: at a few hundred milliseconds each, 500 files is minutes.
//
// A realistic template — a service with its wrappers and its CI — is a few dozen
// files. Past 500 it is almost always a repository that committed its
// dependencies or its build output, which is not a template.
//
// The numbers are estimates, not measurements. The one to check against a real
// instance is GitLab's maximum request body: if it is under about 43 MiB (32 MiB
// once base64-encoded), MaxBytes has to come down.
const (
	MaxFiles = 500
	MaxBytes = 32 << 20
)

// Fetch returns the files of the template at src, or an error saying why it
// cannot be used — including that it is larger than a template may be.
func Fetch(ctx context.Context, src Source, creds Credentials) ([]File, error) {
	files, err := fetch(ctx, src, creds)
	if err != nil {
		return nil, err
	}
	if err := checkLimits(files); err != nil {
		return nil, err
	}
	return files, nil
}

// checkLimits refuses a template past MaxFiles or MaxBytes, naming the biggest
// file so the reader knows where to look. It runs before anything is created on
// the forge: a refusal after the repository exists would leave an empty one.
func checkLimits(files []File) error {
	if len(files) > MaxFiles {
		return fmt.Errorf("the template has %d files, more than the %d a template may have", len(files), MaxFiles)
	}

	total, biggest := 0, File{}
	for _, f := range files {
		total += len(f.Content)
		if len(f.Content) > len(biggest.Content) {
			biggest = f
		}
	}
	if total > MaxBytes {
		return fmt.Errorf("the template is %s, more than the %s a template may be — the largest file is %s (%s)",
			FormatSize(total), FormatSize(MaxBytes), biggest.Path, FormatSize(len(biggest.Content)))
	}
	return nil
}

// FormatSize writes a byte count the way the application says it: 1.5 MiB.
func FormatSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func fetch(ctx context.Context, src Source, creds Credentials) ([]File, error) {
	if err := src.Validate(); err != nil {
		return nil, err
	}

	switch src.Kind {
	case KindGit:
		raw, err := git.ArchiveRemote(ctx, src.URL, src.Ref, src.Path, creds.Token, archiveCap)
		if err != nil {
			return nil, tooLargeOr(fmt.Errorf("fetching %s: %w", src.URL, err), err)
		}
		return readArchive(raw)

	case KindLocal:
		raw, err := git.ArchiveLocal(ctx, src.Path, src.Ref, "", archiveCap)
		if err != nil {
			return nil, tooLargeOr(err, err)
		}
		return readArchive(raw)

	default: // KindOCI, Validate has refused anything else
		return fetchOCI(ctx, src, creds)
	}
}

// archiveCap bounds a tar stream from git: the content a template may hold plus
// a header and padding per file, so a template at the limits still fits.
const archiveCap = MaxBytes + MaxFiles*2048

func readArchive(raw []byte) ([]File, error) {
	files, err := oci.ReadTarLimited(bytes.NewReader(raw), archiveLimits)
	if err != nil {
		return nil, explainArchiveError(err)
	}
	return fromArchive(files), nil
}

var archiveLimits = oci.Limits{Files: MaxFiles, Bytes: MaxBytes}

// errTooLarge is what a template past the limits reports, however early it was
// noticed — checkLimits has the exact figures for the ones that got that far.
var errTooLarge = fmt.Errorf("the template is larger than a template may be (%d files, %s)", MaxFiles, FormatSize(MaxBytes))

// tooLargeOr says the template is too large when err is a limit being hit, and
// gives wrapped otherwise.
func tooLargeOr(wrapped, err error) error {
	if errors.Is(err, git.ErrOutputTooLarge) || errors.Is(err, oci.ErrArchiveTooLarge) {
		return errTooLarge
	}
	return wrapped
}

// explainArchiveError turns a reader's refusal into something a person can act
// on: what was wrong, and what to do about it.
func explainArchiveError(err error) error {
	if errors.Is(err, oci.ErrArchiveTooLarge) {
		return errTooLarge
	}
	if errors.Is(err, oci.ErrNotRegularFile) {
		return fmt.Errorf("%w — a template cannot carry links or special files; replace it with a regular file", err)
	}
	return err
}

func fetchOCI(ctx context.Context, src Source, creds Credentials) ([]File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	client := oci.NewClient(src.URL, creds.Username, creds.Password)
	client.Limits = archiveLimits
	tmpl, err := client.DownloadTemplate(src.Path, src.Ref)
	if err != nil {
		return nil, explainArchiveError(fmt.Errorf("downloading %s:%s: %w", src.Path, src.Ref, err))
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
