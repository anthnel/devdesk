package dashboard

import (
	"strings"
	"testing"
)

// The `secrets` node of Health's two trees. It counts **targets** and not
// secrets: two repositories are two decisions, forty leaks in the same one
// are only one, and it's :sec that breaks it down.

func secretsFound() *bool { v := true; return &v }
func secretsClean() *bool { v := false; return &v }

// postureWith builds a posture whose two families carry the given verdicts.
func postureWith(images, repositories []*bool) posture {
	p := posture{Read: true}
	for _, verdict := range images {
		p.Images.add(0, verdict, day(2))
	}
	for _, verdict := range repositories {
		p.Repositories.add(0, verdict, day(2))
	}
	return p
}

func TestTheHealthBoxCountsTheTargetsThatCarryASecret(t *testing.T) {
	m, _ := loadedModel(t)
	m = feed(t, m, PostureMsg{Posture: postureWith(
		[]*bool{secretsFound(), secretsClean(), secretsClean()},
		[]*bool{secretsFound(), secretsFound(), secretsClean()},
	)})

	repos, images := healthColumns(m)

	if got := nodeUnder(images, "Images", "secrets"); !strings.Contains(got, "1") {
		t.Errorf("the images node reads %q, want the one image that carries a secret", got)
	}
	if got := nodeUnder(repos, "Repositories", "secrets"); !strings.Contains(got, "2") {
		t.Errorf("the repositories node reads %q, want the two repositories", got)
	}
}

// The case the boolean couldn't say. `scan.enable_secret` turned off, a
// missing tool, or entries written before the image scan had a secrets
// step: in all three cases the cache carries no verdict, and `0` would read
// "no target carries any" for targets nobody has looked at.
func TestAnInventoryWithNoVerdictAtAllPrintsNoCount(t *testing.T) {
	m, _ := loadedModel(t)
	m = feed(t, m, PostureMsg{Posture: postureWith(
		[]*bool{nil, nil},
		[]*bool{nil},
	)})

	repos, images := healthColumns(m)

	for _, tt := range []struct {
		side  []string
		head  string
		label string
	}{
		{images, "Images", "images"},
		{repos, "Repositories", "repositories"},
	} {
		got := nodeUnder(tt.side, tt.head, "secrets")
		if !strings.Contains(got, unknownValue()) {
			t.Errorf("the %s node reads %q with no verdict known, want %q", tt.label, got, unknownValue())
		}
		if strings.Contains(got, "0") {
			t.Errorf("the %s node reads %q — a zero says nobody carries a secret, of targets nobody looked at",
				tt.label, got)
		}
	}
}

// A verdict known on only part of the set is enough to display the count:
// it's then a floor, and a nonzero floor is actionable. That's what tells
// apart "we know nothing" from "we already know there is at least one".
func TestAPartiallyKnownInventoryStillReportsWhatItKnows(t *testing.T) {
	m, _ := loadedModel(t)
	m = feed(t, m, PostureMsg{Posture: postureWith(
		[]*bool{secretsFound(), nil, nil},
		nil,
	)})

	_, images := healthColumns(m)

	got := nodeUnder(images, "Images", "secrets")
	if !strings.Contains(got, "1") {
		t.Errorf("the images node reads %q, want the one verdict that is known", got)
	}
}

// A target with no verdict enters neither count: neither carrying, nor
// cleared.
func TestATargetWithoutAVerdictCountsNeitherWay(t *testing.T) {
	var side postureSide
	side.add(0, secretsFound(), day(1))
	side.add(0, nil, day(1))
	side.add(0, secretsClean(), day(1))

	if side.Targets != 3 {
		t.Errorf("Targets = %d, want the three scanned targets", side.Targets)
	}
	if side.Secrets != 1 {
		t.Errorf("Secrets = %d, want the one carrier", side.Secrets)
	}
	if side.SecretsKnown != 2 {
		t.Errorf("SecretsKnown = %d, want the two targets a stage actually looked at", side.SecretsKnown)
	}
}

// Total() serves the tiers too narrow for two trees; it must sum both
// halves of the verdict just as it sums the CRITICALs.
func TestTheTotalFoldsBothFamiliesVerdicts(t *testing.T) {
	p := postureWith([]*bool{secretsFound(), nil}, []*bool{secretsFound(), secretsClean()})

	total := p.Total()

	if total.Secrets != 2 {
		t.Errorf("Total().Secrets = %d, want both families' carriers", total.Secrets)
	}
	if total.SecretsKnown != 3 {
		t.Errorf("Total().SecretsKnown = %d, want the three targets with a verdict", total.SecretsKnown)
	}
}
