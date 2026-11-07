package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ordo-systems/keen-proxy/internal/model"
)

func TestYAMLPreviewAndExport(t *testing.T) {
	s, d := newServer()
	h := s.Handler()
	if w := do(t, h, "GET", "/api/config/yaml", "", "", lan); w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code)
	}
	w := do(t, h, "POST", "/api/config/import", `{"yaml":"{}"}`, "secret", lan)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if d.applied != nil {
		t.Fatal("preview applied")
	}
	var c map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c["schema_version"] != float64(1) {
		t.Fatal(c)
	}
	w = do(t, h, "POST", "/api/config/import", `{"yaml":"{}\n---\n{}"}`, "secret", lan)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = do(t, h, "GET", "/api/config/yaml", "", "secret", lan)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w.Header().Get("Content-Disposition") == "" {
		t.Fatal("missing download header")
	}
}

func TestSupportExportRemovesURLSecrets(t *testing.T) {
	s, d := newServer()
	d.cfg.DNS.Upstreams = []string{"https://dns.example/private-token"}
	d.cfg.GeoIP.Databases = []model.GeoIPDatabase{{ID: "custom-1", URL: "https://geo.example/geo-token", Enabled: true}}
	d.cfg.Failover.URL = "https://health.example/health-token"
	w := do(t, s.Handler(), "GET", "/api/config/yaml?redacted=1", "", "secret", lan)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, v := range []string{"private-token", "geo-token", "health-token"} {
		if strings.Contains(w.Body.String(), v) {
			t.Fatal("secret leaked", v)
		}
	}
}
