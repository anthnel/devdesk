package security

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthnel/devdesk/internal/git"
	"github.com/anthnel/devdesk/internal/patch"
	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
)

// The change a built-in fix would make (DEVDESK-753). One computation feeds the
// preview and the confirmation of ctrl+o, so what is shown is what is written.

// reasonPreviewStale is why a fix is refused on a file written after the scan:
// the lines the scan reported may no longer be the lines of the file.
const reasonPreviewStale = "The file changed after the scan — re-scan to preview the fix"

// misconfigChange is the fix as it would land: the pure product shared by the
// preview and ctrl+o.
type misconfigChange struct {
	// File is the slash path, relative to the target, of the file the fix edits.
	File string
	// Lines is the formatted diff, sanitized, uncapped and uncut.
	Lines []string
	// Updated is the exact bytes ctrl+o writes.
	Updated []byte
	Scope   remediation.Scope
}

// misconfigChangeOf is the pure part of the computation: no I/O. It returns a
// reason when the rule declines, and an error when the rewrite fails.
func misconfigChangeOf(rule remediation.Rule, rel string, original []byte, f scan.Finding) (misconfigChange, string, error) {
	edits, reason := rule.Fix(original, f)
	if reason != "" {
		return misconfigChange{}, reason, nil
	}
	updated, err := patch.Rewrite(original, edits)
	if err != nil {
		return misconfigChange{}, "", fmt.Errorf("%s: %w", rel, err)
	}
	diff, err := patch.Diff(rel, original, edits)
	if err != nil {
		return misconfigChange{}, "", fmt.Errorf("%s: %w", rel, err)
	}
	return misconfigChange{
		File: rel, Lines: changeLines(diff), Updated: updated,
		Scope: rule.ScopeOf(updated, edits),
	}, "", nil
}

// changeLines formats a patch.Diff for display: the two header lines are
// dropped (the file is named elsewhere), a hunk marker is indented by two spaces
// so every line starts with +, - or a space, and each line is made safe for the
// terminal. Nothing is capped or cut.
//
// Only the first two lines can be headers: a removed line "-- x" reads "--- x"
// further down and is a change.
func changeLines(diff string) []string {
	lines := strings.Split(diff, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) >= 2 && strings.HasPrefix(lines[0], "--- ") && strings.HasPrefix(lines[1], "+++ ") {
		lines = lines[2:]
	}
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if strings.HasPrefix(l, "@@") {
			l = "  " + l
		}
		out[i] = withoutControlSpace(l)
	}
	return out
}

// previewStale says whether a file written at modTime changed after the scan
// that ended at scannedAt. A time that is not known cannot make a file stale.
func previewStale(modTime, scannedAt time.Time) bool {
	return !modTime.IsZero() && !scannedAt.IsZero() && modTime.After(scannedAt)
}

// withoutPath is err's text for a log, with the operation and the absolute path
// of an I/O error left out ("open /home/me/src/Dockerfile: permission denied"
// reads "permission denied"). A relative path travels beside it, in the line the
// caller writes: the logs of a shared machine are not a map of its disks.
func withoutPath(err error) string {
	text := err.Error()
	var pe *fs.PathError
	if errors.As(err, &pe) {
		text = strings.Replace(text, pe.Op+" "+pe.Path+": ", "", 1)
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		text = strings.Replace(text, le.Op+" "+le.Old+" "+le.New+": ", "", 1)
	}
	return text
}

// previewFailure words an error for the screen, never showing err.Error()
// (Rule 128): the file is named, the detail is in the log.
func previewFailure(rel string, err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return rel + " no longer exists"
	case errors.Is(err, fs.ErrPermission):
		return rel + " cannot be read"
	default:
		return "the fix could not be computed — check logs"
	}
}

// resolveInside is the absolute path of file under target and its slash path
// relative to target, or the reason it is refused: the path leaves the target
// lexically, or a link on the way leads out of it. Nothing is read.
func resolveInside(target, file string) (path, rel, reason string) {
	path, a := findingFilePath(scan.Finding{File: file}, target, true)
	if !a.Enabled() {
		return "", "", a.Reason
	}
	if !staysInside(target, path) {
		return "", "", reasonFileLinkOutsideTarget
	}
	r, err := filepath.Rel(filepath.Clean(target), path)
	if err != nil {
		return "", "", reasonFileOutsideTarget
	}
	return path, filepath.ToSlash(r), ""
}

// maxFixFileBytes is the largest file the detail reads to compute a fix: a
// Dockerfile or a manifest is a few kilobytes, and the scanned tree is not
// trusted to keep to that (DEVDESK-753). Both Original and Updated are kept for
// the write, so the memory a preview holds is bounded by twice this.
const maxFixFileBytes = 1 << 20

// What a file DevDesk will not read is refused with.
const (
	reasonFileNotRegular = "This is not a regular file — DevDesk will not read it"
	reasonFileTooLarge   = "This file is larger than 1 MiB — too large to preview the fix"
)

// errFileTooLarge is readBounded's refusal of a reader holding more than the cap.
var errFileTooLarge = errors.New("file is larger than the cap")

// checkFile declines a file that is not a regular file, is over the cap, or was
// written after the scan.
func checkFile(info fs.FileInfo, scannedAt time.Time) error {
	switch {
	case !info.Mode().IsRegular():
		return errDeclined{reasonFileNotRegular}
	case info.Size() > maxFixFileBytes:
		return errDeclined{reasonFileTooLarge}
	case previewStale(info.ModTime(), scannedAt):
		return errDeclined{reasonPreviewStale}
	}
	return nil
}

// checkFileOn is a stat failure or a decline of checkFile for the file at path.
// Nothing is opened, so a FIFO or a device is refused before it can block.
func checkFileOn(path, rel string, scannedAt time.Time) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	return checkFile(info, scannedAt)
}

// readBounded reads r whole, and refuses it when it holds more than the cap: the
// size read earlier is not trusted, the file may have grown since.
func readBounded(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxFixFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFixFileBytes {
		return nil, errFileTooLarge
	}
	return data, nil
}

// readChecked reads the file at path under the checks of checkFile, applied to
// the descriptor that is read rather than to the path alone: the file cannot be
// swapped between the check and the read, and a write during the read is seen
// by comparing the descriptor before and after.
func readChecked(path, rel string, scannedAt time.Time) ([]byte, error) {
	if err := checkFileOn(path, rel, scannedAt); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|openNoBlock, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	defer func() { _ = f.Close() }()
	before, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	if err := checkFile(before, scannedAt); err != nil {
		return nil, err
	}
	data, err := readBounded(f)
	if errors.Is(err, errFileTooLarge) {
		return nil, errDeclined{reasonFileTooLarge}
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	after, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		return nil, errDeclined{reasonPreviewStale}
	}
	return data, nil
}

// computeMisconfigFix is the I/O side of the fix, shared by the Cmd ctrl+o starts
// and by anything else that wants the same answer: the path is checked (lexically,
// then through links), the files are measured against the end of the scan, and
// only then is the edited file read. scannedAt is the end time of the result on
// screen; the zero time never makes a file stale.
func computeMisconfigFix(target string, f scan.Finding, scannedAt time.Time) MisconfigFixPreparedMsg {
	fail := func(err error) MisconfigFixPreparedMsg {
		return MisconfigFixPreparedMsg{Target: target, Rule: f.ID, Err: err}
	}
	decline := func(reason string) MisconfigFixPreparedMsg { return fail(errDeclined{reason}) }

	rule, ok := remediation.FixFor(f)
	if !ok {
		return decline(noFixReason(f))
	}
	// A rule that edits another file than the finding's reads the finding's own
	// to find it, so that one is checked before the rule is asked.
	findingPath := ""
	if rule.Target != nil {
		var reason string
		if findingPath, _, reason = resolveInside(target, f.File); reason != "" {
			return decline(reason)
		}
	}
	edited, reason := rule.FileFor(target, f)
	if reason != "" {
		return decline(reason)
	}
	path, rel, reason := resolveInside(target, edited)
	if reason != "" {
		return decline(reason)
	}
	if err := checkFileOn(path, rel, scannedAt); err != nil {
		return fail(err)
	}
	if findingPath != "" && findingPath != path {
		if err := checkFileOn(findingPath, f.File, scannedAt); err != nil {
			return fail(err)
		}
	}
	original, err := readChecked(path, rel, scannedAt)
	if err != nil {
		return fail(err)
	}
	change, reason, err := misconfigChangeOf(rule, rel, original, f)
	if err != nil {
		return fail(err)
	}
	if reason != "" {
		return decline(reason)
	}
	file := preparedWrite{File: rel, Path: path, Original: original, Updated: change.Updated}
	if file.State, err = git.StateOf(path); err != nil {
		log.Printf("ERROR [security/misconfig] git state of %s: %s", rel, withoutPath(err))
		file.StateErr = true
	}
	body := rule.Title + "\n\n"
	if rule.Warning != "" {
		body += "Warning: " + rule.Warning + "\n\n"
	}
	return MisconfigFixPreparedMsg{
		Target: target, File: file, FindingFile: f.File,
		Rule: f.ID, Instance: instanceOf(f), Body: body + strings.Join(change.Lines, "\n") + "\n",
		Scope: change.Scope, Lines: change.Lines,
	}
}
