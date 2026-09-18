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
// maxBytes (0 for no limit) stops a repository larger than that from being
// buffered whole; the error is ErrOutputTooLarge.
func ArchiveLocal(ctx context.Context, repoPath, ref, subdir string, maxBytes int64) ([]byte, error) {
	if strings.HasPrefix(ref, "-") {
		return nil, fmt.Errorf("refusing ref %q: it would be read as a git option", ref)
	}
	if _, err := runContext(ctx, repoPath, "", "rev-parse", "--git-dir"); err != nil {
		return nil, fmt.Errorf("%s is not a git repository", repoPath)
	}
	tree := treeish(ref, subdir)
	if err := refuseSubmodules(ctx, repoPath, tree); err != nil {
		return nil, err
	}
	out, err := runContextMax(ctx, repoPath, "", maxBytes, "archive", "--format=tar", tree)
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
func ArchiveRemote(ctx context.Context, repoURL, ref, subdir, token string, maxBytes int64) ([]byte, error) {
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
	tree := treeish("FETCH_HEAD", subdir)
	if err := refuseSubmodules(ctx, dir, tree); err != nil {
		return nil, err
	}
	out, err := runContextMax(ctx, dir, "", maxBytes, "archive", "--format=tar", tree)
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

// refuseSubmodules fails when tree holds a submodule. `git archive` writes one
// as an empty directory, so the template would arrive missing what it says it
// contains, and nothing would say so.
func refuseSubmodules(ctx context.Context, dir, tree string) error {
	out, err := runContext(ctx, dir, "", "ls-tree", "-r", tree)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "160000 ") {
			_, path, _ := strings.Cut(line, "\t")
			return fmt.Errorf("%s is a git submodule, which a template cannot carry", path)
		}
	}
	return nil
}
