// Command osh is the Open Site Health process.
//
// This binary is a runnable skeleton: it loads config, opens a file-backed
// target store, exposes /healthz, and parks probe/alert stubs until
// issues #1–#3 are implemented.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bugman666/open-site-health/internal/alert"
	"github.com/bugman666/open-site-health/internal/config"
	"github.com/bugman666/open-site-health/internal/httpapi"
	"github.com/bugman666/open-site-health/internal/probe"
	"github.com/bugman666/open-site-health/internal/targets"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("osh: ")

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	store, err := targets.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("targets: %v", err)
	}

	alerts := alert.New(cfg)
	alerts.LogStub()

	scheduler := probe.New(cfg, store, alerts)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go scheduler.Start(ctx)

	srv := httpapi.New(cfg, store, alerts)
	if err := srv.ListenAndServe(ctx); err != nil {
		log.Fatalf("http: %v", err)
	}
}
