// Package metrics samples the machine DevDesk runs on. Il possède
// l'échantillonnage et le calcul des débits ; l'affichage appartient à la vue.
//
// Rien ici ne garde d'état : un débit se calcule entre deux relevés cumulatifs,
// et le relevé précédent **voyage avec le message** plutôt que d'être stocké
// dans le package. Un `Cmd` Bubble Tea ne doit modifier aucun état partagé
// (Rule 110), et deux échantillonnages qui se chevauchent sur un état de
// package produiraient un débit calculé contre le mauvais instant.
package metrics

import "time"

// HostSample is one reading of the host.
type HostSample struct {
	Taken time.Time

	CPUPercent float64
	// Cores is the logical core count, which a percentage needs to be read:
	// 40 % sur quatre cœurs et 40 % sur trente-deux ne décrivent pas la même
	// machine. Il ne change pas d'un échantillon à l'autre et n'est compté
	// qu'une fois.
	Cores      int
	MemPercent float64
	MemUsed    uint64
	MemTotal   uint64

	// NetRXPerSec and NetTXPerSec are bytes per second, and mean nothing unless
	// HasRate is true: net.IOCounters is cumulative, so the first reading after
	// a start has nothing to subtract from. La vue affiche `-`, pas `0`.
	NetRXPerSec float64
	NetTXPerSec float64
	HasRate     bool

	// OK is false when the host could not be read at all.
	OK bool
}

// Counters is the cumulative network reading a rate is measured against. Elle
// est rendue avec l'échantillon et repassée au suivant.
type Counters struct {
	RX    uint64
	TX    uint64
	At    time.Time
	Valid bool
}

// rate returns the per-second deltas between two cumulative readings.
//
// Il refuse trois situations plutôt que d'inventer un nombre :
//   - pas de relevé précédent — le premier échantillon n'a pas de débit ;
//   - un compteur qui recule — une interface réinitialisée, un rollover : la
//     soustraction donnerait un débit négatif, qui n'existe pas ;
//   - un intervalle nul ou négatif — une division par zéro, ou une horloge qui
//     a reculé.
//
// Dans les trois cas l'échantillon est *abandonné*, pas rendu à zéro : zéro est
// une mesure, et celle-ci n'a pas eu lieu.
func rate(prev, cur Counters) (rx, tx float64, ok bool) {
	if !prev.Valid || !cur.Valid {
		return 0, 0, false
	}
	if cur.RX < prev.RX || cur.TX < prev.TX {
		return 0, 0, false
	}
	elapsed := cur.At.Sub(prev.At).Seconds()
	if elapsed <= 0 {
		return 0, 0, false
	}
	return float64(cur.RX-prev.RX) / elapsed, float64(cur.TX-prev.TX) / elapsed, true
}

// DiskUsage is one filesystem's occupancy.
//
// `Used` est repris tel quel du système plutôt que calculé en `Total - Free` :
// sur ext4 les blocs réservés à root ne sont ni libres ni utilisés, donc les
// deux ne sont pas égaux, et un octet affiché à côté d'un pourcentage doit
// venir du même comptage que lui.
type DiskUsage struct {
	Path        string
	Free        uint64
	Used        uint64
	Total       uint64
	UsedPercent float64
	OK          bool
}

// TreeSize is how much disk one directory tree occupies — ce que les dépôts
// clonés coûtent, et non le remplissage du volume qui les porte. C'est celle
// des deux sur laquelle on peut agir.
type TreeSize struct {
	Path  string
	Bytes uint64
	// Partial is true when part of the tree could not be read. Un dossier
	// refusé fait sous-estimer le total, et un total sous-estimé sans mention
	// se lit comme une mesure.
	Partial bool
	OK      bool
}
