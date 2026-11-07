package api

import (
	"archive/zip"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"runtime"
	"strings"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/daemon"
	"github.com/ordo-systems/keen-proxy/internal/journal"
)

const bundleReadme = `KeenProxy diagnostic archive, format 1

config.json describes the applied configuration. It cannot be restored.
status.json summarizes runtime health at export time.
journals/ contains all readable retained routing, audit and system records.

Aliases are consistent only within this archive. Credentials, raw profiles
and free-form log messages are excluded. Known event categories replace
message details. Event times, port numbers, policies and counts remain.
Review the archive before sharing, since these details can be identifying.
`

type supportAlias func(kind, value string) string

// archiveAliases uses a fresh key for each download so exported aliases
// cannot be correlated across archives or checked against a list of guesses.
func archiveAliases() supportAlias {
	var key [32]byte
	rand.Read(key[:])
	return func(kind, value string) string {
		if value == "" || (kind == "connection" && (value == "direct" || value == "block")) {
			return value
		}
		if kind == "ip" || kind == "host" {
			if addr, err := netip.ParseAddr(value); err == nil {
				kind, value = "ip", addr.WithZone("").Unmap().String()
			} else if kind == "host" {
				value = strings.ToLower(strings.TrimSuffix(value, "."))
			}
		}
		mac := hmac.New(sha256.New, key[:])
		io.WriteString(mac, kind+"\x00"+value)
		return kind + "-" + hex.EncodeToString(mac.Sum(nil)[:8])
	}
}

func journalClientHost(value string) string {
	if host, _, err := net.SplitHostPort(value); err == nil {
		return host
	}
	return strings.Trim(value, "[]")
}

// supportBundle streams sanitized records through a bounded set of ZIP members.
func (s *Server) supportBundle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.supportBusy.CompareAndSwap(false, true) {
		writeError(w, http.StatusServiceUnavailable, "another diagnostic archive is being generated")
		return
	}
	defer s.supportBusy.Store(false)
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	deadline, _ := ctx.Deadline()
	_ = http.NewResponseController(w).SetWriteDeadline(deadline)

	cfg := s.D.Config()
	if cfg == nil {
		writeError(w, http.StatusServiceUnavailable, "configuration unavailable")
		return
	}
	alias := archiveAliases()
	status := supportStatus(s.D.Status(ctx), alias)
	now := time.Now().UTC().Truncate(time.Second)
	status["archive_format"] = 1
	status["generated_at"] = now
	status["installed_release"] = s.Updates.Status().Current
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="keenproxy-diagnostics-`+now.Format("20060102-150405")+`.zip"`)
	if err := s.writeSupportBundle(ctx, w, alias, supportConfig(cfg, alias), status); err != nil {
		// An incomplete archive must not look like a successful download.
		if s.Log != nil {
			s.Log.Warn("diagnostic archive interrupted", "err", err)
		}
		panic(http.ErrAbortHandler)
	}
}

func (s *Server) writeSupportBundle(ctx context.Context, out io.Writer, alias supportAlias, config, status any) error {
	z := zip.NewWriter(out)
	readme, err := z.Create("README.txt")
	if err != nil {
		return err
	}
	if _, err := io.WriteString(readme, bundleReadme); err != nil {
		return err
	}
	if err := writeBundleJSON(z, "config.json", config); err != nil {
		return err
	}
	if err := writeBundleJSON(z, "status.json", status); err != nil {
		return err
	}
	for _, kind := range journal.Kinds {
		if err := writeSupportJournal(ctx, z, kind, s.D.Journal(kind), alias); err != nil {
			return err
		}
	}
	return z.Close()
}

func writeBundleJSON(z *zip.Writer, name string, value any) error {
	out, err := z.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func writeSupportJournal(ctx context.Context, z *zip.Writer, kind journal.Kind, j *journal.Journal, alias supportAlias) error {
	out, err := z.Create("journals/" + string(kind) + ".jsonl")
	if err != nil || j == nil {
		return err
	}
	enc := json.NewEncoder(out)
	return j.Visit(func(entry journal.Entry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return enc.Encode(supportEntry(kind, entry, alias))
	})
}

// supportStatus copies operational measurements and aliases runtime connection IDs.
func supportStatus(st daemon.Status, alias supportAlias) map[string]any {
	health := make(map[string]any, len(st.Health))
	for id, h := range st.Health {
		health[alias("connection", id)] = map[string]any{
			"ok": h.OK, "checked": h.Checked.UTC().Truncate(time.Second),
			"latency_ms": h.Latency, "error_class": failureClass(h.Error),
		}
	}
	active := make(map[string]string, len(st.Failover))
	for id, f := range st.Failover {
		active[alias("connection", id)] = alias("connection", f.Active)
	}
	return map[string]any{
		"os": runtime.GOOS, "arch": runtime.GOARCH,
		"started": st.Started.UTC().Truncate(time.Second), "mode": known(st.Mode, "redirect", "tproxy"),
		"firewall": st.Firewall, "firewall_restorations": st.Restored, "dns_listening": st.DNS != "",
		"online_devices": st.OnlineDevices, "intercept_sources": st.Sources,
		"country_databases": st.GeoIP.Active, "category_databases": st.GeoIP.Categories,
		"connection_health": health, "active_fallbacks": active,
	}
}
