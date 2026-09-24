package remediation

import (
	"github.com/anthnel/devdesk/internal/dockerfile"
	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/scan"
)

// gitIgnoreLine is what fixIgnoreGit appends. `.git` in a .dockerignore is
// relative to the context root, which is where the check found the directory.
const gitIgnoreLine = ".git"

// ignoreFileToEdit is the ignore file fixIgnoreGit appends to: the one ignore
// file that applies to the Dockerfile, when there is exactly one.
//
// None is a detection-only finding — creating the file is not this fix (§3.81).
// Two — the Dockerfile's own, which BuildKit reads, and the context's, which
// the legacy builder reads — is declined: which one the user's builds read is
// not something the files say, and editing the wrong one would be a fix a
// re-scan confirms while the build still copies .git.
func ignoreFileToEdit(root string, f scan.Finding) (string, string) {
	files := dockerfile.IgnoreFiles(root, f.File)
	switch len(files) {
	case 0:
		return "", ReasonNoDockerignore
	case 1:
		return files[0], ""
	default:
		return "", ReasonTwoIgnoreFiles
	}
}

// fixIgnoreGit satisfies "the build context copies .git into the image"
// (DEVDESK-CTX-001) by appending `.git` to the ignore file.
//
// An append, like fixAppendCleanup: nothing already in the file is touched, and
// a line at the end wins over any earlier negation, since the last matching
// pattern decides. `.git` in an image is never wanted, which is what makes this
// the one entry of the file DevDesk writes on its own.
func fixIgnoreGit(content []byte, _ scan.Finding) ([]patch.Edit, string) {
	if excluded, certain := dockerfile.ParseIgnore(content).ExcludesTree(".git"); excluded && certain {
		return nil, ReasonGitAlreadyIgnored
	}
	eol := lineEnding(content)
	text := gitIgnoreLine + eol
	if len(content) > 0 && !endsWithNewline(content) {
		text = eol + text
	}
	at := len(content)
	return []patch.Edit{{Span: patch.Span{Start: at, End: at}, New: text}}, ""
}
