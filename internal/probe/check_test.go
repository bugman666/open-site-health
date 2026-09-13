package probe

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bugman666/open-site-health/internal/alert"
	"github.com/bugman666/open-site-health/internal/config"
	"github.com/bugman666/open-site-health/internal/targets"
)

func newTestScheduler(t *testing.T, cfg config.Config) (*Scheduler, *targets.Store, *ResultStore) {
	t.Helper()
	if cfg.Listen == "" {
		cfg = config.Defaults()
	}
	cfg.AllowPrivateTargets = true
	dir := t.TempDir()
	store, err := targets.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.AllowPrivate = true
	results, err := OpenResults(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := New(cfg, store, results, alert.New(cfg))
	s.Timeout = time.Second
	return s, store, results
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func leafCert(t *testing.T, notBefore, notAfter time.Time) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func startTLS(t *testing.T, cert tls.Certificate, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(h)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func mustAdd(t *testing.T, store *targets.Store, raw string) targets.Target {
	t.Helper()
	tgt, err := store.Add(raw)
	if err != nil {
		t.Fatal(err)
	}
	return tgt
}

func mustLatest(t *testing.T, results *ResultStore, id string) Result {
	t.Helper()
	r, err := results.Latest(id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestClassifyCertBoundaries(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	warnDays := 14

	okCert := &x509.Certificate{NotAfter: now.AddDate(0, 0, 30)}
	if st, _ := classifyCert(okCert, warnDays, now); st != CertOK {
		t.Fatalf("30d: %s", st)
	}

	warnExact := &x509.Certificate{NotAfter: now.AddDate(0, 0, warnDays)}
	if st, _ := classifyCert(warnExact, warnDays, now); st != CertWarn {
		t.Fatalf("exactly warn window: %s", st)
	}

	expired := &x509.Certificate{NotAfter: now.Add(-time.Second)}
	if st, _ := classifyCert(expired, warnDays, now); st != CertExpired {
		t.Fatalf("expired: %s", st)
	}

	if st, _ := classifyCert(warnExact, 0, now); st != CertOK {
		t.Fatalf("warnDays=0 should not warn: %s", st)
	}
}

// TC2.2: a 2xx target is marked up and the result is stored.
func TestAvailabilityUp(t *testing.T) {
	s, store, results := newTestScheduler(t, config.Defaults())
	srv := httptest.NewServer(okHandler())
	t.Cleanup(srv.Close)

	tgt := mustAdd(t, store, srv.URL)
	if err := s.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := mustLatest(t, results, tgt.ID)
	if got.Availability != Up {
		t.Fatalf("availability: %s (%s)", got.Availability, got.Message)
	}
	if got.CertStatus != CertNA {
		t.Fatalf("http target cert: %s", got.CertStatus)
	}
	if got.HTTPStatus != http.StatusOK {
		t.Fatalf("http status: %d", got.HTTPStatus)
	}
	if got.CheckedAt.IsZero() || got.URL == "" {
		t.Fatalf("incomplete result: %#v", got)
	}
}

// TC2.3: timeout, connect failure, and 5xx are down and not a cert fault.
func TestAvailabilityDown(t *testing.T) {
	t.Run("connection refused", func(t *testing.T) {
		s, store, results := newTestScheduler(t, config.Defaults())
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()

		tgt := mustAdd(t, store, "http://"+addr)
		if err := s.CheckAll(context.Background()); err != nil {
			t.Fatal(err)
		}
		got := mustLatest(t, results, tgt.ID)
		if got.Availability != Down {
			t.Fatalf("availability: %s", got.Availability)
		}
		if got.CertStatus != CertNA {
			t.Fatalf("connect fail should not look like a cert issue: %s", got.CertStatus)
		}
		if got.Message == "" {
			t.Fatal("expected error message")
		}
	})

	t.Run("http 500", func(t *testing.T) {
		s, store, results := newTestScheduler(t, config.Defaults())
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		tgt := mustAdd(t, store, srv.URL)
		if err := s.CheckAll(context.Background()); err != nil {
			t.Fatal(err)
		}
		got := mustLatest(t, results, tgt.ID)
		if got.Availability != Down {
			t.Fatalf("availability: %s", got.Availability)
		}
		if got.CertStatus != CertNA {
			t.Fatalf("http 5xx cert: %s", got.CertStatus)
		}
		if got.HTTPStatus != http.StatusInternalServerError {
			t.Fatalf("http status: %d", got.HTTPStatus)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		s, store, results := newTestScheduler(t, config.Defaults())
		s.Timeout = 150 * time.Millisecond
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(2 * time.Second)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)

		tgt := mustAdd(t, store, srv.URL)
		if err := s.CheckAll(context.Background()); err != nil {
			t.Fatal(err)
		}
		got := mustLatest(t, results, tgt.ID)
		if got.Availability != Down {
			t.Fatalf("availability: %s", got.Availability)
		}
		if got.CertStatus != CertNA {
			t.Fatalf("timeout cert: %s", got.CertStatus)
		}
	})

	t.Run("5xx with healthy cert", func(t *testing.T) {
		cfg := config.Defaults()
		cfg.TLSWarnDays = 14
		s, store, results := newTestScheduler(t, cfg)
		now := time.Now()
		cert := leafCert(t, now.Add(-time.Hour), now.Add(365*24*time.Hour))
		srv := startTLS(t, cert, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))

		tgt := mustAdd(t, store, srv.URL)
		if err := s.CheckAll(context.Background()); err != nil {
			t.Fatal(err)
		}
		got := mustLatest(t, results, tgt.ID)
		if got.Availability != Down {
			t.Fatalf("availability: %s (%s)", got.Availability, got.Message)
		}
		if got.CertStatus != CertOK {
			t.Fatalf("5xx must stay distinct from cert faults: cert=%s", got.CertStatus)
		}
	})
}

// TC2.4: cert still valid but inside the warn window → warn, not expired, not down.
func TestCertWarn(t *testing.T) {
	cfg := config.Defaults()
	cfg.TLSWarnDays = 14
	s, store, results := newTestScheduler(t, cfg)

	now := time.Now()
	cert := leafCert(t, now.Add(-time.Hour), now.Add(7*24*time.Hour))
	srv := startTLS(t, cert, okHandler())

	tgt := mustAdd(t, store, srv.URL)
	if err := s.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := mustLatest(t, results, tgt.ID)
	if got.Availability != Up {
		t.Fatalf("availability: %s (%s)", got.Availability, got.Message)
	}
	if got.CertStatus != CertWarn {
		t.Fatalf("cert: %s", got.CertStatus)
	}
	if got.CertExpiry == nil {
		t.Fatal("expected cert_expiry")
	}
}

// TC2.5: expired cert is marked expired and is not a bare connectivity failure.
func TestCertExpired(t *testing.T) {
	cfg := config.Defaults()
	cfg.TLSWarnDays = 14
	s, store, results := newTestScheduler(t, cfg)

	now := time.Now()
	cert := leafCert(t, now.Add(-48*time.Hour), now.Add(-time.Hour))
	srv := startTLS(t, cert, okHandler())

	tgt := mustAdd(t, store, srv.URL)
	if err := s.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := mustLatest(t, results, tgt.ID)
	if got.CertStatus != CertExpired {
		t.Fatalf("cert: %s", got.CertStatus)
	}
	if got.Availability != Up {
		t.Fatalf("expired cert should still be reachable (distinct from down): %s %s", got.Availability, got.Message)
	}
	if got.CertExpiry == nil {
		t.Fatal("expected cert_expiry")
	}
}

func TestCheckAllEmptyStore(t *testing.T) {
	s, _, _ := newTestScheduler(t, config.Defaults())
	if err := s.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestProbeRefusesBlockedDestination(t *testing.T) {
	s, store, results := newTestScheduler(t, config.Defaults())
	tgt := mustAdd(t, store, "http://169.254.169.254/latest/meta-data/")
	s.cfg.AllowPrivateTargets = false

	start := time.Now()
	if err := s.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("blocked destination should not wait on the network")
	}
	got := mustLatest(t, results, tgt.ID)
	if got.Availability != Down {
		t.Fatalf("availability: %s", got.Availability)
	}
	if got.HTTPStatus != 0 {
		t.Fatalf("should not have fetched metadata: %#v", got)
	}
	if got.Message == "" {
		t.Fatal("expected blocked-destination message")
	}
}

func TestProbeRefusesPrivateEvenIfAlreadyStored(t *testing.T) {
	s, store, results := newTestScheduler(t, config.Defaults())
	srv := httptest.NewServer(okHandler())
	t.Cleanup(srv.Close)
	tgt := mustAdd(t, store, srv.URL)

	s.cfg.AllowPrivateTargets = false
	if err := s.CheckAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := mustLatest(t, results, tgt.ID)
	if got.Availability != Down {
		t.Fatalf("availability: %s (%s)", got.Availability, got.Message)
	}
	if got.HTTPStatus == http.StatusOK {
		t.Fatalf("must not fetch a private listener: %#v", got)
	}
}
