// Command server runs the reconciliation engine as an HTTP service.
//
// Usage:
//
//	server [--addr :8080] [--high 10000] [--critical 100000]
//
// Endpoints:
//
//	GET  /healthz       liveness + contract version
//	POST /v1/reconcile  multipart form with "psp" and "ledger" CSV file fields
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/api"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/matcher"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	high := flag.Int64("high", matcher.DefaultConfig().HighImpactMinor, "high-impact threshold in minor units")
	critical := flag.Int64("critical", matcher.DefaultConfig().CriticalImpactMinor, "critical-impact threshold in minor units")
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := api.NewServer(matcher.Config{HighImpactMinor: *high, CriticalImpactMinor: *critical}, log)

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
	}

	// Run the listener in a goroutine so main can wait for a shutdown signal and
	// drain in-flight requests gracefully.
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", *addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		log.Error("server error", "err", err)
		os.Exit(1)
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("graceful shutdown failed", "err", err)
			os.Exit(1)
		}
	}
}
