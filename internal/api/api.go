// Package api serves the keenProxy HTTP API and web interface.
//
// Authentication accepts Authorization: Bearer or the keenproxy_token cookie.
// The token is loaded from api.token_file or created on first startup.
// A separate address guard restricts access to private, loopback, and link-local
// peers even when the listening port is exposed.
package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/daemon"
	"github.com/ordo-systems/keen-proxy/internal/journal"
	"github.com/ordo-systems/keen-proxy/internal/links"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/ordo-systems/keen-proxy/internal/stats"
	"github.com/ordo-systems/keen-proxy/internal/trusttunnel"
	"github.com/ordo-systems/keen-proxy/internal/update"
)

const cookieName = "keenproxy_token"

// Daemon defines the daemon capabilities used by the API and its test doubles.
type Daemon interface {
	Domains() daemon.DomainStatus
	RefreshDomains(context.Context, []string, bool) (daemon.DomainStatus, error)
	Config() *model.Config
	Apply(ctx context.Context, cfg *model.Config) error
	Status(ctx context.Context) daemon.Status
	Devices() []daemon.Device
	Stats(withSeries bool) stats.Snapshot
	Hosts(tag, query string, limit int) ([]stats.Host, int)
	Journal(kind journal.Kind) *journal.Journal
	// Mask redacts configuration secrets from exported journal text.
	Mask() func(string) string
	// NoTokenFrom lists networks permitted to access the panel without a token.
	NoTokenFrom() []netip.Prefix
}

// Server binds daemon operations, authentication policy, and embedded UI assets.
type Server struct {
	D       Daemon
	Token   string
	UI      fs.FS
	Log     *slog.Logger
	Updates *update.Manager

	supportBusy atomic.Bool

	// Track the last audited trusted login per address so opening another page
	// does not produce a separate login event.
	trustedMu   sync.Mutex
	trustedSeen map[string]time.Time
}

// LoadToken reads the token file or creates it. created reports first creation
// so the caller can display the initial token once.
func LoadToken(path string) (token string, created bool, err error) {
	if raw, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(raw)); t != "" {
			return t, false, nil
		}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", false, err
	}
	token = hex.EncodeToString(b)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", false, err
	}
	return token, true, nil
}

// Handler builds the API routes and applies the peer-address guard to all requests.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /api/diagnostics/check", s.auth(s.diagnosticCheck))
	mux.Handle("GET /api/diagnostics/bundle", s.auth(s.supportBundle))
	mux.Handle("GET /api/catalog", s.auth(s.catalog))
	mux.Handle("GET /api/features", s.auth(s.features))
	mux.Handle("POST /api/presets/preview", s.auth(s.presetPreview))
	mux.Handle("POST /api/filters/refresh", s.auth(s.refreshFilters))
	mux.Handle("POST /api/dns/check", s.auth(s.checkDNS))
	mux.Handle("GET /api/geoip", s.auth(s.geoDatabases))
	mux.Handle("POST /api/geoip/update", s.auth(s.updateGeo))
	mux.Handle("POST /api/geoip/lookup", s.auth(s.lookupGeo))
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/session", s.session)
	mux.Handle("GET /api/domains", s.auth(s.domainStatus))
	mux.Handle("POST /api/domains/resolve", s.auth(s.resolveDomains))
	mux.Handle("GET /api/status", s.auth(s.status))
	mux.Handle("GET /api/config", s.auth(s.getConfig))
	mux.Handle("GET /api/config/yaml", s.auth(s.yamlExport))
	mux.Handle("POST /api/config/import", s.auth(s.yamlImport))
	mux.Handle("GET /api/config/previous", s.auth(s.previousConfig))
	mux.Handle("PUT /api/config", s.auth(s.putConfig))
	mux.Handle("POST /api/config/validate", s.auth(s.validateConfig))
	mux.Handle("GET /api/devices", s.auth(s.devices))
	mux.Handle("POST /api/links/parse", s.auth(s.parseLink))
	mux.Handle("GET /api/stats", s.auth(s.stats))
	mux.Handle("GET /api/stats/hosts", s.auth(s.hosts))
	mux.Handle("GET /api/journal/{kind}", s.auth(s.journalPage))
	mux.Handle("GET /api/journal/{kind}/stream", s.auth(s.journalStream))
	mux.Handle("GET /api/journal/{kind}/export", s.auth(s.journalExport))
	s.updateRoutes(mux)
	mux.Handle("/api/", http.NotFoundHandler())
	mux.Handle("/", s.ui())
	return s.lanOnly(mux)
}

// lanOnly accepts private, loopback, and link-local peers based on RemoteAddr.
func (s *Server) lanOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		a, err := netip.ParseAddr(host)
		if err != nil || !(a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast()) {
			http.Error(w, "access restricted to local networks", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tokenOK compares the bearer token or session cookie without trusting URL parameters.
func (s *Server) tokenOK(r *http.Request) bool {
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		if c, err := r.Cookie(cookieName); err == nil {
			got = c.Value
		}
	}
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) == 1
}

// trustedNet finds the configured token-exempt network containing the peer.
// It uses only RemoteAddr; client-controlled forwarding headers are untrusted.
func (s *Server) trustedNet(r *http.Request) (netip.Prefix, bool) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Prefix{}, false
	}
	a = a.Unmap()
	for _, p := range s.D.NoTokenFrom() {
		if p.Contains(a) {
			return p, true
		}
	}
	return netip.Prefix{}, false
}

// auth accepts trusted-network peers or a valid token before calling the endpoint.
func (s *Server) auth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, trusted := s.trustedNet(r); !trusted && !s.tokenOK(r) {
			writeError(w, http.StatusUnauthorized, "token login required")
			return
		}
		h(w, r)
	})
}

// login bounds token input, audits attempts, and issues a 30-day HttpOnly cookie.
// Failed comparisons incur a fixed delay before the unauthorized response.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(req.Token)), []byte(s.Token)) != 1 {
		s.auditLogin(r, "error", "invalid token")
		time.Sleep(500 * time.Millisecond) // Slow repeated token guesses.
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: s.Token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, MaxAge: int((30 * 24 * time.Hour).Seconds()),
	})
	s.auditLogin(r, "ok", "token login")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// session reports authentication requirements so trusted-network clients
// can enter the UI without a login prompt.
func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	p, trusted := s.trustedNet(r)
	out := map[string]any{"authed": trusted || s.tokenOK(r), "trusted": trusted, "ip": clientIP(r)}
	if trusted {
		out["network"] = p.String()
		if !s.tokenOK(r) && r.URL.Query().Get("open") == "1" && s.firstTrustedVisit(clientIP(r)) {
			s.auditLogin(r, "ok", "without token — address in network "+p.String())
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// firstTrustedVisit rate-limits trusted-login audit events per address.
func (s *Server) firstTrustedVisit(ip string) bool {
	s.trustedMu.Lock()
	defer s.trustedMu.Unlock()
	if s.trustedSeen == nil {
		s.trustedSeen = map[string]time.Time{}
	}
	if last, ok := s.trustedSeen[ip]; ok && time.Since(last) < time.Hour {
		return false
	}
	s.trustedSeen[ip] = time.Now()
	return true
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		daemon.Status
		Build buildIdentity `json:"build"`
	}{s.D.Status(r.Context()), runningBuild})
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg := s.D.Config()
	if cfg == nil {
		writeError(w, http.StatusServiceUnavailable, "daemon is still starting")
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// decodeConfig parses a size-bounded JSON configuration and applies model defaults.
func decodeConfig(r *http.Request) (*model.Config, error) {
	var cfg model.Config
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, err
	}
	cfg.Defaults()
	return &cfg, nil
}

// validateConfig checks model and engine profile constraints without applying changes.
func (s *Server) validateConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := decodeConfig(r)
	if err == nil {
		err = cfg.Validate()
	}
	if err == nil {
		err = validateEngineProfiles(r.Context(), cfg)
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// putConfig validates and applies the complete configuration through the daemon.
func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := decodeConfig(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid configuration: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	ctx = daemon.WithActor(ctx, daemon.Actor{Who: "user", From: clientIP(r)})
	if err := s.D.Apply(ctx, cfg); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.D.Config())
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	list := s.D.Devices()
	if list == nil {
		list = []daemon.Device{}
	}
	writeJSON(w, http.StatusOK, list)
}

// stats returns per-connection summaries, including history when series=1.
func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.D.Stats(r.URL.Query().Get("series") == "1"))
}

func (s *Server) hosts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	list, total := s.D.Hosts(q.Get("tag"), q.Get("q"), limit)
	writeJSON(w, http.StatusOK, map[string]any{"hosts": list, "total": total})
}

// parseLink returns connection-link metadata for the creation wizard.
func (s *Server) parseLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Link string `json:"link"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	link := strings.TrimSpace(req.Link)
	if strings.HasPrefix(link, "tt://") {
		ep, err := trusttunnel.ParseLink(link)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"type": model.ConnTrustTunnel, "name": ep.Name, "protocol": "trusttunnel",
			"server": ep.Hostname, "addresses": ep.Addresses, "http3": ep.HTTP3,
		})
		return
	}
	o, err := links.Parse(link)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"type": model.ConnLink, "name": links.Name(link), "protocol": o["type"],
		"server": o["server"], "port": o["server_port"], "tls": o["tls"] != nil, "transport": o["transport"],
	})
}

// ui serves embedded assets and falls back to index.html for client-side routes
// such as /connections. Unknown API paths remain 404 responses.
func (s *Server) ui() http.Handler {
	files := http.FileServerFS(s.UI)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if _, err := fs.Stat(s.UI, p); errors.Is(err, fs.ErrNotExist) {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			}
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
