package security

import (
	"github.com/anthnel/devdesk/internal/scan"
)

// fixStatus is the fix line of a finding detail, in words: a colour or a key
// name alone would not say it (Rule 134), so the status never names a key.
type fixStatus struct {
	Label   string // words, never a colour alone; "" = no status line
	Detail  string // reason, MissingFor text, catalog rule title, or ""
	Warning string // remediation.Rule.Warning, or ""
}

// fixStatusOf says whether f can be fixed and why not. For a misconfiguration
// it reads misconfigFixAvailability, the computation behind the shortcut column,
// so the detail and the header agree. Secrets, licenses and CI scores have no
// fix status: the zero value, no line.
func fixStatusOf(f scan.Finding, onDisk bool) fixStatus {
	switch scan.Categorize(f) {
	case scan.CategoryVulnerability:
		if f.FixedIn != "" {
			return fixStatus{Label: "Fix available", Detail: "Fixed in " + f.FixedIn}
		}
		return fixStatus{Label: "No fix available", Detail: "No fixed version is published"}
	case scan.CategoryMisconfiguration:
		return misconfigFixStatus(f, onDisk)
	}
	return fixStatus{}
}

func misconfigFixStatus(f scan.Finding, onDisk bool) fixStatus {
	rule, a := misconfigFixAvailability(f, onDisk)
	inCatalog := rule.AVDID != ""
	switch {
	case a.Enabled():
		return fixStatus{Label: "Built-in fix available", Detail: rule.Title, Warning: rule.Warning}
	case !onDisk && inCatalog:
		return fixStatus{Label: "No built-in fix here", Detail: a.Reason}
	case !inCatalog:
		// On an image the availability reason is the image one; the finding's own
		// reason is what explains the missing fix.
		return fixStatus{Label: "No built-in fix", Detail: noFixReason(f)}
	}
	return fixStatus{Label: "No built-in fix for this instance", Detail: a.Reason}
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
