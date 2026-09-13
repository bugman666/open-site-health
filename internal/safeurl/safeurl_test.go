package safeurl

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestValidateBlocksDangerousDestinations(t *testing.T) {
	cases := []struct {
		in           string
		allowPrivate bool
		wantErr      bool
	}{
		{in: "https://example.org", wantErr: false},
		{in: "http://example.org/status", wantErr: false},
		{in: "http://8.8.8.8/", wantErr: false},
		{in: "http://172.15.0.1/", wantErr: false},
		{in: "http://172.32.0.1/", wantErr: false},
		{in: "ftp://example.org", wantErr: true},
		{in: "file:///etc/passwd", wantErr: true},
		{in: "gopher://example.org", wantErr: true},
		{in: "http://", wantErr: true},
		{in: "http://127.0.0.1/", wantErr: true},
		{in: "http://127.0.0.1:8080/healthz", wantErr: true},
		{in: "http://[::1]/", wantErr: true},
		{in: "http://localhost/", wantErr: true},
		{in: "http://LOCALHOST/foo", wantErr: true},
		{in: "http://foo.localhost/", wantErr: true},
		{in: "http://169.254.169.254/", wantErr: true},
		{in: "http://169.254.169.254/latest/meta-data/", wantErr: true},
		{in: "http://10.0.0.1/", wantErr: true},
		{in: "http://192.168.1.1/", wantErr: true},
		{in: "http://172.16.0.1/", wantErr: true},
		{in: "http://172.31.255.255/", wantErr: true},
		{in: "http://0.0.0.0/", wantErr: true},
		{in: "http://[fe80::1]/", wantErr: true},
		{in: "http://[fc00::1]/", wantErr: true},
		{in: "http://100.64.0.1/", wantErr: true},
		{in: "http://metadata.google.internal/", wantErr: true},
		{in: "http://metadata.goog/", wantErr: true},
		{in: "http://metadata/", wantErr: true},
		{in: "http://127.0.0.1/", allowPrivate: true, wantErr: false},
		{in: "http://169.254.169.254/", allowPrivate: true, wantErr: false},
		{in: "http://localhost/", allowPrivate: true, wantErr: false},
		{in: "ftp://127.0.0.1/", allowPrivate: true, wantErr: true},
	}
	for _, tc := range cases {
		err := Validate(tc.in, tc.allowPrivate)
		if tc.wantErr {
			if !errors.Is(err, ErrBlocked) {
				t.Fatalf("Validate(%q, allowPrivate=%v): want ErrBlocked, got %v", tc.in, tc.allowPrivate, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("Validate(%q, allowPrivate=%v): %v", tc.in, tc.allowPrivate, err)
		}
	}
}

func TestRestrictedDialBlocksLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	dial := RestrictedDialContext(&net.Dialer{Timeout: time.Second}, false)
	conn, err := dial(context.Background(), "tcp", ln.Addr().String())
	if conn != nil {
		_ = conn.Close()
		t.Fatal("expected no connection to loopback")
	}
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("got %v", err)
	}
}

func TestRestrictedDialAllowsLoopbackWhenConfigured(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_ = c.Close()
		}
	}()

	dial := RestrictedDialContext(&net.Dialer{Timeout: time.Second}, true)
	conn, err := dial(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}
