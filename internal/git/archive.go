package git

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// ArchiveLocal returns the committed content of ref (HEAD when empty) in the
// repository at repoPath, as a tar stream.
//
// It is `git archive`, so what comes back is what was committed: no .git,
// nothing ignored, and no uncommitted edit — a template must not depend on the
// state of someone's working tree. subdir, when set, becomes the archive root.
func ArchiveLocal(ctx context.Context, repoPath, ref, subdir string) ([]byte, error) {
	if strings.HasPrefix(ref, "-") {
		return nil, fmt.Errorf("refusing ref %q: it would be read as a git option", ref)
	}
	if _, err := runContext(ctx, repoPath, "", "rev-parse", "--git-dir"); err != nil {
		return nil, fmt.Errorf("%s is not a git repository", repoPath)
	}
	out, err := runContext(ctx, repoPath, "", "archive", "--format=tar", treeish(ref, subdir))
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// ArchiveRemote returns ref (the default branch when empty) of the repository
// at repoURL as a tar stream, fetching a single commit into a throwaway
// directory that is removed before returning.
//
// `fetch --depth 1 <ref>` rather than `clone --branch`: the latter refuses a
// commit SHA, and a template pinned to a SHA is the reproducible kind. token
// is offered over HTTPS exactly as Clone offers it.
func ArchiveRemote(ctx context.Context, repoURL, ref, subdir, token string) ([]byte, error) {
	// Both would be read by git as options, not as a URL or a ref.
	if strings.HasPrefix(repoURL, "-") || strings.HasPrefix(ref, "-") {
		return nil, fmt.Errorf("refusing %q: it would be read as a git option", repoURL+" "+ref)
	}
	dir, err := os.MkdirTemp("", "devdesk-template-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	if _, err := runContext(ctx, dir, "", "init", "--quiet"); err != nil {
		return nil, err
	}
	if ref == "" {
		ref = "HEAD"
	}
	if _, err := runContext(ctx, dir, token, "fetch", "--quiet", "--depth", "1", repoURL, ref); err != nil {
		return nil, err
	}
	out, err := runContext(ctx, dir, "", "archive", "--format=tar", treeish("FETCH_HEAD", subdir))
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// treeish names the tree to archive: `ref` or `ref:subdir`, the latter making
// the subdirectory the archive root.
func treeish(ref, subdir string) string {
	if ref == "" {
		ref = "HEAD"
	}
	subdir = strings.Trim(strings.ReplaceAll(subdir, `\`, "/"), "/")
	if subdir == "" {
		return ref
	}
	return ref + ":" + subdir
}
