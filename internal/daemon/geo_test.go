package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/geoip"
	"github.com/ordo-systems/keen-proxy/internal/geoip/geoiptest"
	"github.com/ordo-systems/keen-proxy/internal/model"
)

func writeGeo(t *testing.T, path string, nets map[string]string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := geoiptest.WriteTestDB(path, nets); err != nil {
		t.Fatal(err)
	}
}

// Lookups follow database priority; a disabled database can be downloaded
// and tested without joining the active set.
func TestGeoLookupAndDisabledDatabase(t *testing.T) {
	d := New(Options{RuleSetDir: filepath.Join(t.TempDir(), "rule-sets")})
	writeGeo(t, d.geoStore.Path("dbip"), map[string]string{"10.0.0.0/24": "RU"})
	// A fresh check keeps the built-in from reaching the network.
	dbip, _ := geoip.FindBuiltin("dbip")
	state, _ := json.Marshal(geoip.State{Source: dbip.Source.Key(), Checked: time.Now()})
	os.WriteFile(filepath.Join(d.geoStore.Dir, "dbip.json"), state, 0o644)
	local := filepath.Join(t.TempDir(), "local.mmdb")
	writeGeo(t, local, map[string]string{"10.0.0.0/22": "FI"})
	remote := filepath.Join(t.TempDir(), "remote.mmdb")
	writeGeo(t, remote, map[string]string{"10.0.0.0/8": "DE"})
	raw, _ := os.ReadFile(remote)
	hits := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; w.Write(raw) }))
	defer srv.Close()
	d.geoStore.Client = srv.Client()

	cfg := &model.Config{GeoIP: model.GeoIP{Databases: []model.GeoIPDatabase{
		{ID: "dbip", Enabled: true},
		{ID: "local", Name: "Local", File: local, Enabled: true},
		{ID: "remote", URL: srv.URL + "/r.mmdb"},
	}}}
	d.mu.Lock()
	d.cfg = cfg
	d.reloadGeoLocked(cfg)
	d.mu.Unlock()

	res, err := d.LookupGeo(netip.MustParseAddr("10.0.1.1"), "")
	if err != nil || res.Country != "FI" || res.Database != "local" || len(res.Results) != 2 || res.Results[0].Country != "" {
		t.Fatalf("all enabled: %+v %v", res, err)
	}
	if res, _ := d.LookupGeo(netip.MustParseAddr("10.0.0.1"), ""); res.Country != "RU" || res.Database != "dbip" {
		t.Fatalf("priority: %+v", res)
	}
	if res, _ := d.LookupGeo(netip.MustParseAddr("10.0.1.1"), "remote"); !strings.Contains(res.Results[0].Error, "not downloaded") {
		t.Fatalf("before download: %+v", res)
	}
	p, err := d.geo.set.Prefixes([]string{"FI"})
	if err != nil || len(p["FI"]) != 2 || p["FI"][0].String() != "10.0.1.0/24" || p["FI"][1].String() != "10.0.2.0/23" {
		t.Fatalf("country sets: %v %v", p, err)
	}

	// Automatic checks skip disabled databases.
	if err := d.updateGeo(context.Background(), "", false); err != nil || hits != 0 {
		t.Fatalf("automatic: hits=%d err=%v", hits, err)
	}
	if err := d.UpdateGeo(context.Background(), "remote"); err != nil || hits != 1 {
		t.Fatalf("manual download: hits=%d err=%v", hits, err)
	}
	if res, _ := d.LookupGeo(netip.MustParseAddr("10.0.1.1"), "remote"); res.Country != "DE" || res.Results[0].Enabled {
		t.Fatalf("disabled database: %+v", res)
	}
	if d.geo.set.Len() != 2 {
		t.Fatalf("disabled database must not affect routing")
	}
	infos := d.GeoDatabases()
	if len(infos) != 3 || !infos[2].Present || infos[2].Loaded || !infos[2].Next.IsZero() || infos[1].Name != "Local" || !infos[1].Local {
		t.Fatalf("state: %+v", infos)
	}
	if err := d.UpdateGeo(context.Background(), "nope"); err == nil {
		t.Fatal("unknown database")
	}
	if _, err := d.LookupGeo(netip.MustParseAddr("10.0.1.1"), "nope"); err == nil {
		t.Fatal("unknown database in lookup")
	}

	// Removing a custom database deletes its download on the next check.
	d.mu.Lock()
	d.cfg = &model.Config{GeoIP: model.GeoIP{Databases: cfg.GeoIP.Databases[:2]}}
	d.mu.Unlock()
	d.updateGeo(context.Background(), "", false)
	if _, err := os.Stat(d.geoStore.Path("remote")); err == nil {
		t.Fatal("file of removed database remains")
	}
	if _, err := os.Stat(d.geoStore.Path("dbip")); err != nil {
		t.Fatal("built-in database file was removed")
	}
	info := d.geoInfoLocked()
	if !info.Loaded || info.Active != 2 || len(info.Attributions) != 1 {
		t.Fatalf("summary: %+v", info)
	}
}

// Category databases never answer country lookups; their labels are unioned
// into geocat-<label>.json and reported separately by test lookups.
func TestGeoCategoryDatabases(t *testing.T) {
	d := New(Options{RuleSetDir: filepath.Join(t.TempDir(), "rule-sets")})
	country := filepath.Join(t.TempDir(), "country.mmdb")
	writeGeo(t, country, map[string]string{"10.0.0.0/16": "RU"})
	blocked := filepath.Join(t.TempDir(), "blocked.mmdb")
	writeGeo(t, blocked, map[string]string{"10.0.1.0/24": "RU-BLOCKED"})
	extra := filepath.Join(t.TempDir(), "extra.mmdb")
	writeGeo(t, extra, map[string]string{"10.0.2.0/24": "RU-BLOCKED", "10.0.1.0/24": "TELEGRAM"})
	cfg := &model.Config{GeoIP: model.GeoIP{Databases: []model.GeoIPDatabase{
		{ID: "cat1", File: blocked, Kind: model.GeoIPKindCategory, Enabled: true},
		{ID: "local", File: country, Enabled: true},
		{ID: "cat2", File: extra, Kind: model.GeoIPKindCategory, Enabled: true},
	}}}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cfg = cfg
	d.reloadGeoLocked(cfg)
	if cc, id := d.geo.set.Country(netip.MustParseAddr("10.0.1.1")); cc != "RU" || id != "local" {
		t.Fatalf("country: %s %s", cc, id)
	}
	os.MkdirAll(d.opt.RuleSetDir, 0o755)
	if err := d.writeCategorySetsLocked([]string{"RU-BLOCKED"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(d.opt.RuleSetDir, "geocat-ru-blocked.json"))
	if err != nil || !strings.Contains(string(raw), "10.0.1.0/24") || !strings.Contains(string(raw), "10.0.2.0/24") {
		t.Fatalf("category set: %s %v", raw, err)
	}
	d.mu.Unlock()
	res, err := d.LookupGeo(netip.MustParseAddr("10.0.1.1"), "")
	d.mu.Lock()
	if err != nil || res.Country != "RU" || res.Database != "local" || strings.Join(res.Categories, ",") != "RU-BLOCKED,TELEGRAM" || res.Results[0].Kind != "category" {
		t.Fatalf("lookup: %+v %v", res, err)
	}
	if info := d.geoInfoLocked(); info.Active != 1 || info.Categories != 2 {
		t.Fatalf("summary: %+v", info)
	}
}

// An upgraded router keeps the single-database layout's file until the first
// download, and local entries keep managed files they point at.
func TestGeoLegacyFileAndPrune(t *testing.T) {
	d := New(Options{RuleSetDir: filepath.Join(t.TempDir(), "rule-sets")})
	legacy := filepath.Join(d.geoStore.Dir, "country.mmdb")
	writeGeo(t, legacy, map[string]string{"10.0.0.0/8": "RU"})
	cfg := &model.Config{GeoIP: model.GeoIP{Databases: []model.GeoIPDatabase{
		{ID: "local", File: filepath.Join(t.TempDir(), "missing.mmdb"), Enabled: true},
		{ID: "cat", URL: "https://geo.example/cat.mmdb", Kind: model.GeoIPKindCategory, Enabled: true},
		{ID: "dbip", Enabled: true},
	}}}
	d.mu.Lock()
	d.adoptLegacyGeo(cfg)
	d.reloadGeoLocked(cfg)
	d.mu.Unlock()
	if cc, id := d.geo.set.Country(netip.MustParseAddr("10.1.1.1")); cc != "RU" || id != "dbip" {
		t.Fatalf("adopted database: %q %q", cc, id)
	}
	if _, err := os.Stat(legacy); err == nil {
		t.Fatal("legacy file remains")
	}

	// A local entry points at a file downloaded under a removed ID.
	writeGeo(t, d.geoStore.Path("old"), map[string]string{"10.0.0.0/8": "FI"})
	os.WriteFile(filepath.Join(d.geoStore.Dir, "old.json"), []byte("{}"), 0o600)
	d.pruneGeo([]model.GeoIPDatabase{{ID: "mine", File: d.geoStore.Path("old"), Enabled: true}})
	if _, err := os.Stat(d.geoStore.Path("old")); err != nil {
		t.Fatal("referenced managed file was pruned")
	}
}

// A download reloads databases enabled by an Apply that ran meanwhile, and
// rule-set failures after an update reach the caller.
func TestGeoUpdateUsesCurrentConfig(t *testing.T) {
	ruleSets := filepath.Join(t.TempDir(), "rule-sets")
	d := New(Options{RuleSetDir: ruleSets})
	remote := filepath.Join(t.TempDir(), "remote.mmdb")
	writeGeo(t, remote, map[string]string{"10.0.0.0/8": "DE"})
	raw, _ := os.ReadFile(remote)
	off, err := model.Parse([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	on := *off
	off.GeoIP.Databases = []model.GeoIPDatabase{{ID: "remote"}}
	on.GeoIP.Databases = []model.GeoIPDatabase{{ID: "remote", Enabled: true}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		d.cfg = &on
		d.mu.Unlock()
		w.Write(raw)
	}))
	defer srv.Close()
	off.GeoIP.Databases[0].URL = srv.URL + "/r.mmdb"
	on.GeoIP.Databases[0].URL = off.GeoIP.Databases[0].URL
	d.geoStore.Client = srv.Client()
	d.mu.Lock()
	d.cfg = off
	d.mu.Unlock()
	// A file in place of the rule-set directory makes compilation fail.
	os.WriteFile(ruleSets, nil, 0o644)

	err = d.UpdateGeo(context.Background(), "remote")
	if err == nil || !strings.Contains(err.Error(), "routing rule sets") {
		t.Fatalf("want rule-set error, got %v", err)
	}
	if cc, id := d.geo.set.Country(netip.MustParseAddr("10.1.1.1")); cc != "DE" || id != "remote" {
		t.Fatalf("database enabled during download is not loaded: %q %q", cc, id)
	}
}
