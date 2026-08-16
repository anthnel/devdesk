package dashboard

import (
	"strings"
	"testing"
)

// Le nœud `secrets` des deux arbres de Health. Il compte des **cibles** et non
// des secrets : deux dépôts sont deux décisions, quarante fuites dans le même
// n'en font qu'une, et c'est :sec qui détaille.

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

// Le cas que le booléen ne savait pas dire. `scan.enable_secret` coupée, un
// outil absent, ou des entrées écrites avant que le scan d'image ait une étape
// secrets : dans les trois cas le cache ne porte aucun verdict, et `0` se
// lirait « aucune cible n'en porte » de cibles que personne n'a regardées.
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
		if !strings.Contains(got, unknownValue) {
			t.Errorf("the %s node reads %q with no verdict known, want %q", tt.label, got, unknownValue)
		}
		if strings.Contains(got, "0") {
			t.Errorf("the %s node reads %q — a zero says nobody carries a secret, of targets nobody looked at",
				tt.label, got)
		}
	}
}

// Un verdict connu sur une partie seulement suffit à afficher le compte : il est
// alors un plancher, et un plancher non nul se décide. C'est ce qui distingue
// « on ne sait rien » de « on sait déjà qu'il y en a un ».
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

// Une cible sans verdict n'entre dans aucun des deux décomptes : ni porteuse,
// ni innocentée.
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

// Total() sert les paliers trop étroits pour deux arbres ; il doit sommer les
// deux moitiés du verdict comme il somme les CRITICAL.
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
