package scan

import (
	"slices"
	"testing"
)

const (
	testPSSBaselineURL = "https://kubernetes.io/docs/concepts/security/pod-security-standards/#baseline"
	testAVDKSV17URL    = "https://avd.aquasec.com/misconfig/ksv-0017"
)

func parseOneMisconfig(t *testing.T, misconf string) Finding {
	t.Helper()
	findings, err := parseTrivyOutput([]byte(`{"Results":[{"Target":"deploy/app.yaml","Class":"config","Type":"kubernetes","Misconfigurations":[` + misconf + `]}]}`))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	return findings[0]
}

// DEVDESK-686 (S-01, criteria 2 and 7) — Trivy puts the Pod Security link, with
// its #baseline or #restricted anchor, in References and keeps only the rule's
// own page in PrimaryURL. The parser used to read PrimaryURL alone, which left
// the profile of every rule out of reach of the label and the filter.
func TestTrivyMisconfigKeepsThePodSecurityReference(t *testing.T) {
	f := parseOneMisconfig(t, `{"ID":"KSV-0017","Title":"Privileged","Severity":"HIGH","Status":"FAIL",
	  "PrimaryURL":"`+testAVDKSV17URL+`",
	  "References":["`+testPSSBaselineURL+`","`+testAVDKSV17URL+`"],
	  "CauseMetadata":{"StartLine":20,"EndLine":24}}`)

	want := []string{testAVDKSV17URL, testPSSBaselineURL}
	if !slices.Equal(f.References, want) {
		t.Errorf("References = %v, want %v (PrimaryURL first, then the PSS link)", f.References, want)
	}
	// References[0] is what `o` opens in the details: it must not change.
	if len(f.References) > 0 && f.References[0] != testAVDKSV17URL {
		t.Errorf("References[0] = %q, want PrimaryURL %q", f.References[0], testAVDKSV17URL)
	}
}

func TestTrivyMisconfigReferencesAreDeduplicated(t *testing.T) {
	tests := []struct {
		name string
		json string
		want []string
	}{
		{
			name: "primary url repeated in references",
			json: `{"ID":"KSV-0017","PrimaryURL":"` + testAVDKSV17URL + `","References":["` + testAVDKSV17URL + `","` + testPSSBaselineURL + `","` + testAVDKSV17URL + `"]}`,
			want: []string{testAVDKSV17URL, testPSSBaselineURL},
		},
		{
			name: "a reference listed twice",
			json: `{"ID":"KSV-0017","PrimaryURL":"` + testAVDKSV17URL + `","References":["` + testPSSBaselineURL + `","` + testPSSBaselineURL + `"]}`,
			want: []string{testAVDKSV17URL, testPSSBaselineURL},
		},
		{
			name: "no primary url keeps the references in order",
			json: `{"ID":"KSV-0017","References":["` + testPSSBaselineURL + `","` + testAVDKSV17URL + `"]}`,
			want: []string{testPSSBaselineURL, testAVDKSV17URL},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseOneMisconfig(t, tt.json).References; !slices.Equal(got, tt.want) {
				t.Errorf("References = %v, want %v", got, tt.want)
			}
		})
	}
}

// A report from before this change, or a rule without a References list,
// behaves exactly as it did: PrimaryURL alone.
func TestTrivyMisconfigWithoutReferencesKeepsPrimaryURL(t *testing.T) {
	f := parseOneMisconfig(t, `{"ID":"DS002","Severity":"HIGH","PrimaryURL":"https://avd.aquasec.com/misconfig/ds002"}`)
	if want := []string{"https://avd.aquasec.com/misconfig/ds002"}; !slices.Equal(f.References, want) {
		t.Errorf("References = %v, want %v", f.References, want)
	}

	none := parseOneMisconfig(t, `{"ID":"DS002","Severity":"HIGH"}`)
	if len(none.References) != 0 {
		t.Errorf("References = %v, want none", none.References)
	}
}
