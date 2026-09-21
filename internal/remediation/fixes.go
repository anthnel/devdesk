// Package remediation turns a scan's findings into what can be done about
// them, without any I/O: which vulnerabilities have a fixed version, whether a
// base image or an application dependency has to move, and the command that
// moves it.
//
// Everything here is a function of the findings. Whether a proposed fix
// actually clears the CVEs is not decided here — that is measured by
// re-scanning, never inferred.
package remediation

import (
	"slices"
	"sort"

	"github.com/anthnel/devdesk/internal/scan"
)

// Summary counts the vulnerabilities that have a fixed version, by what has to
// move to clear them.
type Summary struct {
	// BaseImage is fixed by a package upgrade in the image's OS (or a newer
	// base image); Dependencies by bumping an application dependency.
	BaseImage    int
	Dependencies int
	// Unclassified are fixable findings recorded without a class — a result
	// cached before the class was stored. They are counted apart, never as
	// either class, and disappear at the next scan.
	Unclassified int
	// Unfixed are vulnerabilities with no fixed version yet.
	Unfixed int
}

// Fixable is the number of vulnerabilities that have a fixed version.
func (s Summary) Fixable() int { return s.BaseImage + s.Dependencies + s.Unclassified }

// Summarize counts the vulnerabilities in findings. Secrets, licenses,
// misconfigurations and pipeline findings are not vulnerabilities and are
// ignored.
func Summarize(findings []scan.Finding) Summary {
	var s Summary
	for _, f := range findings {
		if scan.Categorize(f) != scan.CategoryVulnerability {
			continue
		}
		if f.FixedIn == "" {
			s.Unfixed++
			continue
		}
		switch f.Class {
		case scan.ClassOSPackages:
			s.BaseImage++
		case scan.ClassLangPackages:
			s.Dependencies++
		default:
			s.Unclassified++
		}
	}
	return s
}

// Fix is one package that has to move, with every CVE moving it clears.
type Fix struct {
	Class     string
	Ecosystem string
	Package   string
	Installed string
	// Target is the version to move to: the highest of what each CVE needs, so
	// one bump clears them all. Empty when the versions could not be ordered;
	// Options then lists them and none is preferred.
	Target  string
	Options []string
	// Command is the command that performs the move, empty when none is known
	// for the ecosystem or when Target is empty.
	Command  string
	CVEs     []string
	Severity scan.SeverityLevel
}

type fixKey struct{ ecosystem, pkg, installed string }

// group is a Fix being built, plus what only the building needs.
type group struct {
	fix Fix
	// ambiguous is set when one CVE listed several fixed versions that
	// scan.PickFixed could not choose between.
	ambiguous bool
}

// Group collects the fixable vulnerabilities into one Fix per (ecosystem,
// package, installed version), worst severity first. A package installed in
// two versions is two fixes: each has its own target.
func Group(findings []scan.Finding) []Fix {
	byKey := map[fixKey]*group{}
	var order []fixKey

	for _, f := range findings {
		if scan.Categorize(f) != scan.CategoryVulnerability || f.FixedIn == "" || f.PkgName == "" {
			continue
		}
		key := fixKey{f.Ecosystem, f.PkgName, f.Version}
		g, seen := byKey[key]
		if !seen {
			g = &group{fix: Fix{
				Class: f.Class, Ecosystem: f.Ecosystem, Package: f.PkgName,
				Installed: f.Version, Severity: f.Severity,
			}}
			byKey[key] = g
			order = append(order, key)
		}
		g.add(f)
	}

	fixes := make([]Fix, 0, len(order))
	for _, key := range order {
		fixes = append(fixes, byKey[key].done())
	}
	sort.SliceStable(fixes, func(i, j int) bool {
		return severityRank(fixes[i].Severity) > severityRank(fixes[j].Severity)
	})
	return fixes
}

// add records one vulnerability: its ID, its severity, and the version it needs.
func (g *group) add(f scan.Finding) {
	g.fix.CVEs = append(g.fix.CVEs, f.ID)
	if severityRank(f.Severity) > severityRank(g.fix.Severity) {
		g.fix.Severity = f.Severity
	}
	need := scan.PickFixed(f.Version, f.FixedIn)
	if need == "" {
		// Several fixed versions and no way to choose: keep the raw list so
		// the reader sees it, and give up on a single target.
		g.ambiguous = true
		need = f.FixedIn
	}
	if !slices.Contains(g.fix.Options, need) {
		g.fix.Options = append(g.fix.Options, need)
	}
}

// done settles Target and Command: the highest version any CVE needs, so one
// bump clears them all — provided the versions can be ordered.
func (g *group) done() Fix {
	fix := g.fix
	sort.Strings(fix.Options)
	if !g.ambiguous {
		fix.Target = highest(fix.Options)
	}
	if fix.Target != "" {
		fix.Command, _ = scan.FixCommand(fix.Ecosystem, fix.Package, fix.Target)
	}
	return fix
}

// highest returns the greatest of versions, or "" if two of them cannot be
// ordered. A lone version is returned as is, orderable or not.
func highest(versions []string) string {
	best := versions[0]
	for _, v := range versions[1:] {
		c, ok := scan.CompareVersions(v, best)
		if !ok {
			return ""
		}
		if c > 0 {
			best = v
		}
	}
	return best
}

func severityRank(s scan.SeverityLevel) int {
	switch s {
	case scan.SeverityCritical:
		return 4
	case scan.SeverityHigh:
		return 3
	case scan.SeverityMedium:
		return 2
	case scan.SeverityLow:
		return 1
	}
	return 0
}
