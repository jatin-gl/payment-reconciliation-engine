package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/contract"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/matcher"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	return NewServer(matcher.DefaultConfig(), nil).Handler()
}

func TestHealth(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("body = %v", body)
	}
}

func TestReconcileEndpoint(t *testing.T) {
	srv := newTestServer(t)

	body, contentType := buildMultipart(t,
		"../../testdata/psp_settlement.csv",
		"../../testdata/internal_ledger.csv")

	req := httptest.NewRequest(http.MethodPost, "/v1/reconcile", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var report contract.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.ContractVersion == "" || report.Summary.DiscrepancyCount == 0 {
		t.Errorf("unexpected report: %+v", report.Summary)
	}
	if report.Summary.MatchedCount != 1 {
		t.Errorf("matched = %d, want 1", report.Summary.MatchedCount)
	}
	// Every discrepancy in the wire form must carry a stable id and a type.
	for _, d := range report.Discrepancies {
		if d.ID == "" || d.Type == "" {
			t.Errorf("wire discrepancy missing id/type: %+v", d)
		}
	}
}

func TestReconcileEndpoint_MissingFile(t *testing.T) {
	srv := newTestServer(t)

	// Only include the psp part; ledger is absent.
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("psp", "psp.csv")
	part.Write([]byte("transaction_id,amount,currency\nTXN-1,10.00,USD\n"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/reconcile", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func buildMultipart(t *testing.T, pspPath, ledgerPath string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for field, path := range map[string]string{"psp": pspPath, "ledger": ledgerPath} {
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open %s: %v", path, err)
		}
		part, err := w.CreateFormFile(field, path)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := io.Copy(part, f); err != nil {
			t.Fatalf("copy: %v", err)
		}
		f.Close()
	}
	w.Close()
	return &buf, w.FormDataContentType()
}
