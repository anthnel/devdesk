package status

import (
	"errors"
	"time"

	"github.com/anthnel/devdesk/internal/status"
	"github.com/anthnel/devdesk/internal/viewer"
)

var errNoCertificates = errors.New("no certificates found")

// certDialTimeout is the fallback used when the monitor declares none of its
// own (config.ComponentConfig.Timeout == 0) — the same case SSLChecker itself
// falls back on, but resolved here rather than through cfg.Status.Timeout: that
// field is cast straight to a time.Duration elsewhere without a *time.Second
// multiplication, which would make this dial time out in nanoseconds.
const certDialTimeout = 10 * time.Second

// certSource dials the monitored target again and formats the certificate
// chain it presents.
//
// Unlike inspectSource/logsSource it cannot serve the last check's result:
// ComponentStatus keeps only the leaf's expiry, issuer and days-left — the
// chain itself was already discarded by the time this view sees it — so
// opening the document means paying the dial SSLChecker already pays on every
// refresh, a second time.
type certSource struct {
	Target    string
	Component string
	Timeout   time.Duration
}

func (s certSource) Name() string { return "certificate · " + s.Component }

func (s certSource) Kind() viewer.Kind { return viewer.KindPlain }

func (s certSource) Load() ([]byte, error) {
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = certDialTimeout
	}
	certs, err := status.FetchCertificateChain(s.Target, timeout)
	if err != nil {
		return nil, err
	}
	if len(certs) == 0 {
		return nil, errNoCertificates
	}
	return status.FormatCertificateChain(certs), nil
}
