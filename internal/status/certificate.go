package status

// CertState is what a monitored certificate is, as opposed to what a service
// is. Un service est joignable ou non ; un certificat a une date, donc il a un
// état de plus — celui où il est encore valide et demande déjà une action.
//
// C'est pour ça que les certificats ne peuvent pas emprunter le vocabulaire
// des moniteurs. `up` / `down` / `error` collait ensemble trois faits qui ne
// se règlent pas de la même façon : un certificat périmé (le service est déjà
// cassé), un certificat à renouveler (il ne l'est pas encore, et c'est le seul
// moment où l'on peut agir), et un certificat qu'on n'a pas pu lire (on ne
// sait rien). Le troisième est le seul qui soit vraiment une erreur.
type CertState string

const (
	// CertValid — lu, et loin de son échéance.
	CertValid CertState = "valid"
	// CertToRenew — lu, encore valide, et dans la fenêtre de renouvellement.
	CertToRenew CertState = "to renew"
	// CertExpired — lu, et sa date est passée.
	CertExpired CertState = "expired"
	// CertError — pas lu du tout : hôte injoignable, poignée de main refusée,
	// chaîne vide, cible non configurée. C'est une absence de mesure, et non
	// un verdict sur le certificat.
	CertError CertState = "error"
)

// CertRenewWindowDays is where a certificate stops being a date and becomes a
// task. Il vaut la fenêtre `WARNING` de SSLChecker : les deux répondent à la
// même question, et deux seuils qui divergent feraient dire `to renew` au
// dashboard de ce que `:status` affiche encore en vert.
const CertRenewWindowDays = 30

// CertStateOf classifies one monitored certificate.
//
// Elle se décide sur `SSLDaysLeft` et non sur `Status`, parce que `StatusType`
// n'a pas quatre valeurs à donner : SSLChecker rend `ERROR` aussi bien pour un
// certificat périmé que pour un qui expire dans six jours, et `DOWN` pour un
// hôte injoignable. Le nombre de jours, lui, distingue les trois — et son
// absence est exactement le cas où rien n'a pu être lu.
func CertStateOf(c ComponentStatus) CertState {
	if c.SSLDaysLeft == nil {
		return CertError
	}
	switch days := *c.SSLDaysLeft; {
	case days < 0:
		return CertExpired
	case days <= CertRenewWindowDays:
		return CertToRenew
	default:
		return CertValid
	}
}

// CertCounts tallies a set of monitored certificates, one figure per state.
func CertCounts(certs []ComponentStatus) (valid, toRenew, expired, errored int) {
	for _, c := range certs {
		switch CertStateOf(c) {
		case CertValid:
			valid++
		case CertToRenew:
			toRenew++
		case CertExpired:
			expired++
		case CertError:
			errored++
		}
	}
	return
}
