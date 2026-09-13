// Command osh is the Open Site Health process.
//
// It loads config, opens the file-backed target store, serves /healthz
// and /targets, and parks probe/alert stubs until issues #2 and #3.
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
