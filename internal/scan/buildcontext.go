package scan

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/anthnel/devdesk/internal/dockerfile"
)

// The build-context check (§3.81).
//
// A `COPY . .` sends the whole directory to the builder, and whatever a
// .dockerignore does not leave out ends up in a layer of the image. Neither
// Trivy nor hadolint says anything about it — verified on Trivy 0.71.2 and
// hadolint 2.14.0 — and the secret scanners cannot: they read the working tree
// before any build, and `.git` matches no secret pattern. It is not a secret,
// it is the vector — a secret committed and then "removed" is still in the
// history, and the history is still in the layer.
//
// So DevDesk reads it itself. Every finding it emits rests on three facts it
// has checked rather than supposed:
//
//   - a COPY or ADD takes the whole context, in a stage the final image is
//     built from;
//   - the path exists on disk;
//   - no ignore file that may apply leaves it out.
//
// Anything it cannot establish for certain is silence, not a finding.
//
// Only a Dockerfile at the root of the scanned directory is checked. Nothing in
// a Dockerfile says where its build context is — `docker build -f
// sub/Dockerfile .` puts it elsewhere — and at the root, "the context is this
// directory" is the one safe assumption. A Dockerfile further down is logged as
// not checked, with that reason.

// The rule ids, in a namespace of DevDesk's own: an AVD-looking id would
// collide with Trivy's catalog, and the fix catalog is keyed by id.
const (
	BuildContextGitID       = "DEVDESK-CTX-001"
	BuildContextSensitiveID = "DEVDESK-CTX-002"
)

// maxSensitiveListed bounds the paths one finding names; the rest are counted.
const maxSensitiveListed = 5

// The sensitive-file walk is bounded as dockerfile.Find is, and for the same
// reason: a deep tree is vendored or generated, not the project's own files.
const (
	maxContextDepth     = 4
	maxContextSensitive = 200
)

// contextSkippedDirs are not walked for sensitive files. `.git` is reported on
// its own; the others belong to a package manager or a build, and a `.env`
// inside them is not the project's.
var contextSkippedDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".worktrees": true,
	".venv": true, "venv": true, "target": true,
}

// sensitiveNames are the files whose presence in an image is an exposure on its
// own, whatever they hold.
var sensitiveNames = map[string]bool{
	".env": true, "credentials.json": true, ".npmrc": true,
	"id_rsa": true, "id_dsa": true, "id_ecdsa": true, "id_ed25519": true,
}

// envTemplates are the `.env.<suffix>` files that are committed on purpose,
// as a template of the real one.
var envTemplates = map[string]bool{
	"example": true, "sample": true, "template": true, "dist": true,
}

// isSensitiveName reports whether a file name is one whose copy into an image
// is reported.
func isSensitiveName(name string) bool {
	lower := strings.ToLower(name)
	if sensitiveNames[lower] {
		return true
	}
	if suffix, ok := strings.CutPrefix(lower, ".env."); ok {
		return !envTemplates[suffix]
	}
	return strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key")
}

// CheckBuildContext returns what the root Dockerfiles of root would copy into
// their image that should not be there.
func CheckBuildContext(root string) ([]Finding, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		// Nothing to read, and the scanners that were asked to read it say so
		// themselves: one missing directory is one error, not one per stage.
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	hasGit := false
	if info, err := os.Stat(filepath.Join(root, ".git")); err == nil && info.IsDir() {
		// A worktree's .git is a file naming the real one elsewhere: copying it
		// exposes a path, not a history.
		hasGit = true
	}
	var candidates []string // walked once, only if a Dockerfile needs it
	walked := false

	var findings []Finding
	for _, e := range entries {
		if !e.Type().IsRegular() || !dockerfile.IsDockerfileName(e.Name()) {
			continue
		}
		cp, ok, err := wholeContextCopy(filepath.Join(root, e.Name()))
		if err != nil {
			log.Printf("ERROR [scan/build-context] read %s: %v", e.Name(), err)
			continue
		}
		if !ok {
			continue
		}
		sent, ok := contextFilter(root, e.Name())
		if !ok {
			continue
		}
		if hasGit && sent(".git", true) {
			findings = append(findings, gitFinding(root, e.Name(), cp))
		}
		if !walked {
			candidates, walked = sensitiveCandidates(root), true
		}
		var exposed []string
		for _, c := range candidates {
			if sent(c, false) {
				exposed = append(exposed, c)
			}
		}
		if len(exposed) > 0 {
			findings = append(findings, sensitiveFinding(e.Name(), cp, exposed))
		}
	}
	logUncheckedDockerfiles(root)
	return findings, nil
}

// wholeContextCopy returns the first COPY or ADD of the file that takes the
// whole context in a stage the final image is built from.
//
// An earlier stage's layers are not the image's: a builder stage that copies
// everything and hands one binary to the next exposes nothing. A final stage
// built FROM an earlier one carries that stage's layers, so the chain is
// followed. A `COPY --from` of a whole earlier stage is left alone — what it
// carries depends on that stage's paths, which is a guess.
func wholeContextCopy(path string) (dockerfile.Copy, bool, error) {
	content, err := os.ReadFile(path) //nolint:gosec // a Dockerfile of the directory being scanned
	if err != nil {
		return dockerfile.Copy{}, false, err
	}
	stages := dockerfile.Parse(content).Stages
	var best dockerfile.Copy
	found := false
	for _, st := range shippedStages(stages) {
		for _, cp := range st.Copies {
			if cp.TakesWholeContext() && (!found || cp.Line < best.Line) {
				best, found = cp, true
			}
		}
	}
	return best, found, nil
}

// shippedStages are the final stage and the earlier stages it is built FROM.
func shippedStages(stages []dockerfile.Stage) []dockerfile.Stage {
	if len(stages) == 0 {
		return nil
	}
	i := len(stages) - 1
	out := []dockerfile.Stage{stages[i]}
	for stages[i].Kind == dockerfile.KindStage {
		parent := -1
		for j := i - 1; j >= 0; j-- {
			if strings.EqualFold(stages[j].Name, stages[i].Written) {
				parent = j
				break
			}
		}
		if parent < 0 {
			break
		}
		i = parent
		out = append(out, stages[i])
	}
	return out
}

// contextFilter returns whether a path is certainly sent to the builder of the
// Dockerfile at root/name: no ignore file that may apply leaves it out, and
// every one of them can be read with certainty. ok is false when an ignore file
// cannot be read at all, which leaves the Dockerfile unchecked.
func contextFilter(root, name string) (sent func(rel string, tree bool) bool, ok bool) {
	var ignores []dockerfile.Ignore
	for _, rel := range dockerfile.IgnoreFiles(root, name) {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // an ignore file of the directory being scanned
		if err != nil {
			log.Printf("ERROR [scan/build-context] read %s: %v", rel, err)
			return nil, false
		}
		ignores = append(ignores, dockerfile.ParseIgnore(content))
	}
	return func(rel string, tree bool) bool {
		for _, ig := range ignores {
			excluded, certain := ig.Excluded(rel)
			if tree {
				excluded, certain = ig.ExcludesTree(rel)
			}
			if excluded || !certain {
				return false
			}
		}
		return true
	}, true
}

// sensitiveCandidates lists the sensitive files under root, as slash paths
// relative to it, whatever the ignore files say — that is decided per
// Dockerfile.
func sensitiveCandidates(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable subdirectory is skipped, not fatal
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || rel == "." {
			return nil
		}
		if d.IsDir() {
			if contextSkippedDirs[d.Name()] || strings.Count(rel, string(filepath.Separator)) >= maxContextDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && isSensitiveName(d.Name()) {
			out = append(out, filepath.ToSlash(rel))
		}
		if len(out) >= maxContextSensitive {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// logUncheckedDockerfiles says which Dockerfiles copy their whole context but
// were not checked, and why: their context is not known.
func logUncheckedDockerfiles(root string) {
	paths, _, err := dockerfile.Find(root)
	if err != nil {
		return
	}
	for _, rel := range paths {
		if !strings.Contains(rel, "/") {
			continue
		}
		if _, ok, _ := wholeContextCopy(filepath.Join(root, filepath.FromSlash(rel))); ok {
			log.Printf("[scan/build-context] %s: %s not checked — a Dockerfile outside the root does not say where its build context is", root, rel)
		}
	}
}

// ignoreFileWording names the ignore file for a message: the one there is, or
// the fact that there is none.
func ignoreFileWording(root, name string) string {
	files := dockerfile.IgnoreFiles(root, name)
	if len(files) == 0 {
		return "there is no .dockerignore"
	}
	return strings.Join(files, " and ") + " does not exclude it"
}

func gitFinding(root, name string, cp dockerfile.Copy) Finding {
	return Finding{
		ID:     BuildContextGitID,
		Title:  "The build context copies .git into the image",
		Source: SourceBuildContext, IaCType: "dockerfile", Severity: SeverityHigh,
		File: name, Line: cp.Line, EndLine: cp.EndLine,
		Description: "A COPY or ADD of the whole build context sends the .git directory into a layer of the image. " +
			"The complete history comes with it, and a secret committed then deleted stays recoverable from that layer.",
		Message:    fmt.Sprintf("%s takes the whole build context, and %s", cp.Keyword, ignoreFileWording(root, name)),
		Resolution: "Add .git to .dockerignore, or copy only the paths the image needs.",
	}
}

func sensitiveFinding(name string, cp dockerfile.Copy, exposed []string) Finding {
	listed := exposed
	more := ""
	if len(listed) > maxSensitiveListed {
		listed = listed[:maxSensitiveListed]
		more = fmt.Sprintf(" and %d more", len(exposed)-maxSensitiveListed)
	}
	return Finding{
		ID:     BuildContextSensitiveID,
		Title:  "The build context copies sensitive files into the image",
		Source: SourceBuildContext, IaCType: "dockerfile", Severity: SeverityHigh,
		File: name, Line: cp.Line, EndLine: cp.EndLine,
		Description: "A COPY or ADD of the whole build context sends files that usually hold credentials into a layer of the image, " +
			"where anyone who can pull it can read them. A file never committed is invisible to git and to the secret scanners.",
		Message:    fmt.Sprintf("%s takes the whole build context, which holds %s%s", cp.Keyword, strings.Join(listed, ", "), more),
		Resolution: "Exclude these files in .dockerignore, or copy only the paths the image needs.",
	}
}

// runBuildContextStage runs the check as a stage of the scan. It needs no tool,
// so it runs whenever the Misconfiguration category is on for a directory.
//
// It does not set MisconfigScanned: it reads a handful of paths, not the
// Dockerfile's rules, and a target it found nothing in has not been checked for
// misconfigurations by it.
func runBuildContextStage(target string, result *Result, mu *sync.Mutex, notify func(ProgressUpdate)) {
	const stage, label = "build-context", "Build context"
	notify(ProgressUpdate{Stage: stage, Label: label, Status: StageRunning})
	findings, err := CheckBuildContext(target)
	mu.Lock()
	defer mu.Unlock()
	if err != nil {
		recordStageError(result, "build context", err)
		notify(ProgressUpdate{Stage: stage, Label: label, Status: StageError, Detail: err.Error()})
		return
	}
	result.Findings = append(result.Findings, findings...)
	notify(ProgressUpdate{Stage: stage, Label: label, Status: StageDone})
}

// checksBuildContext reports whether the scan runs the build-context check.
func (s *Scanner) checksBuildContext(targetType TargetType) bool {
	if targetType != TargetDirectory {
		return false
	}
	set := s.options.Categories.Category(string(CategoryIDMisconfig))
	return set != nil && set.Enabled
}
