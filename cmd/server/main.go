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

// parseFlags parses the server's command-line flags into a listen address and
// matcher config. Extracted from main so the flag/config logic is unit-testable.
func parseFlags(args []string) (addr string, cfg matcher.Config, err error) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	addrFlag := fs.String("addr", ":8080", "listen address")
	high := fs.Int64("high", matcher.DefaultConfig().HighImpactMinor, "high-impact threshold in minor units")
	critical := fs.Int64("critical", matcher.DefaultConfig().CriticalImpactMinor, "critical-impact threshold in minor units")
	if err := fs.Parse(args); err != nil {
		return "", matcher.Config{}, err
	}
	return *addrFlag, matcher.Config{HighImpactMinor: *high, CriticalImpactMinor: *critical}, nil
}

// newHTTPServer builds the configured *http.Server for the given handler.
func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
}

func main() {
	addr, cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := api.NewServer(cfg, log)
	httpServer := newHTTPServer(addr, server.Handler())

	// Run the listener in a goroutine so main can wait for a shutdown signal and
	// drain in-flight requests gracefully.
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr)
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
