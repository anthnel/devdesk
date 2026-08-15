package dashboard

import (
	"log"
	"time"

	"github.com/anthnel/devdesk/internal/cache"
)

// posture is what the scan caches say about the current context, without
// running anything: c'est ce qui relie le dashboard à l'inventaire de §3.11
// sans lancer un seul scan (Rule 126).
type posture struct {
	// Read tells "the caches were consulted" apart from "there is nothing in
	// them" — sans lui, un cache vide et un cache jamais lu affichent le même
	// zéro, et l'un des deux serait un mensonge.
	Read bool

	// Les deux caches sont gardés séparés parce qu'ils ne se corrigent pas de
	// la même façon : une CRITICAL dans une image se règle en changeant de tag,
	// dans un dépôt en changeant du code. Les sommer donnait un total sur
	// lequel on ne pouvait rien décider.
	Images       postureSide
	Repositories postureSide
}

// postureSide is one family's tally.
//
// Le décompte des HIGH est parti avec son affichage : la boîte montre désormais
// combien de cibles n'ont jamais été scannées, ce qui se décide (lancer un scan)
// là où un HIGH de plus ne se décidait pas. Un champ que plus personne ne lit se
// lit comme une donnée qu'on a oublié d'afficher.
type postureSide struct {
	Targets  int
	Critical int
	// Oldest is the age of the least recently scanned target: c'est la question
	// utile, parce qu'un compteur de CRITICAL vieux de trois semaines est un
	// compteur sur du code qui n'existe plus.
	Oldest time.Time
}

// Total folds both families, for the paliers too narrow to show them apart.
func (p posture) Total() postureSide {
	total := p.Images
	total.Targets += p.Repositories.Targets
	total.Critical += p.Repositories.Critical
	if o := p.Repositories.Oldest; !o.IsZero() && (total.Oldest.IsZero() || o.Before(total.Oldest)) {
		total.Oldest = o
	}
	return total
}

// readPosture sums both caches for one context. Un cache illisible n'est pas
// une posture vide : il rend Read=false, donc `-` et non `0`.
func readPosture(context string) posture {
	images, errImages := cache.NewImageScanCache(context)
	workspaces, errWorkspaces := cache.NewWorkspaceScanCache(context)
	if errImages != nil || errWorkspaces != nil {
		log.Printf("ERROR [dashboard] reading the scan caches: %v / %v", errImages, errWorkspaces)
		return posture{}
	}

	p := posture{Read: true}
	for _, entry := range images.GetAll() {
		p.Images.add(entry.Critical, entry.ScannedAt)
	}
	for _, entry := range workspaces.GetAll() {
		p.Repositories.add(entry.Critical, entry.ScannedAt)
	}
	return p
}

// add folds one scanned target into a family's tally.
func (p *postureSide) add(critical int, scannedAt time.Time) {
	p.Targets++
	p.Critical += critical

	// Une entrée sans horodatage ne rajeunit pas la posture : le zéro d'un
	// time.Time serait le plus ancien de tous et ferait lire « jamais scanné »
	// à un inventaire qui l'est.
	if scannedAt.IsZero() {
		return
	}
	if p.Oldest.IsZero() || scannedAt.Before(p.Oldest) {
		p.Oldest = scannedAt
	}
}

// ── Coverage ─────────────────────────────────────────────────────────────────
//
// Ce que les caches savent, c'est ce qui a été scanné. Combien il en reste
// demande l'autre moitié — l'inventaire — et celle-ci est déjà dans le modèle :
// `docker system df` compte les images, `fetchWorkspaceStats` les dépôts. La
// soustraction se fait donc ici, et non dans readPosture, qui ne lit que des
// fichiers de cache et n'a aucune raison d'aller interroger Docker.
//
// Les trois retournent `(n, mesuré)` plutôt qu'un entier : sans le second, un
// inventaire pas encore chargé rendrait un zéro, et « rien à scanner » est
// exactement le contraire de « on ne sait pas encore ».

// unscannedImages counts the local images that carry no cache entry.
func (m Model) unscannedImages() (int, bool) {
	if !m.posture.Read || m.loadingOCI || m.ociStats == nil || !m.ociStats.Available {
		return 0, false
	}
	return uncovered(m.ociStats.ImagesCount, m.posture.Images.Targets), true
}

// unscannedRepositories counts the workspaces that carry no cache entry.
func (m Model) unscannedRepositories() (int, bool) {
	if !m.posture.Read || m.loadingWorkspaces {
		return 0, false
	}
	return uncovered(m.workspaceCount, m.posture.Repositories.Targets), true
}

// unscannedTotal folds both, for the paliers too narrow to show them apart. Une
// seule moitié manquante suffit à rendre le total non mesuré : la somme d'un
// nombre et d'une inconnue est une inconnue.
func (m Model) unscannedTotal() (int, bool) {
	images, okImages := m.unscannedImages()
	repositories, okRepositories := m.unscannedRepositories()
	if !okImages || !okRepositories {
		return 0, false
	}
	return images + repositories, true
}

// uncovered is the inventory minus what has been scanned, floored at zero: le
// cache garde l'entrée d'une image supprimée depuis, donc la différence peut
// passer sous zéro — et « il en reste moins que zéro à scanner » n'est pas une
// phrase. Le plancher dit ce qu'il faut en retenir : plus rien à scanner.
func uncovered(inventory, scanned int) int {
	return max(inventory-scanned, 0)
}
