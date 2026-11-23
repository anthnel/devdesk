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
	// Unfixed is set on a GroupAll fix whose members have no fixed version
	// (DEVDESK-754). Target, Options and Command are empty then. Group never
	// returns one.
	Unfixed bool
	// Position tells apart the vulnerabilities that name no package and share an
	// ID, an ecosystem and an installed version: it is 1 for the first of them,
	// 2 for the next one, and 0 for a row that names a package.
	Position int
	// Members are the findings behind the fix, in the order they were given. Its
	// length is len(CVEs).
	Members []scan.Finding
}

// FixKey identifies a Fix across two scans: what it is about, never what it
// would move to, so a re-scan that changes the target or the members finds the
// same row again (DEVDESK-754).
type FixKey struct {
	Ecosystem, Package, Installed string
	Unfixed                       bool
	// Position is Fix.Position: without it a vulnerability with no package
	// would share its key with a package named like its ID.
	Position int
}

// Key is the identity of f.
func (f Fix) Key() FixKey {
	return FixKey{Ecosystem: f.Ecosystem, Package: f.Package, Installed: f.Installed, Unfixed: f.Unfixed, Position: f.Position}
}

// GroupAll is Group plus what Group leaves out: one Unfixed Fix per (ecosystem,
// package, installed) with no fixed version, and one Fix per vulnerability that
// names no package (Package = its ID, no Command). The sum of len(CVEs) over the
// fixes that are not Unfixed is Summarize(findings).Fixable().
//
// Worst severity first, then the most CVEs, then package, then installed
// version, the fixable row before the unfixed one: the order does not depend on
// the order of findings.
func GroupAll(findings []scan.Finding) []Fix {
	fixes := build(findings, true)
	sort.SliceStable(fixes, func(i, j int) bool { return fixLess(fixes[i], fixes[j]) })
	return fixes
}

// fixLess is the total order of GroupAll.
func fixLess(a, b Fix) bool {
	if ra, rb := severityRank(a.Severity), severityRank(b.Severity); ra != rb {
		return ra > rb
	}
	if len(a.CVEs) != len(b.CVEs) {
		return len(a.CVEs) > len(b.CVEs)
	}
	if a.Package != b.Package {
		return a.Package < b.Package
	}
	if a.Installed != b.Installed {
		if c, ok := scan.CompareVersionsIn(a.Ecosystem, a.Installed, b.Installed); ok && c != 0 {
			return c < 0
		}
		return a.Installed < b.Installed
	}
	return !a.Unfixed && b.Unfixed
}

type fixKey struct {
	ecosystem, pkg, installed string
	unfixed                   bool
	// n is the 1-based position of a vulnerability that names no package, so
	// each one stays its own row; 0 for a named package.
	n int
}

// group is a Fix being built, plus what only the building needs.
type group struct {
	fix Fix
	// ambiguous is set when one CVE listed several fixed versions that
	// scan.PickFixed could not choose between.
	ambiguous bool
	// bare is set on a vulnerability that names no package: there is no
	// package to name in a command.
	bare bool
}

// Group collects the fixable vulnerabilities into one Fix per (ecosystem,
// package, installed version), worst severity first. A package installed in
// two versions is two fixes: each has its own target.
func Group(findings []scan.Finding) []Fix {
	fixes := build(findings, false)
	sort.SliceStable(fixes, func(i, j int) bool {
		return severityRank(fixes[i].Severity) > severityRank(fixes[j].Severity)
	})
	return fixes
}

// build collects the vulnerabilities into fixes, in the order they first
// appear. With all unset it keeps only the fixable ones that name a package.
func build(findings []scan.Finding, all bool) []Fix {
	byKey := map[fixKey]*group{}
	var order []fixKey
	bareSeen := map[fixKey]int{}

	for i, f := range findings {
		if scan.Categorize(f) != scan.CategoryVulnerability {
			continue
		}
		unfixed, bare := f.FixedIn == "", f.PkgName == ""
		if !all && (unfixed || bare) {
			continue
		}
		key := fixKey{ecosystem: f.Ecosystem, pkg: f.PkgName, installed: f.Version, unfixed: unfixed}
		position := 0
		if bare {
			key.pkg, key.n = f.ID, i+1
			same := fixKey{ecosystem: key.ecosystem, pkg: key.pkg, installed: key.installed, unfixed: unfixed}
			bareSeen[same]++
			position = bareSeen[same]
		}
		g, seen := byKey[key]
		if !seen {
			g = &group{bare: bare, fix: Fix{
				Class: f.Class, Ecosystem: f.Ecosystem, Package: key.pkg,
				Installed: f.Version, Severity: f.Severity, Unfixed: unfixed, Position: position,
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
	return fixes
}

// add records one vulnerability: its ID, its severity, and the version it needs.
func (g *group) add(f scan.Finding) {
	g.fix.CVEs = append(g.fix.CVEs, f.ID)
	g.fix.Members = append(g.fix.Members, f)
	if severityRank(f.Severity) > severityRank(g.fix.Severity) {
		g.fix.Severity = f.Severity
	}
	if g.fix.Unfixed {
		return
	}
	need := scan.PickFixedIn(f.Ecosystem, f.Version, f.FixedIn)
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
	if fix.Unfixed {
		return fix
	}
	sort.Strings(fix.Options)
	if !g.ambiguous {
		fix.Target = highest(fix.Ecosystem, fix.Options)
	}
	if fix.Target != "" && !g.bare {
		fix.Command, _ = scan.FixCommand(fix.Ecosystem, fix.Package, fix.Target)
	}
	return fix
}

// highest returns the greatest of versions, or "" if two of them cannot be
// ordered. A lone version is returned as is, orderable or not.
func highest(ecosystem string, versions []string) string {
	best := versions[0]
	for _, v := range versions[1:] {
		c, ok := scan.CompareVersionsIn(ecosystem, v, best)
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
