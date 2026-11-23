package remediation

import (
	"slices"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

// DEVDESK-753: the preview tests walk the catalog itself, so a rule added later
// without a fixture fails them. CatalogIDs is what they walk.
//
// The story's criteria speak of 22 rules; the catalog holds fewer today. Nothing
// here (or in the view's tests) pins a count: the list is read from the catalog.

func TestCatalogIDsListsEveryRuleOfTheCatalogOnce(t *testing.T) {
	ids := CatalogIDs()
	if len(ids) == 0 {
		t.Fatal("CatalogIDs is empty")
	}
	if len(ids) != len(catalog) {
		t.Errorf("CatalogIDs lists %d ids, the catalog holds %d rules", len(ids), len(catalog))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("%q is listed twice", id)
		}
		seen[id] = true
	}
}

func TestCatalogIDsAreSorted(t *testing.T) {
	ids := CatalogIDs()
	if len(ids) == 0 {
		t.Fatal("CatalogIDs is empty")
	}
	if !slices.IsSorted(ids) {
		t.Errorf("CatalogIDs is not sorted: %v", ids)
	}
}

// An id the list returns is one FixFor accepts: it is the AVD id of the rule, not
// a map key in another spelling.
func TestEveryCatalogIDFindsItsRule(t *testing.T) {
	if len(CatalogIDs()) == 0 {
		t.Fatal("CatalogIDs is empty")
	}
	for _, id := range CatalogIDs() {
		rule, ok := FixFor(scan.Finding{ID: id, Source: scan.SourceTrivyMisconfig})
		if !ok {
			// The two ids DevDesk emits itself come from another source.
			rule, ok = FixFor(scan.Finding{ID: id, Source: scan.SourceBuildContext})
		}
		if !ok {
			rule, ok = FixFor(scan.Finding{ID: id, Source: scan.SourceKubeconform})
		}
		if !ok {
			t.Errorf("FixFor finds no rule for %q", id)
			continue
		}
		if RuleKey(rule.AVDID) != RuleKey(id) {
			t.Errorf("%q found the rule %q", id, rule.AVDID)
		}
	}
}

// The list is a copy: a caller that sorts or edits it cannot reorder the next one.
func TestCatalogIDsReturnsACopy(t *testing.T) {
	first := CatalogIDs()
	if len(first) == 0 {
		t.Fatal("CatalogIDs is empty")
	}
	want := first[0]
	first[0] = "changed"
	if got := CatalogIDs()[0]; got != want {
		t.Errorf("the second call starts with %q, want %q", got, want)
	}
}
