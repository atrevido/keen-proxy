package api

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ordo-systems/keen-proxy/internal/update"
)

func TestUpdatePermissions(t *testing.T) {
	s, d := newServer()
	d.cfg.API.NoTokenFrom = []string{"192.168.7.0/24"}
	h := s.Handler()
	for _, tc := range []struct {
		name, method, path, body, token, origin string
		want                                    int
	}{
		{"trusted status", "GET", "/api/updates", "", "", "", 200},
		{"trusted check", "POST", "/api/updates/check", "{}", "", "", 200},
		{"install requires token", "POST", "/api/updates/install", `{"target":"v0.4.0"}`, "", "", 403},
		{"preferences require token", "PUT", "/api/updates/preferences", `{"time":"04:00"}`, "", "", 403},
		{"blocks cross-origin install", "POST", "/api/updates/install", `{"target":"v0.4.0"}`, "secret", "http://evil.example", 403},
		{"demo cannot configure updates", "PUT", "/api/updates/preferences", `{"time":"04:00"}`, "secret", "", 409},
		{"invalid schedule is rejected", "PUT", "/api/updates/preferences", `{"time":7}`, "secret", "", 400},
		{"demo cannot install", "POST", "/api/updates/install", `{"target":"v0.4.0"}`, "secret", "", 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.RemoteAddr = lan
			r.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.want, w.Body)
			}
		})
	}
}

func TestUpdateErrorsDoNotExposeInstallerDetails(t *testing.T) {
	s, _ := newServer()
	w := httptest.NewRecorder()
	s.updateError(w, errors.New("private installer error"))
	if w.Code != 500 || strings.Contains(w.Body.String(), "private installer error") {
		t.Fatalf("internal error was not safely reported: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	s.updateError(w, update.ErrPreferences)
	if w.Code != 400 {
		t.Fatalf("invalid preferences returned %d", w.Code)
	}
}
