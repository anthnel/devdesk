package remediation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/anthnel/devdesk/internal/scan"
)

const (
	pssBase       = "https://kubernetes.io/docs/concepts/security/pod-security-standards/"
	pssBaselineLn = pssBase + "#baseline"
	pssRestrictLn = pssBase + "#restricted"
)

// trivyFixture is the part of the recorded Trivy output these tests read.
type trivyFixture struct {
	Results []struct {
		Target            string
		Type              string
		Misconfigurations []struct {
			ID         string
			Status     string
			References []string
		}
	}
}

func loadPSSFixture(t *testing.T) trivyFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "pss", "trivy-0.76.2.json"))
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	var fx trivyFixture
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return fx
}

// anchorOf reads the profile out of a reference list the way Trivy writes it,
// independently of the production helper, so the table is checked against the
// raw output and not against our own reading of it.
func anchorOf(refs []string) string {
	for _, r := range refs {
		if strings.HasPrefix(r, pssBase+"#") {
			return strings.TrimPrefix(r, pssBase+"#")
		}
	}
	return ""
}

func pssFinding(id string, refs ...string) scan.Finding {
	return scan.Finding{ID: id, Source: scan.SourceTrivyMisconfig, References: refs}
}

// DEVDESK-686 (S-01, criterion 2) — the table is written by hand, so it can
// drift from Trivy's checks in both directions: a rule Trivy anchors that the
// table lacks (the label would silently be missing), and a table entry Trivy
// no longer anchors (the label would claim something Trivy does not). The
// recorded output is the reference.
func TestThePodSecurityTableMatchesTrivy(t *testing.T) {
	fx := loadPSSFixture(t)

	anchored := map[string]string{} // RuleKey -> anchor, from Trivy
	unanchored := map[string]bool{} // PSS link without an anchor
	for _, r := range fx.Results {
		for _, m := range r.Misconfigurations {
			if m.Status != "FAIL" {
				continue
			}
			key := RuleKey(m.ID)
			if a := anchorOf(m.References); a != "" {
				if prev, ok := anchored[key]; ok && prev != a {
					t.Errorf("%s is anchored both #%s and #%s in the fixture", m.ID, prev, a)
				}
				anchored[key] = a
				continue
			}
			for _, ref := range m.References {
				if strings.HasPrefix(ref, pssBase) {
					unanchored[key] = true
				}
			}
		}
	}
	if len(anchored) != 20 {
		t.Fatalf("the fixture anchors %d rules, want 20 (13 baseline, 7 restricted): record it again", len(anchored))
	}

	// Trivy -> table.
	for key, anchor := range anchored {
		got, ok := pssProfiles[key]
		if !ok {
			t.Errorf("%s is anchored #%s by Trivy and missing from the table", key, anchor)
			continue
		}
		if string(got) != anchor {
			t.Errorf("%s: table says %q, Trivy anchors #%s", key, got, anchor)
		}
	}
	// Table -> Trivy.
	for key, profile := range pssProfiles {
		anchor, ok := anchored[key]
		if !ok {
			t.Errorf("%s is in the table as %q and Trivy no longer anchors it (or the fixture lacks it)", key, profile)
			continue
		}
		if string(profile) != anchor {
			t.Errorf("%s: table says %q, Trivy anchors #%s", key, profile, anchor)
		}
	}
	// A PSS link without an anchor names no profile: never in the table.
	for _, key := range []string{"KSV117", "KSV119"} {
		if !unanchored[key] {
			t.Errorf("%s should carry a PSS link without an anchor in the fixture", key)
		}
		if _, ok := pssProfiles[key]; ok {
			t.Errorf("%s has no anchor and must not be in the table", key)
		}
	}
	if len(pssProfiles) != 20 {
		t.Errorf("the table has %d rules, want 20", len(pssProfiles))
	}
}

// Every rule of the recorded output, as the application sees it, resolves to
// exactly the anchor Trivy wrote, from the table and not from the fallback.
func TestPodSecurityResolvesEveryRecordedFinding(t *testing.T) {
	fx := loadPSSFixture(t)
	seen := map[string]bool{}
	var bad []string
	for _, r := range fx.Results {
		for _, m := range r.Misconfigurations {
			if m.Status != "FAIL" {
				continue
			}
			f := scan.Finding{ID: m.ID, Source: scan.SourceTrivyMisconfig, References: m.References}
			got, ok := PodSecurity(f)
			want := anchorOf(m.References)
			if want == "" {
				if ok {
					bad = append(bad, fmt.Sprintf("%s (%s): got %+v, want not Pod Security", m.ID, r.Target, got))
				}
				continue
			}
			seen[m.ID] = true
			if !ok || string(got.Profile) != want || got.FromTrivy {
				bad = append(bad, fmt.Sprintf("%s (%s): got %+v ok=%v, want {%s false}", m.ID, r.Target, got, ok, want))
			}
		}
	}
	if len(bad) > 0 {
		t.Errorf("%d recorded findings resolve wrongly, first: %s", len(bad), bad[0])
	}
	if len(seen) != 20 {
		ids := make([]string, 0, len(seen))
		for id := range seen {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		t.Errorf("resolved %d distinct anchored rules, want 20: %v", len(seen), ids)
	}
}

func TestPodSecurity(t *testing.T) {
	unconfined := pssFinding("KSV-0104", pssBaselineLn)
	unconfined.SeccompOrigin = "unconfined"
	absent := pssFinding("KSV-0104", pssBaselineLn)
	absent.SeccompOrigin = "absent"
	unknown := pssFinding("KSV-0104", pssBaselineLn)
	kubeconform := pssFinding("KSV-0017", pssBaselineLn)
	kubeconform.Source = scan.SourceKubeconform
	vuln := pssFinding("CVE-2024-1", pssBaselineLn)
	vuln.Source = "trivy"
	d22 := pssFinding("KSV-0022", pssBaselineLn)
	d22.SeccompOrigin = "absent" // only KSV-0104 reads the origin
	avdSpelling := pssFinding("AVD-KSV-0017")
	shortSpelling := pssFinding("KSV017")
	conflict := pssFinding("KSV-0017", pssRestrictLn) // the table wins over Trivy's anchor

	tests := []struct {
		name   string
		f      scan.Finding
		want   PSS
		wantOK bool
	}{
		{"KSV-0017 is baseline", pssFinding("KSV-0017"), PSS{Profile: ProfileBaseline}, true},
		{"KSV-0030 is restricted", pssFinding("KSV-0030"), PSS{Profile: ProfileRestricted}, true},
		{"KSV-0003 is not Pod Security", pssFinding("KSV-0003"), PSS{}, false},
		{"AVD-KSV-0017 spelling", avdSpelling, PSS{Profile: ProfileBaseline}, true},
		{"KSV017 spelling", shortSpelling, PSS{Profile: ProfileBaseline}, true},
		{"table wins over the anchor", conflict, PSS{Profile: ProfileBaseline}, true},
		{"KSV-0104 unconfined is baseline", unconfined, PSS{Profile: ProfileBaseline}, true},
		{"KSV-0104 absent is restricted", absent, PSS{Profile: ProfileRestricted}, true},
		{"KSV-0104 unknown origin is baseline", unknown, PSS{Profile: ProfileBaseline}, true},
		{"KSV-0022 stays baseline", d22, PSS{Profile: ProfileBaseline}, true},
		{"unknown rule, restricted anchor", pssFinding("KSV-9999", "https://avd.aquasec.com/misconfig/ksv-9999", pssRestrictLn), PSS{Profile: ProfileRestricted, FromTrivy: true}, true},
		{"unknown rule, baseline anchor", pssFinding("KSV-9998", pssBaselineLn), PSS{Profile: ProfileBaseline, FromTrivy: true}, true},
		{"unknown rule, unknown anchor", pssFinding("KSV-9997", pssBase+"#privileged"), PSS{}, false},
		{"unknown rule, no link", pssFinding("KSV-9996", "https://avd.aquasec.com/misconfig/ksv-9996"), PSS{}, false},
		{"KSV-0117 link without anchor", pssFinding("KSV-0117", pssBase), PSS{}, false},
		{"KSV-0119 link without anchor", pssFinding("KSV-0119", pssBase), PSS{}, false},
		{"a kubeconform finding", kubeconform, PSS{}, false},
		{"a vulnerability", vuln, PSS{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := PodSecurity(tt.f)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("PodSecurity = %+v, %v; want %+v, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestPSSAnchor(t *testing.T) {
	tests := []struct {
		name   string
		refs   []string
		want   Profile
		wantOK bool
	}{
		{"baseline", []string{"https://avd.aquasec.com/misconfig/ksv-0017", pssBaselineLn}, ProfileBaseline, true},
		{"restricted", []string{pssRestrictLn, "https://avd.aquasec.com/misconfig/ksv-0001"}, ProfileRestricted, true},
		{"no anchor", []string{pssBase}, "", false},
		{"unknown anchor", []string{pssBase + "#privileged"}, "", false},
		{"anchor on another site", []string{"https://example.com/#restricted"}, "", false},
		{"empty", nil, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := pssAnchor(tt.refs)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("pssAnchor = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// Restricted requires everything baseline does, so a restricted target covers
// both; a baseline target covers baseline only.
func TestProfileCovers(t *testing.T) {
	tests := []struct {
		target, p Profile
		want      bool
	}{
		{ProfileBaseline, ProfileBaseline, true},
		{ProfileBaseline, ProfileRestricted, false},
		{ProfileRestricted, ProfileBaseline, true},
		{ProfileRestricted, ProfileRestricted, true},
	}
	for _, tt := range tests {
		if got := tt.target.Covers(tt.p); got != tt.want {
			t.Errorf("%s.Covers(%s) = %v, want %v", tt.target, tt.p, got, tt.want)
		}
	}
}
