package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ordo-systems/keen-proxy/internal/daemon"
	"github.com/ordo-systems/keen-proxy/internal/model"
)

func TestGeoEndpoints(t *testing.T) {
	cfg := &model.Config{}
	cfg.Defaults()
	s := &Server{D: NewDemo(cfg), Token: "secret"}
	h := s.Handler()
	if w := do(t, h, "POST", "/api/geoip/lookup", `{"ip":"77.88.8.8"}`, "", lan); w.Code != 401 {
		t.Fatal("authorization required", w.Code)
	}
	for _, body := range []string{`{"ip":"example.com"}`, `{"ip":"fe80::1%eth0"}`, `{"ip":"1.1.1.1","extra":1}`} {
		if w := do(t, h, "POST", "/api/geoip/lookup", body, "secret", lan); w.Code != 400 {
			t.Errorf("%s: %d", body, w.Code)
		}
	}
	w := do(t, h, "POST", "/api/geoip/lookup", `{"ip":"77.88.8.8"}`, "secret", lan)
	var res daemon.GeoLookup
	json.Unmarshal(w.Body.Bytes(), &res)
	if w.Code != 200 || res.Country != "RU" || res.Database != "dbip" || len(res.Results) != 1 {
		t.Fatalf("enabled: %d %s", w.Code, w.Body)
	}
	// A disabled database is tested only on request and only after a download.
	w = do(t, h, "POST", "/api/geoip/lookup", `{"ip":"77.88.8.8","id":"loyalsoldier"}`, "secret", lan)
	if !strings.Contains(w.Body.String(), "not downloaded") {
		t.Fatalf("before download: %s", w.Body)
	}
	if w := do(t, h, "POST", "/api/geoip/update", `{"id":"loyalsoldier"}`, "secret", lan); w.Code != 200 || !strings.Contains(w.Body.String(), `"demo":true`) {
		t.Fatalf("download: %d %s", w.Code, w.Body)
	}
	w = do(t, h, "POST", "/api/geoip/lookup", `{"ip":"77.88.8.8","id":"loyalsoldier"}`, "secret", lan)
	json.Unmarshal(w.Body.Bytes(), &res)
	if res.Country != "RU" || res.Results[0].Enabled {
		t.Fatalf("disabled database: %s", w.Body)
	}
	if w := do(t, h, "POST", "/api/geoip/lookup", `{"ip":"77.88.8.8","id":"nope"}`, "secret", lan); w.Code != 400 {
		t.Fatalf("unknown database: %d", w.Code)
	}
	w = do(t, h, "GET", "/api/geoip", "", "secret", lan)
	var list geoResponse
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Databases) != len(cfg.GeoIP.Databases) || !list.Demo {
		t.Fatalf("list: %s", w.Body)
	}
	w = do(t, h, "GET", "/api/catalog", "", "secret", lan)
	if !strings.Contains(w.Body.String(), `"geoip":[{"id":"dbip"`) {
		t.Fatalf("catalog: %s", w.Body)
	}
}
