package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/atrevido/keenetic-proxy/internal/daemon"
	"github.com/atrevido/keenetic-proxy/internal/journal"
	"github.com/atrevido/keenetic-proxy/internal/model"
	"github.com/atrevido/keenetic-proxy/internal/stats"
)

type fakeDaemon struct {
	cfg      *model.Config
	applied  *model.Config
	failErr  error
	journals journal.Set
}

func (f *fakeDaemon) Journal(k journal.Kind) *journal.Journal { return f.journals[k] }
func (f *fakeDaemon) Mask() func(string) string               { return journal.Masker(f.cfg) }
func (f *fakeDaemon) NoTokenFrom() []netip.Prefix             { return f.cfg.API.NoTokenPrefixes() }

func (f *fakeDaemon) Config() *model.Config { return f.cfg }
func (f *fakeDaemon) Apply(_ context.Context, c *model.Config) error {
	if f.failErr != nil {
		return f.failErr
	}
	if err := c.Validate(); err != nil {
		return err
	}
	f.applied, f.cfg = c, c
	return nil
}
func (f *fakeDaemon) Status(context.Context) daemon.Status { return daemon.Status{Mode: "redirect"} }
func (f *fakeDaemon) Devices() []daemon.Device {
	return []daemon.Device{{MAC: "aa:bb:cc:dd:ee:ff", IP: "192.168.7.10"}}
}

func (f *fakeDaemon) Stats(bool) stats.Snapshot { return stats.Snapshot{} }
func (f *fakeDaemon) Hosts(string, string, int) ([]stats.Host, int) {
	return []stats.Host{{Host: "youtube.com", Down: 10}}, 1
}

func newServer() (*Server, *fakeDaemon) {
	cfg := &model.Config{}
	cfg.Defaults()
	d := &fakeDaemon{cfg: cfg, journals: journal.NewSet("")}
	ui := fstest.MapFS{"index.html": {Data: []byte("<div id=app>")}, "assets/app.js": {Data: []byte("js")}}
	return &Server{D: d, Token: "secret", UI: ui}, d
}

func do(t *testing.T, h http.Handler, method, path, body, token, remote string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = remote
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const lan = "192.168.7.88:5000"

func TestAuthAndLANOnly(t *testing.T) {
	s, _ := newServer()
	h := s.Handler()
	if c := do(t, h, "GET", "/api/status", "", "", lan).Code; c != http.StatusUnauthorized {
		t.Errorf("without token: %d", c)
	}
	if c := do(t, h, "GET", "/api/status", "", "wrong", lan).Code; c != http.StatusUnauthorized {
		t.Errorf("invalid token: %d", c)
	}
	if c := do(t, h, "GET", "/api/status", "", "secret", lan).Code; c != http.StatusOK {
		t.Errorf("with token: %d", c)
	}
	if c := do(t, h, "GET", "/api/status", "", "secret", "8.8.8.8:1234").Code; c != http.StatusForbidden {
		t.Errorf("from the internet: %d", c)
	}
}

func TestNoTokenFrom(t *testing.T) {
	s, d := newServer()
	d.cfg.API.NoTokenFrom = []string{"192.168.7.0/24"}
	h := s.Handler()
	if c := do(t, h, "GET", "/api/status", "", "", lan).Code; c != http.StatusOK {
		t.Errorf("from trusted network without token: %d", c)
	}
	if c := do(t, h, "GET", "/api/status", "", "", "10.1.30.5:4000").Code; c != http.StatusUnauthorized {
		t.Errorf("from another network without token: %d", c)
	}
	// A forwarding header cannot turn an untrusted peer into a trusted address.
	r := httptest.NewRequest("GET", "/api/status", nil)
	r.RemoteAddr = "10.1.30.5:4000"
	r.Header.Set("X-Forwarded-For", "192.168.7.20")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("X-Forwarded-For: %d", rec.Code)
	}

	var sess map[string]any
	do(t, h, "GET", "/api/session?open=1", "", "", lan) // Reopening within an hour is not a new login.
	json.Unmarshal(do(t, h, "GET", "/api/session?open=1", "", "", lan).Body.Bytes(), &sess)
	if sess["authed"] != true || sess["trusted"] != true || sess["network"] != "192.168.7.0/24" {
		t.Errorf("session from trusted network: %v", sess)
	}
	json.Unmarshal(do(t, h, "GET", "/api/session", "", "", "10.1.30.5:4000").Body.Bytes(), &sess)
	if sess["authed"] != false || sess["trusted"] != false {
		t.Errorf("session from untrusted network: %v", sess)
	}
	if p := d.journals[journal.Audit].Find(journal.Query{Text: "without token"}, nil); len(p.Entries) != 1 {
		t.Errorf("tokenless login in audit: %d", len(p.Entries))
	}
}

func TestLoginSetsCookie(t *testing.T) {
	s, _ := newServer()
	w := do(t, s.Handler(), "POST", "/api/login", `{"token":"secret"}`, "", lan)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Set-Cookie"), "keenproxy_token=secret") {
		t.Fatalf("login: %d %q", w.Code, w.Header().Get("Set-Cookie"))
	}
	r := httptest.NewRequest("GET", "/api/devices", nil)
	r.RemoteAddr = lan
	r.AddCookie(&http.Cookie{Name: cookieName, Value: "secret"})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "aa:bb:cc:dd:ee:ff") {
		t.Errorf("using cookie: %d %s", rec.Code, rec.Body)
	}
}

func TestPutConfig(t *testing.T) {
	s, d := newServer()
	h := s.Handler()
	body := `{"groups":[{"name":"g","target":"direct","entries":["example.com"]}]}`
	w := do(t, h, "PUT", "/api/config", body, "secret", lan)
	if w.Code != http.StatusOK || d.applied == nil || d.applied.Groups[0].Entries[0] != "example.com" {
		t.Fatalf("PUT: %d %s", w.Code, w.Body)
	}
	bad := `{"groups":[{"name":"g","target":"nowhere","entries":["example.com"]}]}`
	w = do(t, h, "PUT", "/api/config", bad, "secret", lan)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "nowhere") {
		t.Errorf("invalid configuration: %d %s", w.Code, w.Body)
	}
	w = do(t, h, "PUT", "/api/config", `{"unknown":1}`, "secret", lan)
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown field: %d", w.Code)
	}
	d.failErr = errors.New("sing-box rejected")
	w = do(t, h, "PUT", "/api/config", body, "secret", lan)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "sing-box") {
		t.Errorf("apply error: %d %s", w.Code, w.Body)
	}
}

func TestParseLink(t *testing.T) {
	s, _ := newServer()
	w := do(t, s.Handler(), "POST", "/api/links/parse", "{\"link\":\"trojan://pw@tr.example.com:443#\u041c\u043e\u0439\"}", "secret", lan)
	var got map[string]any
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got["protocol"] != "trojan" || got["name"] != "\u041c\u043e\u0439" || got["server"] != "tr.example.com" {
		t.Errorf("parse: %d %v", w.Code, got)
	}
	w = do(t, s.Handler(), "POST", "/api/links/parse", `{"link":"vless://u@h:1?type=xhttp"}`, "secret", lan)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("invalid link: %d", w.Code)
	}
}

func TestUIFallback(t *testing.T) {
	s, _ := newServer()
	h := s.Handler()
	for _, p := range []string{"/", "/connections", "/lists/whitelist"} {
		w := do(t, h, "GET", p, "", "", lan)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "id=app") {
			t.Errorf("%s: %d %q", p, w.Code, w.Body.String())
		}
	}
	if w := do(t, h, "GET", "/assets/app.js", "", "", lan); w.Body.String() != "js" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("assets: %q %q", w.Body.String(), w.Header().Get("Cache-Control"))
	}
	if w := do(t, h, "GET", "/api/nope", "", "secret", lan); w.Code != 404 {
		t.Errorf("unknown API: %d", w.Code)
	}
}

func TestStatsEndpoints(t *testing.T) {
	s, _ := newServer()
	if w := do(t, s.Handler(), "GET", "/api/stats?series=1", "", "secret", lan); w.Code != 200 || !strings.Contains(w.Body.String(), "outbounds") {
		t.Errorf("stats: %d %s", w.Code, w.Body)
	}
	if w := do(t, s.Handler(), "GET", "/api/stats/hosts?tag=fi", "", "secret", lan); w.Code != 200 || !strings.Contains(w.Body.String(), "youtube.com") {
		t.Errorf("hosts: %d %s", w.Code, w.Body)
	}
}

func TestJournalEndpoints(t *testing.T) {
	s, d := newServer()
	h := s.Handler()
	do(t, h, "POST", "/api/login", `{"token":"nope"}`, "", lan)
	do(t, h, "POST", "/api/login", `{"token":"secret"}`, "", lan)
	d.journals[journal.System].Add(journal.Entry{Level: "WARN", Source: "keenproxy", Message: "token secret appeared in message"})

	w := do(t, h, "GET", "/api/journal/audit?result=error", "", "secret", lan)
	var page journal.Page
	json.Unmarshal(w.Body.Bytes(), &page)
	if w.Code != http.StatusOK || len(page.Entries) != 1 || !strings.Contains(page.Entries[0].Details, "192.168.7.88") {
		t.Errorf("login audit: %d %s", w.Code, w.Body)
	}
	if !slices.Contains(page.Facets["result"], "ok") {
		t.Errorf("filter values: %v", page.Facets)
	}
	if c := do(t, h, "GET", "/api/journal/nope", "", "secret", lan).Code; c != http.StatusNotFound {
		t.Errorf("unknown journal: %d", c)
	}
	w = do(t, h, "GET", "/api/journal/system/export?since=1h", "", "secret", lan)
	if !strings.Contains(w.Header().Get("Content-Disposition"), "keenproxy-system-") || strings.Contains(w.Body.String(), "secret") ||
		!strings.Contains(w.Body.String(), "appeared in message") {
		t.Errorf("export: %q\n%s", w.Header().Get("Content-Disposition"), w.Body)
	}
}
