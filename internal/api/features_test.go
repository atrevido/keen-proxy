package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogAndPresetPreview(t *testing.T) {
	s, d := newServer()
	h := s.Handler()
	for _, path := range []string{"/api/catalog", "/api/features"} {
		if w := do(t, h, "GET", path, "", "", lan); w.Code != 401 {
			t.Fatal(path, w.Code)
		}
	}
	w := do(t, h, "GET", "/api/catalog", "", "secret", lan)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Steam") {
		t.Fatal(w.Code, w.Body.String())
	}
	req := map[string]any{"config": d.cfg, "id": "steam", "target": "direct"}
	raw, _ := json.Marshal(req)
	w = do(t, h, "POST", "/api/presets/preview", string(raw), "secret", lan)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if d.applied != nil || len(d.cfg.Groups) != 0 {
		t.Fatal("preview changed live config")
	}
	if !strings.Contains(w.Body.String(), "steampowered.com") {
		t.Fatal(w.Body.String())
	}
}
