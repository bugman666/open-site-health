package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bugman666/open-site-health/internal/targets"
)

const (
	defaultProbeTimeout = 10 * time.Second
	maxResponseBody     = 1 << 20
	userAgent           = "open-site-health/0.1"
)

func (s *Scheduler) probeTimeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return defaultProbeTimeout
}

func (s *Scheduler) checkOne(ctx context.Context, t targets.Target) Result {
	now := time.Now().UTC()
	r := Result{
		TargetID:   t.ID,
		URL:        t.URL,
		CheckedAt:  now,
		CertStatus: CertNA,
	}

	timeout := s.probeTimeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		r.Availability = Down
		r.Message = err.Error()
		return r
	}
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			// Skip verify so an expired or untrusted leaf is still readable
			// and is classified as a cert problem, not as downtime.
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
			DisableKeepAlives: true,
		},
	}

	start := time.Now()
	resp, err := client.Do(req)
	r.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		r.Availability = Down
		r.Message = err.Error()
		if isHTTPS(t.URL) {
			r.CertStatus, r.CertExpiry = s.lookupCert(ctx, t.URL, now)
		}
		return r
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBody))

	r.HTTPStatus = resp.StatusCode
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		r.Availability = Up
	} else {
		r.Availability = Down
		r.Message = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		r.CertStatus, r.CertExpiry = classifyCert(resp.TLS.PeerCertificates[0], s.cfg.TLSWarnDays, now)
	} else if isHTTPS(t.URL) {
		r.CertStatus, r.CertExpiry = s.lookupCert(ctx, t.URL, now)
	}
	return r
}

func (s *Scheduler) lookupCert(ctx context.Context, rawURL string, now time.Time) (CertStatus, *time.Time) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return CertNA, nil
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "443")
	}
	dialer := &tls.Dialer{
		Config: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec
			ServerName:         u.Hostname(),
		},
	}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return CertNA, nil
	}
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return CertNA, nil
	}
	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return CertNA, nil
	}
	return classifyCert(certs[0], s.cfg.TLSWarnDays, now)
}

func classifyCert(cert *x509.Certificate, warnDays int, now time.Time) (CertStatus, *time.Time) {
	if cert == nil {
		return CertNA, nil
	}
	exp := cert.NotAfter.UTC()
	expiry := exp
	if !now.Before(exp) {
		return CertExpired, &expiry
	}
	if warnDays > 0 && !exp.After(now.AddDate(0, 0, warnDays)) {
		return CertWarn, &expiry
	}
	return CertOK, &expiry
}

func isHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && strings.EqualFold(u.Scheme, "https")
}
