package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/journal"
	"github.com/ordo-systems/keen-proxy/internal/model"
)

const bundlePath = "/api/diagnostics/bundle"

func TestSupportBundleAuthorization(t *testing.T) {
	s, _ := newServer()
	h := s.Handler()
	if got := do(t, h, "GET", bundlePath, "", "", lan).Code; got != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d", got)
	}
	if got := do(t, h, "GET", bundlePath, "", s.Token, "8.8.8.8:1234").Code; got != http.StatusForbidden {
		t.Errorf("non-LAN status = %d", got)
	}
}

func TestSupportBundleIncludesRetainedHistoryWithoutPrivateValues(t *testing.T) {
	s, d := newServer()
	s.Token = "private-panel-token"
	d.cfg.API.TokenFile = "/private/router/user/token"
	d.cfg.Clients = []model.Client{{IP: "192.168.7.42", MAC: "aa:bb:cc:dd:ee:ff", Name: "private-device-name"}}
	d.cfg.Connections = []model.Connection{{ID: "private-connection", Name: "private-tunnel-name", Type: model.ConnLink, Link: "vless://secret@example.net"}}
	d.cfg.Groups = []model.Group{{Name: "private rule name", Target: "private-connection", Entries: []string{"private-host.example"}}}
	path := t.TempDir() + "/routes.jsonl"
	d.journals[journal.Routes] = journal.New(journal.Options{File: path, Memory: 1, MaxBytes: 1})
	e := journal.Entry{Time: time.Now().Add(-72 * time.Hour), Client: "192.168.7.42:4040", Host: "private-host.example", IP: "203.0.113.9", DestinationPort: 443, Network: "tcp", Outbound: "private-connection", Rule: "DNS IP fallback: group «private rule name»"}
	d.journals[journal.Routes].Add(e)
	// Reopen with a larger limit so one entry is in the rotated file and one in the active file.
	d.journals[journal.Routes].Close()
	d.journals[journal.Routes] = journal.New(journal.Options{File: path, Memory: 1, MaxBytes: 1 << 20})
	defer d.journals[journal.Routes].Close()
	e.Time = time.Now()
	e.Client = "192.168.7.42:5050"
	e.Rule = "group «private rule name»"
	d.journals[journal.Routes].Add(e)
	d.journals[journal.Routes].Add(journal.Entry{Client: e.Client, Host: e.Host, Network: "dns", Outbound: "block", Rule: "DNS: private-list-name"})
	// System/audit fields can contain arbitrary text from an old configuration.
	d.journals[journal.Audit].Add(journal.Entry{Actor: "user", Action: "Configuration rejected", Result: "error", Target: "config", Details: "retired-password https://private-url.example"})
	d.journals[journal.System].Add(journal.Entry{Source: "sing-box", Level: "WARN", Message: "connect failed: private-host.example: i/o timeout", Count: 4})
	read := func() map[string]string {
		t.Helper()
		w := do(t, s.Handler(), "GET", bundlePath, "", s.Token, lan)
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/zip" || w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
			t.Fatalf("unexpected download: %d %v", w.Code, w.Header())
		}
		z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
		if err != nil {
			t.Fatal(err)
		}
		files := map[string]string{}
		for _, f := range z.File {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
			files[f.Name] = string(raw)
		}
		return files
	}
	archives := []map[string]string{read(), read()}
	var previous string
	for _, files := range archives {
		for _, name := range []string{"README.txt", "config.json", "status.json", "journals/routes.jsonl", "journals/audit.jsonl", "journals/system.jsonl"} {
			if files[name] == "" {
				t.Errorf("missing %s", name)
			}
		}
		for name, data := range files {
			for _, secret := range []string{s.Token, "private", "retired-password", "192.168.7.42", "aa:bb:cc:dd:ee:ff", "203.0.113.9", "vless://", "example.net"} {
				if strings.Contains(data, secret) {
					t.Errorf("%s contains raw value %q", name, secret)
				}
			}
		}
		routeLines := strings.Split(strings.TrimSpace(files["journals/routes.jsonl"]), "\n")
		if len(routeLines) != 3 {
			t.Fatalf("expected rotated and current routes, got %d", len(routeLines))
		}
		var first, second, blocked journal.Entry
		if json.Unmarshal([]byte(routeLines[0]), &first) != nil || json.Unmarshal([]byte(routeLines[1]), &second) != nil ||
			json.Unmarshal([]byte(routeLines[2]), &blocked) != nil {
			t.Fatal("invalid JSONL")
		}
		if blocked.Network != "dns" || blocked.Outbound != "block" || blocked.Rule != "DNS blocked" || blocked.Host != first.Host {
			t.Errorf("DNS blocking context missing: %+v", blocked)
		}
		if first.Rule != "DNS IP fallback: "+second.Rule || !strings.Contains(files["config.json"], second.Rule) {
			t.Errorf("DNS IP fallback context missing: %+v %+v", first, second)
		}
		if first.Client == "" || first.Client != second.Client || first.Outbound == "" || first.Outbound == previous ||
			first.DestinationPort != 443 || first.Network != "tcp" || first.Time.After(time.Now().Add(-48*time.Hour)) ||
			!strings.Contains(files["config.json"], first.Outbound) {
			t.Errorf("routing correlation or anonymization failed: %+v %+v", first, second)
		}
		previous = first.Outbound
		if !strings.Contains(files["journals/audit.jsonl"], "Configuration rejected") ||
			!strings.Contains(files["journals/system.jsonl"], "timeout") ||
			!strings.Contains(files["journals/system.jsonl"], `"count":4`) {
			t.Errorf("diagnostic event classes missing")
		}
	}
}
