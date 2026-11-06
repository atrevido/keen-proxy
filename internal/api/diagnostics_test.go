package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/atrevido/keenetic-proxy/internal/daemon"
	"github.com/atrevido/keenetic-proxy/internal/model"
)

func TestDiagnosticEndpointAuthAndUnknownConnection(t *testing.T) {
	s, _ := newServer()
	h := s.Handler()
	if w := do(t, h, "POST", "/api/diagnostics/check", `{"connection":"unknown"}`, "", lan); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := do(t, h, "POST", "/api/diagnostics/check", `{"connection":"unknown"}`, "secret", lan); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := do(t, h, "POST", "/api/diagnostics/check", strings.Repeat("x", 5000), "secret", lan); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestDiagnosticEndpointDemoSuccessAndUnavailableCapability(t *testing.T) {
	s, fake := newServer()
	fake.cfg.Connections = []model.Connection{{ID: "one", Name: "One", Type: model.ConnLink}}
	if w := do(t, s.Handler(), "POST", "/api/diagnostics/check", `{"connection":"one"}`, "secret", lan); w.Code != 501 {
		t.Fatal(w.Code, w.Body.String())
	}
	s.D = &Demo{cfg: fake.cfg}
	w := do(t, s.Handler(), "POST", "/api/diagnostics/check", `{"connection":"one"}`, "secret", lan)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var report daemon.DiagnosticReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Demo || report.Connection != "one" || len(report.Results) != 3 {
		t.Fatal("bad report", report)
	}
	for _, body := range []string{`{"connection":"one","url":"https://private.test"}`, `{"connection":"one"}{}`} {
		if w := do(t, s.Handler(), "POST", "/api/diagnostics/check", body, "secret", lan); w.Code != 400 {
			t.Fatal("accepted unexpected request", w.Code)
		}
	}
}
