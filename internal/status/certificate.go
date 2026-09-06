package status

// CertState is what a monitored certificate is, as opposed to what a service
// is. A service is reachable or it is not; a certificate has a date, so it
// has one more state — the one where it is still valid and already demands
// action.
//
// That is why certificates cannot borrow the monitors' vocabulary.
// `up` / `down` / `error` lumped together three facts that are not handled
// the same way: an expired certificate (the service is already broken), a
// certificate due for renewal (it is not broken yet, and this is the only
// moment where action can be taken), and a certificate that could not be
// read (nothing is known). The third is the only one that is truly an
// error.
type CertState string

const (
	// CertValid — read, and far from its expiry.
	CertValid CertState = "valid"
	// CertToRenew — read, still valid, and inside the renewal window.
	CertToRenew CertState = "to renew"
	// CertExpired — read, and its date has passed.
	CertExpired CertState = "expired"
	// CertError — not read at all: unreachable host, refused handshake,
	// empty chain, target not configured. This is an absence of a
	// measurement, not a verdict on the certificate.
	CertError CertState = "error"
)

// CertRenewWindowDays is where a certificate stops being a date and becomes a
// task. It matches SSLChecker's `WARNING` window: the two answer the same
// question, and two diverging thresholds would make the dashboard say
// `to renew` about something `:status` still shows in green.
const CertRenewWindowDays = 30

// CertStateOf classifies one monitored certificate.
//
// It is decided from `SSLDaysLeft` and not from `Status`, because
// `StatusType` does not have four values to give: SSLChecker returns
// `ERROR` both for an expired certificate and for one expiring in six days,
// and `DOWN` for an unreachable host. The number of days, on the other
// hand, distinguishes the three — and its absence is exactly the case where
// nothing could be read.
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
