package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jatin-gl/payment-reconciliation-engine/internal/api"
	"github.com/jatin-gl/payment-reconciliation-engine/internal/matcher"
)

func TestParseFlags_Defaults(t *testing.T) {
	addr, cfg, err := parseFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if addr != ":8080" {
		t.Errorf("addr = %q, want :8080", addr)
	}
	def := matcher.DefaultConfig()
	if cfg.HighImpactMinor != def.HighImpactMinor || cfg.CriticalImpactMinor != def.CriticalImpactMinor {
		t.Errorf("cfg = %+v, want defaults %+v", cfg, def)
	}
}

func TestParseFlags_Custom(t *testing.T) {
	addr, cfg, err := parseFlags([]string{"--addr", ":9000", "--high", "500", "--critical", "5000"})
	if err != nil {
		t.Fatal(err)
	}
	if addr != ":9000" || cfg.HighImpactMinor != 500 || cfg.CriticalImpactMinor != 5000 {
		t.Errorf("addr=%q cfg=%+v", addr, cfg)
	}
}

func TestParseFlags_Invalid(t *testing.T) {
	if _, _, err := parseFlags([]string{"--high", "notanumber"}); err == nil {
		t.Fatal("an invalid numeric flag should error")
	}
}

func TestNewHTTPServer_WiresHandlerWithTimeouts(t *testing.T) {
	srv := newHTTPServer(":0", api.NewServer(matcher.DefaultConfig(), nil).Handler())
	if srv.ReadHeaderTimeout == 0 || srv.ReadTimeout == 0 || srv.WriteTimeout == 0 {
		t.Error("server timeouts should be configured")
	}
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthz via wired handler = %d, want 200", rec.Code)
	}
}
