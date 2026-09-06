package status

import "testing"

func days(n int) *int { return &n }

// The four states exist because StatusType only has three to give, and it
// already puts two of them in the same bucket: SSLChecker returns `ERROR`
// both for an expired certificate and for one expiring in six days.
func TestCertStateOfSeparatesWhatStatusTypeCollapses(t *testing.T) {
	cases := []struct {
		name string
		comp ComponentStatus
		want CertState
	}{
		{"loin de l'échéance", ComponentStatus{Status: StatusOK, SSLDaysLeft: days(200)}, CertValid},
		{"juste hors fenêtre", ComponentStatus{Status: StatusOK, SSLDaysLeft: days(CertRenewWindowDays + 1)}, CertValid},
		{"dernier jour de la fenêtre", ComponentStatus{Status: StatusWarning, SSLDaysLeft: days(CertRenewWindowDays)}, CertToRenew},
		{"urgent mais valide", ComponentStatus{Status: StatusError, SSLDaysLeft: days(3)}, CertToRenew},
		{"expire aujourd'hui", ComponentStatus{Status: StatusError, SSLDaysLeft: days(0)}, CertToRenew},
		{"périmé", ComponentStatus{Status: StatusError, SSLDaysLeft: days(-1)}, CertExpired},
		{"hôte injoignable", ComponentStatus{Status: StatusDown}, CertError},
		{"cible non configurée", ComponentStatus{Status: StatusError}, CertError},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CertStateOf(c.comp); got != c.want {
				t.Errorf("CertStateOf() = %q, want %q", got, c.want)
			}
		})
	}
}

// An expired certificate and an unreadable certificate are two different
// facts, and it was counting them together that the Health box complained
// about.
func TestCertCountsKeepsExpiredApartFromUnreadable(t *testing.T) {
	valid, toRenew, expired, errored := CertCounts([]ComponentStatus{
		{SSLDaysLeft: days(200)},
		{SSLDaysLeft: days(90)},
		{SSLDaysLeft: days(12)},
		{SSLDaysLeft: days(-4)},
		{Status: StatusDown},
	})

	for _, c := range []struct {
		label     string
		got, want int
	}{
		{"valid", valid, 2},
		{"to renew", toRenew, 1},
		{"expired", expired, 1},
		{"error", errored, 1},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.label, c.got, c.want)
		}
	}
}
