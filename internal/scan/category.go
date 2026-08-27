package scan

// Où va un finding : une seule définition, pour tout le monde.
//
// Il y en avait deux, qui ne disaient pas la même chose. `CountFindings`
// classait sur `Source` seul et envoyait tout le reste vers les compteurs de
// sévérité ; la vue security classait sur `Source` plus `PkgName` plus `Match`.
// Trois entrées les séparaient — un finding trivy sans `PkgName`, une source
// inconnue, un finding trivy portant un `Match` — et chacune produisait un
// finding compté dans la barre CVE du header mais absent de tout onglet, donc
// invisible dans la table. Même famille que D24, D25 et D26 : deux copies d'une
// règle, une seule mise à jour.
//
// La classification se fait maintenant sur la source et rien d'autre, ce qui
// demandait que les secrets Trivy en aient une à eux (`trivy-secret`) au lieu
// d'être reconnus à la présence d'un `Match`.

// Les sources qu'un scanner peut produire. Une source absente de cette liste
// est un défaut de programmation, pas une entrée utilisateur : `Categorize`
// la classe en vulnérabilité, ce qui la laisse visible dans l'onglet CVE plutôt
// que de la faire disparaître.
const (
	SourceTrivy          = "trivy"           // vulnérabilités
	SourceTrivySecret    = "trivy-secret"    // secrets détectés par Trivy
	SourceTrivyLicense   = "trivy-license"   // licences
	SourceTrivyMisconfig = "trivy-misconfig" // misconfigurations IaC
	SourceGitleaks       = "gitleaks"        // secrets détectés par Gitleaks
	SourcePlumber        = "plumber"         // score de sécurité de pipeline (§3.42)
)

// Category est la famille à laquelle un finding appartient : un onglet de la
// vue des résultats, et un compteur de `Result`.
type Category int

const (
	CategoryVulnerability Category = iota
	CategorySecret
	CategoryLicense
	CategoryMisconfiguration
	CategoryCIScore
)

// Categorize retourne la famille d'un finding.
//
// C'est la seule fonction qui décide, et `Result.CountFindings` comme les
// onglets de la vue security passent par elle.
func Categorize(f Finding) Category {
	switch f.Source {
	case SourceGitleaks, SourceTrivySecret:
		return CategorySecret
	case SourceTrivyLicense:
		return CategoryLicense
	case SourceTrivyMisconfig:
		return CategoryMisconfiguration
	case SourcePlumber:
		return CategoryCIScore
	default:
		return CategoryVulnerability
	}
}
