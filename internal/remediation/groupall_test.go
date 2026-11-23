package remediation

import (
	"reflect"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-754 (SC1, step 1): GroupAll turns every vulnerability of a scan into
// one row per cause, the fixable ones and the ones with no fix published alike.
// Group keeps its own contract (fixable, named packages only) and gains Members.

// fixView is what a test asserts on, so a table reads as data.
type fixView struct {
	Package   string
	Installed string
	Unfixed   bool
	Target    string
	Command   string
	CVEs      []string
	Options   []string
	Severity  scan.SeverityLevel
}

// viewOf reads nil and empty slices alike: only the content is pinned.
func viewOf(f Fix) fixView {
	orNil := func(s []string) []string {
		if len(s) == 0 {
			return nil
		}
		return s
	}
	return fixView{f.Package, f.Installed, f.Unfixed, f.Target, f.Command, orNil(f.CVEs), orNil(f.Options), f.Severity}
}

func viewsOf(fixes []Fix) []fixView {
	out := make([]fixView, 0, len(fixes))
	for _, f := range fixes {
		out = append(out, viewOf(f))
	}
	return out
}

func TestGroupAllRows(t *testing.T) {
	const (
		crit = scan.SeverityCritical
		high = scan.SeverityHigh
		low  = scan.SeverityLow
	)
	os := scan.ClassOSPackages
	lang := scan.ClassLangPackages
	cases := []struct {
		name     string
		findings []scan.Finding
		want     []fixView
	}{
		{
			name: "three CVEs of one package moved by one version make one row",
			findings: []scan.Finding{
				vuln("CVE-1", "golang.org/x/net", "gomod", lang, "1.0.0", "1.2.0", high),
				vuln("CVE-2", "golang.org/x/net", "gomod", lang, "1.0.0", "1.3.0", high),
				vuln("CVE-3", "golang.org/x/net", "gomod", lang, "1.0.0", "1.4.0", high),
			},
			want: []fixView{{
				Package: "golang.org/x/net", Installed: "1.0.0", Target: "1.4.0",
				Command: "go get golang.org/x/net@v1.4.0",
				CVEs:    []string{"CVE-1", "CVE-2", "CVE-3"}, Options: []string{"1.2.0", "1.3.0", "1.4.0"}, Severity: high,
			}},
		},
		{
			name: "versions that differ by a letter suffix are ordered: the target is the later letter",
			findings: []scan.Finding{
				vuln("CVE-1", "openssl", "alpine", os, "1.1.1k", "1.1.1l", high),
				vuln("CVE-2", "openssl", "alpine", os, "1.1.1k", "1.1.1w", high),
			},
			want: []fixView{{
				Package: "openssl", Installed: "1.1.1k", Target: "1.1.1w", Command: "apk upgrade openssl",
				CVEs: []string{"CVE-1", "CVE-2"}, Options: []string{"1.1.1l", "1.1.1w"}, Severity: high,
			}},
		},
		{
			name: "an OS package gives its command and its delta",
			findings: []scan.Finding{
				vuln("CVE-1", "openssl", "alpine", os, "3.1.0", "3.1.4", crit),
				vuln("CVE-2", "openssl", "alpine", os, "3.1.0", "3.1.5", high),
			},
			want: []fixView{{
				Package: "openssl", Installed: "3.1.0", Target: "3.1.5", Command: "apk upgrade openssl",
				CVEs: []string{"CVE-1", "CVE-2"}, Options: []string{"3.1.4", "3.1.5"}, Severity: crit,
			}},
		},
		{
			name: "one package in two installed versions is two rows, lower version first",
			findings: []scan.Finding{
				vuln("CVE-1", "semver", "npm", lang, "6.0.0", "6.3.1", high),
				vuln("CVE-1", "semver", "npm", lang, "5.0.0", "5.7.2", high),
			},
			want: []fixView{
				{Package: "semver", Installed: "5.0.0", Target: "5.7.2", Command: "npm install semver@5.7.2",
					CVEs: []string{"CVE-1"}, Options: []string{"5.7.2"}, Severity: high},
				{Package: "semver", Installed: "6.0.0", Target: "6.3.1", Command: "npm install semver@6.3.1",
					CVEs: []string{"CVE-1"}, Options: []string{"6.3.1"}, Severity: high},
			},
		},
		{
			name: "a fixed and an unfixed CVE of one package are two rows",
			findings: []scan.Finding{
				vuln("CVE-1", "libfoo", "npm", lang, "1.0.0", "1.0.1", high),
				vuln("CVE-2", "libfoo", "npm", lang, "1.0.0", "", crit),
			},
			want: []fixView{
				{Package: "libfoo", Installed: "1.0.0", Unfixed: true, CVEs: []string{"CVE-2"}, Severity: crit},
				{Package: "libfoo", Installed: "1.0.0", Target: "1.0.1", Command: "npm install libfoo@1.0.1",
					CVEs: []string{"CVE-1"}, Options: []string{"1.0.1"}, Severity: high},
			},
		},
		{
			name: "CVEs with no fix published are one unfixed row per package and version",
			findings: []scan.Finding{
				vuln("CVE-1", "zlib", "alpine", os, "1.2.11", "", low),
				vuln("CVE-2", "zlib", "alpine", os, "1.2.11", "", high),
			},
			want: []fixView{{
				Package: "zlib", Installed: "1.2.11", Unfixed: true,
				CVEs: []string{"CVE-1", "CVE-2"}, Severity: high,
			}},
		},
		{
			name: "versions that cannot be ordered give no target and list the options",
			findings: []scan.Finding{
				vuln("CVE-1", "libfoo", "npm", lang, "1.0.0", "1.0.1", high),
				vuln("CVE-2", "libfoo", "npm", lang, "1.0.0", "weird", high),
			},
			want: []fixView{{
				Package: "libfoo", Installed: "1.0.0",
				CVEs: []string{"CVE-1", "CVE-2"}, Options: []string{"1.0.1", "weird"}, Severity: high,
			}},
		},
		{
			name: "an ambiguous fixed list is kept as written",
			findings: []scan.Finding{
				vuln("CVE-1", "libfoo", "npm", lang, "1.0.0", "abc, def", high),
			},
			want: []fixView{{
				Package: "libfoo", Installed: "1.0.0",
				CVEs: []string{"CVE-1"}, Options: []string{"abc, def"}, Severity: high,
			}},
		},
		{
			name: "secrets, licenses and misconfigurations are not rows",
			findings: []scan.Finding{
				{ID: "S1", Source: scan.SourceGitleaks, PkgName: "x", FixedIn: "1"},
				{ID: "L1", Source: scan.SourceTrivyLicense, PkgName: "y", FixedIn: "2"},
				{ID: "M1", Source: scan.SourceTrivyMisconfig},
			},
			want: []fixView{},
		},
		{
			name:     "nothing in, nothing out",
			findings: nil,
			want:     []fixView{},
		},
		{
			name: "rows are ordered by severity, then by CVE count, then by package",
			findings: []scan.Finding{
				vuln("CVE-1", "zeta", "npm", lang, "1.0.0", "1.0.1", high),
				vuln("CVE-2", "alpha", "npm", lang, "1.0.0", "1.0.1", high),
				vuln("CVE-3", "mid", "npm", lang, "1.0.0", "1.0.1", high),
				vuln("CVE-4", "mid", "npm", lang, "1.0.0", "1.0.1", high),
				vuln("CVE-5", "low-one", "npm", lang, "1.0.0", "1.0.1", low),
				vuln("CVE-6", "crit-one", "npm", lang, "1.0.0", "1.0.1", crit),
			},
			want: []fixView{
				{Package: "crit-one", Installed: "1.0.0", Target: "1.0.1", Command: "npm install crit-one@1.0.1", CVEs: []string{"CVE-6"}, Options: []string{"1.0.1"}, Severity: crit},
				{Package: "mid", Installed: "1.0.0", Target: "1.0.1", Command: "npm install mid@1.0.1", CVEs: []string{"CVE-3", "CVE-4"}, Options: []string{"1.0.1"}, Severity: high},
				{Package: "alpha", Installed: "1.0.0", Target: "1.0.1", Command: "npm install alpha@1.0.1", CVEs: []string{"CVE-2"}, Options: []string{"1.0.1"}, Severity: high},
				{Package: "zeta", Installed: "1.0.0", Target: "1.0.1", Command: "npm install zeta@1.0.1", CVEs: []string{"CVE-1"}, Options: []string{"1.0.1"}, Severity: high},
				{Package: "low-one", Installed: "1.0.0", Target: "1.0.1", Command: "npm install low-one@1.0.1", CVEs: []string{"CVE-5"}, Options: []string{"1.0.1"}, Severity: low},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := viewsOf(GroupAll(tc.findings))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("GroupAll =\n  %+v\nwant\n  %+v", got, tc.want)
			}
		})
	}
}

// A vulnerability that names no package is its own row, and its ID is what the
// row is about: it cannot be merged with another one, and it is not lost.
func TestGroupAllGivesAVulnerabilityWithoutAPackageItsOwnRow(t *testing.T) {
	fixes := GroupAll([]scan.Finding{
		{ID: "CVE-A", Source: scan.SourceTrivy, Severity: scan.SeverityHigh, FixedIn: "1.0.1"},
		{ID: "CVE-B", Source: scan.SourceTrivy, Severity: scan.SeverityHigh},
		vuln("CVE-C", "libfoo", "npm", scan.ClassLangPackages, "1.0.0", "1.0.1", scan.SeverityHigh),
	})
	if len(fixes) != 3 {
		t.Fatalf("got %d rows, want 3: %+v", len(fixes), viewsOf(fixes))
	}
	byPackage := map[string]Fix{}
	for _, f := range fixes {
		byPackage[f.Package] = f
	}
	for _, id := range []string{"CVE-A", "CVE-B"} {
		f, ok := byPackage[id]
		if !ok {
			t.Fatalf("no row whose Package is %q (its ID): %+v", id, viewsOf(fixes))
		}
		if !reflect.DeepEqual(f.CVEs, []string{id}) || len(f.Members) != 1 {
			t.Errorf("%s: CVEs = %v, Members = %d, want just itself", id, f.CVEs, len(f.Members))
		}
	}
	if byPackage["CVE-A"].Unfixed {
		t.Error("CVE-A has a fixed version and is Unfixed")
	}
	if !byPackage["CVE-B"].Unfixed {
		t.Error("CVE-B has no fixed version and is not Unfixed")
	}
}

func TestGroupAllKeepsEveryMemberInInputOrder(t *testing.T) {
	a := vuln("CVE-1", "libfoo", "npm", scan.ClassLangPackages, "1.0.0", "1.0.1", scan.SeverityHigh)
	b := vuln("CVE-2", "libfoo", "npm", scan.ClassLangPackages, "1.0.0", "1.0.2", scan.SeverityLow)
	b.File = "package-lock.json"
	other := vuln("CVE-3", "libbar", "npm", scan.ClassLangPackages, "1.0.0", "", scan.SeverityLow)
	fixes := GroupAll([]scan.Finding{a, other, b})

	byPackage := map[string]Fix{}
	for _, f := range fixes {
		byPackage[f.Package] = f
	}
	if got, want := byPackage["libfoo"].Members, []scan.Finding{a, b}; !reflect.DeepEqual(got, want) {
		t.Errorf("libfoo Members = %+v, want %+v", got, want)
	}
	if got, want := byPackage["libbar"].Members, []scan.Finding{other}; !reflect.DeepEqual(got, want) {
		t.Errorf("libbar Members = %+v, want %+v", got, want)
	}
	for _, f := range fixes {
		if len(f.Members) != len(f.CVEs) {
			t.Errorf("%s: %d Members for %d CVEs", f.Package, len(f.Members), len(f.CVEs))
		}
	}
}

// Group keeps its contract (fixable fixes of named packages) and fills Members;
// it is not widened by GroupAll.
func TestGroupFillsMembersAndStaysFixableOnly(t *testing.T) {
	a := vuln("CVE-1", "libfoo", "npm", scan.ClassLangPackages, "1.0.0", "1.0.1", scan.SeverityHigh)
	b := vuln("CVE-2", "libfoo", "npm", scan.ClassLangPackages, "1.0.0", "1.0.2", scan.SeverityHigh)
	fixes := Group([]scan.Finding{
		a, b,
		vuln("CVE-3", "libbar", "npm", scan.ClassLangPackages, "1.0.0", "", scan.SeverityCritical),
		{ID: "CVE-4", Source: scan.SourceTrivy, Severity: scan.SeverityCritical, FixedIn: "9"},
	})
	if len(fixes) != 1 {
		t.Fatalf("Group returned %d rows, want only libfoo: %+v", len(fixes), viewsOf(fixes))
	}
	if want := []scan.Finding{a, b}; !reflect.DeepEqual(fixes[0].Members, want) {
		t.Errorf("Members = %+v, want %+v", fixes[0].Members, want)
	}
	if fixes[0].Unfixed {
		t.Error("Group returned an Unfixed row")
	}
}

func TestFixKey(t *testing.T) {
	fixed := vuln("CVE-1", "libfoo", "npm", scan.ClassLangPackages, "1.0.0", "1.0.1", scan.SeverityHigh)
	keyOf := func(findings ...scan.Finding) FixKey {
		t.Helper()
		fixes := GroupAll(findings)
		if len(fixes) != 1 {
			t.Fatalf("got %d rows, want 1", len(fixes))
		}
		return fixes[0].Key()
	}

	t.Run("the key says what the row is about", func(t *testing.T) {
		want := FixKey{Ecosystem: "npm", Package: "libfoo", Installed: "1.0.0"}
		if got := keyOf(fixed); got != want {
			t.Errorf("Key = %+v, want %+v", got, want)
		}
	})
	t.Run("an unfixed row says so", func(t *testing.T) {
		unfixed := vuln("CVE-2", "libfoo", "npm", scan.ClassLangPackages, "1.0.0", "", scan.SeverityHigh)
		want := FixKey{Ecosystem: "npm", Package: "libfoo", Installed: "1.0.0", Unfixed: true}
		if got := keyOf(unfixed); got != want {
			t.Errorf("Key = %+v, want %+v", got, want)
		}
	})
	t.Run("a row with no package is keyed by its ID", func(t *testing.T) {
		bare := scan.Finding{ID: "CVE-9", Source: scan.SourceTrivy, Severity: scan.SeverityHigh}
		if got := keyOf(bare); got.Package != "CVE-9" {
			t.Errorf("Key.Package = %q, want the ID", got.Package)
		}
	})
	t.Run("the key survives a re-scan that adds a CVE and moves the target", func(t *testing.T) {
		more := vuln("CVE-7", "libfoo", "npm", scan.ClassLangPackages, "1.0.0", "2.0.0", scan.SeverityCritical)
		if before, after := keyOf(fixed), keyOf(fixed, more); before != after {
			t.Errorf("key changed from %+v to %+v: a row could not be found again", before, after)
		}
	})
	t.Run("two installed versions, two keys", func(t *testing.T) {
		other := vuln("CVE-1", "libfoo", "npm", scan.ClassLangPackages, "2.0.0", "2.0.1", scan.SeverityHigh)
		fixes := GroupAll([]scan.Finding{fixed, other})
		if len(fixes) != 2 || fixes[0].Key() == fixes[1].Key() {
			t.Errorf("two installed versions share a key: %+v", viewsOf(fixes))
		}
	})
	t.Run("fixed and unfixed of one package, two keys", func(t *testing.T) {
		unfixed := vuln("CVE-2", "libfoo", "npm", scan.ClassLangPackages, "1.0.0", "", scan.SeverityHigh)
		fixes := GroupAll([]scan.Finding{fixed, unfixed})
		if len(fixes) != 2 || fixes[0].Key() == fixes[1].Key() {
			t.Errorf("a fixed and an unfixed row share a key: %+v", viewsOf(fixes))
		}
	})
	t.Run("two CVEs with no package and the same ID, two keys", func(t *testing.T) {
		a := scan.Finding{ID: "CVE-9", Source: scan.SourceTrivy, Ecosystem: "npm", Version: "1.0", Severity: scan.SeverityHigh}
		b := a
		b.File = "other/package-lock.json"
		fixes := GroupAll([]scan.Finding{a, b})
		if len(fixes) != 2 || fixes[0].Key() == fixes[1].Key() {
			t.Errorf("two vulnerabilities with no package share a key: %+v", viewsOf(fixes))
		}
	})
	t.Run("a CVE with no package and a package named like its ID, two keys", func(t *testing.T) {
		bare := scan.Finding{ID: "CVE-9", Source: scan.SourceTrivy, Ecosystem: "npm", Version: "1.0", Severity: scan.SeverityHigh}
		named := vuln("CVE-1", "CVE-9", "npm", scan.ClassLangPackages, "1.0", "", scan.SeverityHigh)
		fixes := GroupAll([]scan.Finding{bare, named})
		if len(fixes) != 2 || fixes[0].Key() == fixes[1].Key() {
			t.Errorf("a bare vulnerability and a package named like it share a key: %+v", viewsOf(fixes))
		}
	})
	t.Run("the key of a bare row does not move when an unrelated CVE joins", func(t *testing.T) {
		bare := scan.Finding{ID: "CVE-9", Source: scan.SourceTrivy, Ecosystem: "npm", Version: "1.0", Severity: scan.SeverityHigh}
		other := scan.Finding{ID: "CVE-8", Source: scan.SourceTrivy, Ecosystem: "npm", Version: "1.0", Severity: scan.SeverityHigh}
		find := func(findings ...scan.Finding) FixKey {
			for _, f := range GroupAll(findings) {
				if f.Package == "CVE-9" {
					return f.Key()
				}
			}
			t.Fatal("no CVE-9 row")
			return FixKey{}
		}
		if before, after := find(bare), find(other, bare); before != after {
			t.Errorf("key changed from %+v to %+v", before, after)
		}
	})
}

// invariantSets feed the invariant test and seed the fuzz target.
func invariantSets() map[string][]scan.Finding {
	os, lang := scan.ClassOSPackages, scan.ClassLangPackages
	return map[string][]scan.Finding{
		"empty": nil,
		"mixed classes": {
			vuln("CVE-1", "openssl", "alpine", os, "3.1.0", "3.1.4", scan.SeverityHigh),
			vuln("CVE-2", "openssl", "alpine", os, "3.1.0", "3.1.5", scan.SeverityLow),
			vuln("CVE-3", "lodash", "npm", lang, "4.0.0", "4.17.21", scan.SeverityCritical),
			vuln("CVE-4", "old", "", "", "1.0.0", "1.0.1", scan.SeverityHigh),
			vuln("CVE-5", "nofix", "npm", lang, "1.0.0", "", scan.SeverityHigh),
			vuln("CVE-6", "nofix", "npm", lang, "1.0.0", "", scan.SeverityLow),
			{ID: "S1", Source: scan.SourceGitleaks},
			{ID: "L1", Source: scan.SourceTrivyLicense, PkgName: "x", FixedIn: "9"},
			{ID: "M1", Source: scan.SourceTrivyMisconfig},
		},
		"no package": {
			{ID: "CVE-A", Source: scan.SourceTrivy, Severity: scan.SeverityHigh, FixedIn: "1.0.1"},
			{ID: "CVE-B", Source: scan.SourceTrivy, Severity: scan.SeverityHigh},
		},
		"two installed versions": {
			vuln("CVE-1", "semver", "npm", lang, "5.0.0", "5.7.2", scan.SeverityHigh),
			vuln("CVE-1", "semver", "npm", lang, "6.0.0", "6.3.1", scan.SeverityHigh),
			vuln("CVE-2", "semver", "npm", lang, "6.0.0", "", scan.SeverityHigh),
		},
		"ambiguous": {
			vuln("CVE-1", "libfoo", "npm", lang, "1.0.0", "abc, def", scan.SeverityHigh),
			vuln("CVE-2", "libfoo", "npm", lang, "1.0.0", "1.0.1", scan.SeverityHigh),
		},
	}
}

// assertInvariant is AC5: what the header calls Fixable is exactly the CVEs of
// the rows that are not Unfixed, so a grouped list never contradicts it, and a
// row with no published fix is never counted in it.
func assertInvariant(t *testing.T, findings []scan.Finding) {
	t.Helper()
	fixes := GroupAll(findings)
	sum := Summarize(findings)

	var fixable, unfixed, members int
	for _, f := range fixes {
		if len(f.CVEs) != len(f.Members) {
			t.Errorf("%s: %d CVEs for %d Members", f.Package, len(f.CVEs), len(f.Members))
		}
		members += len(f.Members)
		if f.Unfixed {
			unfixed += len(f.CVEs)
			if f.Target != "" || f.Command != "" || len(f.Options) > 0 {
				t.Errorf("%s: an Unfixed row names a target %q / command %q / options %v", f.Package, f.Target, f.Command, f.Options)
			}
			continue
		}
		fixable += len(f.CVEs)
	}
	if fixable != sum.Fixable() {
		t.Errorf("CVEs of fixable rows = %d, Summarize().Fixable() = %d", fixable, sum.Fixable())
	}
	if unfixed != sum.Unfixed {
		t.Errorf("CVEs of unfixed rows = %d, Summarize().Unfixed = %d", unfixed, sum.Unfixed)
	}
	if members != sum.Fixable()+sum.Unfixed {
		t.Errorf("rows hold %d findings, the scan has %d vulnerabilities: one was dropped or counted twice", members, sum.Fixable()+sum.Unfixed)
	}
}

func TestGroupAllFixableCountIsSummarizeFixable(t *testing.T) {
	for name, findings := range invariantSets() {
		t.Run(name, func(t *testing.T) { assertInvariant(t, findings) })
	}
}

// FuzzGroupAllInvariant builds a scan from bytes and checks the invariant; the
// seeds run with `go test`, `go test -fuzz` explores.
func FuzzGroupAllInvariant(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11})
	f.Add([]byte{255, 128, 64, 32, 16, 8, 4, 2, 1, 0})
	f.Add([]byte{3, 3, 3, 3})
	f.Fuzz(func(t *testing.T, data []byte) {
		packages := []string{"", "libfoo", "libbar"}
		versions := []string{"", "1.0.0", "2.0.0"}
		fixed := []string{"", "1.0.1", "1.2.0", "abc, def", "weird"}
		classes := []string{"", scan.ClassOSPackages, scan.ClassLangPackages}
		sources := []string{scan.SourceTrivy, scan.SourceTrivy, scan.SourceTrivy, scan.SourceGitleaks, scan.SourceTrivyLicense}
		severities := []scan.SeverityLevel{scan.SeverityCritical, scan.SeverityHigh, scan.SeverityMedium, scan.SeverityLow, scan.SeverityUnknown}
		var findings []scan.Finding
		for i, b := range data {
			findings = append(findings, scan.Finding{
				ID:        "CVE-" + string(rune('A'+i%26)),
				Source:    sources[int(b)%len(sources)],
				Severity:  severities[int(b>>3)%len(severities)],
				PkgName:   packages[int(b>>1)%len(packages)],
				Version:   versions[int(b>>2)%len(versions)],
				FixedIn:   fixed[int(b>>4)%len(fixed)],
				Class:     classes[int(b>>5)%len(classes)],
				Ecosystem: []string{"npm", "alpine"}[int(b>>6)%2],
			})
		}
		assertInvariant(t, findings)
	})
}

// DEVDESK-754, review: "2.0b" is a PEP 440 pre-release, older than "2.0", so a
// pip package whose CVEs need both has no single target; an OpenSSL-style letter
// still orders.
func TestGroupAllDoesNotOrderALoneLetterOfAPythonPackage(t *testing.T) {
	lang := scan.ClassLangPackages
	pip := GroupAll([]scan.Finding{
		vuln("CVE-1", "requests", "pip", lang, "1.0", "2.0", scan.SeverityHigh),
		vuln("CVE-2", "requests", "pip", lang, "1.0", "2.0b", scan.SeverityHigh),
	})
	if len(pip) != 1 || pip[0].Target != "" || pip[0].Command != "" {
		t.Errorf("pip row = %+v, want no target and no command", viewsOf(pip))
	}
	alpine := GroupAll([]scan.Finding{
		vuln("CVE-1", "openssl", "alpine", scan.ClassOSPackages, "1.1.1k", "1.1.1l", scan.SeverityHigh),
		vuln("CVE-2", "openssl", "alpine", scan.ClassOSPackages, "1.1.1k", "1.1.1w", scan.SeverityHigh),
	})
	if len(alpine) != 1 || alpine[0].Target != "1.1.1w" {
		t.Errorf("alpine row = %+v, want the target 1.1.1w", viewsOf(alpine))
	}
}
