// Command osh is the Open Site Health process.
//
// It loads config, opens the file-backed target and probe stores, serves
// /healthz, /targets and /probes, and runs the probe scheduler. Alert
// delivery stays a stub until issue #3.
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

	results, err := probe.OpenResults(cfg.DataDir)
	if err != nil {
		log.Fatalf("probes: %v", err)
	}

	alerts := alert.New(cfg)
	alerts.LogStub()

	scheduler := probe.New(cfg, store, results, alerts)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go scheduler.Start(ctx)

	srv := httpapi.New(cfg, store, results, alerts)
	if err := srv.ListenAndServe(ctx); err != nil {
		log.Fatalf("http: %v", err)
	}
}
