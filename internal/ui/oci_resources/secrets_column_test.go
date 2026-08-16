package ociresources

import (
	"testing"

	"github.com/anthnel/devdesk/internal/cache"
	"github.com/anthnel/devdesk/internal/ui/theme"
)

// La colonne Secrets de l'onglet Images. Trivy lit les couches d'une image, ce
// que Gitleaks ne sait pas faire : c'est ce qui donne un verdict à une image, et
// il n'était affiché nulle part.

// Les trois états sont distincts à l'écran. Deux d'entre eux se ressemblent
// dangereusement — « regardée, rien trouvé » et « personne n'a regardé » — et
// c'est précisément la paire qu'un booléen confondait.
func TestTheSecretsColumnTellsTheThreeVerdictsApart(t *testing.T) {
	m := loadedModel(t)

	cols := m.imageTable.Table().Columns()
	name := columnIndex(t, cols, "Name")
	secrets := columnIndex(t, cols, "Secrets")

	byName := map[string]string{}
	for _, row := range m.imageTable.Table().Rows() {
		byName[row[name]] = row[secrets]
	}

	tests := []struct {
		image string
		want  theme.SecretsState
		why   string
	}{
		{"api:v1", theme.SecretsFound, "a secret was found"},
		{"cache:v2", theme.SecretsClean, "a stage looked and found nothing"},
		{"orphan", theme.SecretsUnknown, "scanned before there was a secret stage"},
		{"web:v3", theme.SecretsUnknown, "never scanned at all"},
	}

	for _, tt := range tests {
		got, ok := byName[tt.image]
		if !ok {
			t.Errorf("%s is missing from the table", tt.image)
			continue
		}
		if want := theme.SecretsIcon(tt.want); got != want {
			t.Errorf("%s: Secrets cell = %q, want %q — %s", tt.image, got, want, tt.why)
		}
	}
}

// Rule 122 : la cellule est mesurée avant d'être habillée, donc elle ne porte
// aucune séquence. La couleur du verdict passe par Style.
func TestTheSecretsCellIsPlainAndItsColourComesFromStyle(t *testing.T) {
	row := imageRow{Scanned: true, Entry: cache.ImageScanEntry{Sensitive: secretsFound()}}

	secrets := imageColumns()[columnIndexOfSecrets(t)]

	if cell := secrets.Cell(row); cell != theme.IconWorkspaceUntrusted {
		t.Errorf("Cell = %q, want the bare icon", cell)
	}
	if secrets.Style == nil {
		t.Fatal("the Secrets column declares no Style, so its verdict renders in the table's own colour")
	}
	if got, want := secrets.Style(row), theme.SecretsStyle(theme.SecretsFound); got.GetForeground() != want.GetForeground() {
		t.Errorf("Style foreground = %v, want the found verdict's %v", got.GetForeground(), want.GetForeground())
	}
}

// columnIndexOfSecrets finds the column in the freshly-built list, sans passer
// par une table : la position est ce qui bouge quand on en insère une.
func columnIndexOfSecrets(t *testing.T) int {
	t.Helper()
	for i, col := range imageColumns() {
		if col.Title == "Secrets" {
			return i
		}
	}
	t.Fatal("the Images tab has no Secrets column")
	return -1
}
