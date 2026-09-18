// Package api exposes the reconciliation engine over HTTP. The surface is
// deliberately small: a health check and a single reconcile endpoint that takes
// two uploaded CSV files and returns the versioned discrepancy report.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/contract"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/ingest"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/matcher"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/model"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/recon"
)

// maxUploadBytes caps each reconcile request body to protect the server from
// unbounded multipart uploads.
const maxUploadBytes = 32 << 20 // 32 MiB

// Server wires the engine to HTTP handlers.
type Server struct {
	engine *recon.Engine
	log    *slog.Logger
}

// NewServer builds a Server with the given matcher config. A nil logger falls
// back to the slog default.
func NewServer(cfg matcher.Config, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{engine: recon.New(cfg), log: log}
}

// Handler returns the fully-routed http.Handler, with request logging applied.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/reconcile", s.handleReconcile)
	return s.withLogging(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":           "ok",
		"contract_version": model.ContractVersion,
	})
}

// handleReconcile expects multipart/form-data with two file parts, "psp" and
// "ledger", each a CSV in that source's default layout.
func (s *Server) handleReconcile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "could not parse multipart form (send psp and ledger as file fields)", err)
		return
	}
	// Remove any on-disk temp files multipart may have spilled once we're done.
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	psp, err := parseUpload(r, "psp", model.SourcePSP, ingest.DefaultPSPColumns())
	if err != nil {
		writeError(w, http.StatusBadRequest, "parsing psp file", err)
		return
	}
	ledger, err := parseUpload(r, "ledger", model.SourceLedger, ingest.DefaultLedgerColumns())
	if err != nil {
		writeError(w, http.StatusBadRequest, "parsing ledger file", err)
		return
	}

	report := s.engine.Reconcile(psp, ledger)
	writeJSON(w, http.StatusOK, contract.FromReport(report))
}

func parseUpload(r *http.Request, field string, source model.Source, cm ingest.ColumnMap) ([]model.Transaction, error) {
	file, _, err := r.FormFile(field)
	if err != nil {
		return nil, fmt.Errorf("missing %q file field: %w", field, err)
	}
	defer file.Close()
	return ingest.ParseCSV(file, source, cm, ingest.DefaultStatusMap())
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// errorResponse is the JSON body returned for any 4xx/5xx.
type errorResponse struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

func writeError(w http.ResponseWriter, status int, msg string, err error) {
	resp := errorResponse{Error: msg}
	if err != nil {
		resp.Detail = err.Error()
	}
	// A body exceeding the size cap surfaces as this sentinel; report it as 413.
	if errors.As(err, new(*http.MaxBytesError)) {
		status = http.StatusRequestEntityTooLarge
	}
	writeJSON(w, status, resp)
}
