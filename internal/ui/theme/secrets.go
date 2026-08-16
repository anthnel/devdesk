package theme

import "github.com/charmbracelet/lipgloss"

// SecretsState is what a scan can say about secrets, and it has three values
// rather than two. « Rien trouvé » et « personne n'a cherché » se ressemblent
// beaucoup et ne veulent pas dire du tout la même chose : une étape secrets peut
// être coupée par l'option, par un outil absent, ou avoir échoué, et un scan
// d'image n'en avait aucune avant §3.11. Un booléen rendrait l'icône verte dans
// tous ces cas.
//
// Le rendu est ici parce que trois vues l'affichent — workspaces, oci/images et
// l'inventaire de sécurité — et qu'il n'y a qu'une iconographie. La vue
// workspaces décidait sa couleur en comparant la chaîne d'icône déjà rendue, ce
// qu'un renommage d'icône aurait cassé en silence.
type SecretsState int

const (
	SecretsUnknown SecretsState = iota
	SecretsClean
	SecretsFound
)

// SecretsVerdict maps a cached verdict onto the three states.
//
// `scanned` est l'état de la cible elle-même : une cible jamais scannée n'a pas
// de verdict, quel que soit ce que porte l'entrée de cache — il n'y en a pas.
func SecretsVerdict(sensitive *bool, scanned bool) SecretsState {
	if !scanned || sensitive == nil {
		return SecretsUnknown
	}
	if *sensitive {
		return SecretsFound
	}
	return SecretsClean
}

// SecretsIcon is what the cell prints. Texte brut, sans séquence ANSI : c'est
// SecretsStyle qui colore, et l'inverse serait la Rule 122.
func SecretsIcon(state SecretsState) string {
	switch state {
	case SecretsFound:
		return IconWorkspaceUntrusted
	case SecretsClean:
		return IconWorkspaceTrusted
	default:
		return IconWorkspaceUnknown
	}
}

// SecretsStyle colours the verdict. C'est la seule colonne de ces tables qui
// rapporte un constat plutôt qu'un décompte, et un dépôt qui porte un secret est
// ce qui mérite d'être vu avant les compteurs.
func SecretsStyle(state SecretsState) lipgloss.Style {
	switch state {
	case SecretsFound:
		return StatusErrorStyle
	case SecretsClean:
		return StatusOKStyle
	default:
		return DimStyle
	}
}
