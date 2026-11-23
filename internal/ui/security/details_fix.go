package security

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/anthnel/devdesk/internal/remediation"
	"github.com/anthnel/devdesk/internal/scan"
	"github.com/anthnel/devdesk/internal/ui/shortcut"
)

// fixState is the state behind the Fix line of a finding detail (DEVDESK-751).
type fixState int

const (
	fixManual fixState = iota
	fixBuiltIn
	fixBuiltInDeclined
	fixNoFixPublished
	fixCannotFixHere
	fixNoGuidance
)

// The closed vocabulary of the Fix line (AC1). Only labelBuiltInUnavailable
// carries a reason, which Label appends after a colon.
const (
	labelBuiltInAvailable   = "Built-in fix available"
	labelBuiltInUnavailable = "Built-in fix unavailable"
	labelManual             = "Manual"
	labelNoFixPublished     = "No fix published"
	labelCannotFixHere      = "Cannot fix here"
	labelNoGuidance         = "No guidance available"
)

// Reasons a finding's file cannot be opened from the detail (Rule 130).
const (
	reasonFileOutsideTarget = "This file is outside the scanned directory"
	reasonNoFile            = "This finding names no file"
	// reasonFileLinkOutsideTarget is the answer of enter, which follows links:
	// the header cannot know it (it reads no disk).
	reasonFileLinkOutsideTarget = "This file is reached through a link that leaves the scanned directory"
)

// fixStatus is the Fix line of a finding detail, in words: a colour or a key
// name alone would not say it (Rule 134), so the status never names a key.
type fixStatus struct {
	State   fixState
	Reason  string // why the built-in fix is unavailable or cannot run here
	Effect  string // what the fix does: the catalog rule's title, or "Fixed in <v>"
	Warning string // remediation.Rule.Warning, or ""
}

// Label is the closed-vocabulary text of the status.
func (s fixStatus) Label() string {
	switch s.State {
	case fixBuiltIn:
		return labelBuiltInAvailable
	case fixBuiltInDeclined:
		return labelBuiltInUnavailable + ": " + s.Reason
	case fixManual:
		return labelManual
	case fixNoFixPublished:
		return labelNoFixPublished
	case fixCannotFixHere:
		return labelCannotFixHere
	}
	return labelNoGuidance
}

// fixStatusOf says whether f can be fixed and why not. For a misconfiguration
// it reads misconfigFixAvailability, the computation behind the shortcut column,
// so the detail and the header agree (Rule 130).
func fixStatusOf(f scan.Finding, onDisk bool) fixStatus {
	switch scan.Categorize(f) {
	case scan.CategoryMisconfiguration:
		return misconfigFixStatus(f, onDisk)
	case scan.CategoryVulnerability:
		if f.FixedIn != "" {
			return fixStatus{State: fixManual, Effect: "Fixed in " + f.FixedIn}
		}
		return fixStatus{State: fixNoFixPublished}
	}
	return guidanceStatus(f)
}

// misconfigFixStatus is the status of a misconfiguration, in the order of
// misconfigFixAvailability: an image first, then the catalog, then the guidance
// a finding the catalog does not know can still carry.
func misconfigFixStatus(f scan.Finding, onDisk bool) fixStatus {
	rule, a := misconfigFixAvailability(f, onDisk)
	switch {
	case !onDisk:
		return fixStatus{State: fixCannotFixHere, Reason: a.Reason}
	case a.Enabled():
		return fixStatus{State: fixBuiltIn, Effect: rule.Title, Warning: rule.Warning}
	case rule.AVDID != "":
		return fixStatus{State: fixBuiltInDeclined, Reason: a.Reason}
	}
	return guidanceStatus(f)
}

// guidanceStatus is Manual when the finding tells how to fix it by hand, and
// No guidance available when it does not.
func guidanceStatus(f scan.Finding) fixStatus {
	if hasGuidance(f) {
		return fixStatus{State: fixManual}
	}
	return fixStatus{State: fixNoGuidance}
}

// hasGuidance says whether f carries any instruction: a Resolution, Steps, a
// FixCommand or a remediation.MissingFor text.
func hasGuidance(f scan.Finding) bool {
	if f.Resolution != "" || f.FixCommand != "" || len(f.Steps) > 0 {
		return true
	}
	_, ok := remediation.MissingFor(f)
	return ok
}

// runCommand is the text under Run and the text Y copies: the FixCommand trimmed,
// when fixAction calls it a command and it is plain words. One computation, two
// readers.
//
// The command is pasted into a shell, and its names and versions come from a
// scan, so anything but words is refused (DEVDESK-751): a line break, a control
// character (an escape sequence, a tab, a carriage return), a blank other than a
// single space, a character that has no visible form (a zero-width or
// right-to-left mark) or a shell metacharacter. A refused command is not lost:
// fixSteps shows its text under Do.
func runCommand(f scan.Finding) (string, bool) {
	label, text := fixAction(f)
	cmd := strings.TrimSpace(text)
	if label != "Run" || cmd == "" || !scan.PlainWords(cmd) {
		return "", false
	}
	return cmd, true
}

// fixSteps are the numbered steps under Do: the steps DevDesk wrote for the
// finding (Steps, first), what a rule the catalog cannot fix is missing, and a
// FixCommand that is not offered under Run: a sentence rather than a command, or
// a command runCommand refuses.
func fixSteps(f scan.Finding) []string {
	steps := append([]string(nil), f.Steps...)
	if _, inCatalog := remediation.FixFor(f); !inCatalog {
		if text, ok := remediation.MissingFor(f); ok {
			steps = append(steps, text)
		}
	}
	if _, offered := runCommand(f); !offered {
		if _, text := fixAction(f); strings.TrimSpace(text) != "" {
			steps = append(steps, text)
		}
	}
	return steps
}

// fact is one labelled value of the Where section.
type fact struct{ Label, Value string }

// whereFacts are the facts under Where, in order: File, Package, Job, Script,
// Match. A path is shown the way shortPaths shows it, with its line.
func whereFacts(f scan.Finding, target string) []fact {
	var facts []fact
	add := func(label, value string) {
		if value != "" {
			facts = append(facts, fact{label, value})
		}
	}
	file := shortPaths(f.File, target)
	if file != "" && f.Line > 0 {
		file += ":" + strconv.Itoa(f.Line)
	}
	add("File", file)
	add("Package", packageFact(f))
	add("Job", f.Job)
	add("Script", f.ScriptLine)
	add("Match", f.Match)
	return facts
}

// packageFact is "name version (kind)", leaving out what is unknown.
func packageFact(f scan.Finding) string {
	if f.PkgName == "" {
		return ""
	}
	out := f.PkgName
	if f.Version != "" {
		out += " " + f.Version
	}
	if kind := packageKind(f.Class); kind != "" {
		out += " (" + kind + ")"
	}
	return out
}

// findingFilePath is the absolute path of the finding's file under the scanned
// directory, or the reason it cannot be opened. It does no I/O: whether the file
// exists is for the viewer to say (Rule 130, "Not knowing"). A path that leaves
// the target, by a parent segment or an absolute path elsewhere, is refused.
func findingFilePath(f scan.Finding, target string, onDisk bool) (string, shortcut.Availability) {
	if !onDisk {
		return "", shortcut.Unavailable(reasonFileNotOnDisk)
	}
	if f.File == "" {
		return "", shortcut.Unavailable(reasonNoFile)
	}
	root := filepath.Clean(target)
	file := filepath.ToSlash(f.File)
	var path string
	switch {
	case file == scanMount || strings.HasPrefix(file, scanMount+"/"):
		path = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(file, scanMount)))
	case filepath.IsAbs(f.File):
		path = filepath.Clean(f.File)
	default:
		path = filepath.Join(root, filepath.FromSlash(file))
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", shortcut.Unavailable(reasonFileOutsideTarget)
	}
	return path, shortcut.Availability{}
}

// sameFinding says whether a and b are one finding across a re-scan: the line
// is not compared, the fix itself moves it.
func sameFinding(a, b scan.Finding) bool {
	return a.Source == b.Source &&
		remediation.RuleKey(a.ID) == remediation.RuleKey(b.ID) &&
		a.File == b.File && a.PkgName == b.PkgName && a.Version == b.Version &&
		instanceOf(a) == instanceOf(b)
}

// fixAction says how to show f.FixCommand: ("Run", cmd) for a real command,
// ("", instruction) for a sentence such as "Update pkg to 1.2", ("", "") when
// there is none.
func fixAction(f scan.Finding) (label, text string) {
	switch {
	case f.FixCommand == "":
		return "", ""
	case scan.IsFixInstruction(f):
		return "", f.FixCommand
	}
	return "Run", f.FixCommand
}
