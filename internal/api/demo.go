package api

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/compile"
	"github.com/ordo-systems/keen-proxy/internal/daemon"
	"github.com/ordo-systems/keen-proxy/internal/domaincache"
	"github.com/ordo-systems/keen-proxy/internal/geoip"
	"github.com/ordo-systems/keen-proxy/internal/journal"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/ordo-systems/keen-proxy/internal/stats"
)

// Demo implements the daemon API for offline UI development (-ui-demo).
// Configuration is validated and stored in memory without applying network changes.
type Demo struct {
	domainCache *domaincache.Cache
	mu          sync.Mutex
	cfg         *model.Config
	started     time.Time
	journals    journal.Set
	geoFetched  map[string]bool // databases "downloaded" by a demo update
}

// NewDemo seeds in-memory configuration, DNS observations, and synthetic journal activity.
func NewDemo(cfg *model.Config) *Demo {
	d := &Demo{domainCache: domaincache.New(time.Now), cfg: cfg, started: time.Now()}
	demoResolve(d.domainCache, compile.DomainNames(cfg))
	d.startJournals(demoFirewallJSON)
	return d
}

// Config returns a detached copy for callers to edit safely.
func (d *Demo) Config() *model.Config {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cfg.Clone()
}

// Apply validates and retains the supplied configuration in memory without network effects.
func (d *Demo) Apply(_ context.Context, cfg *model.Config) error {
	cfg.Defaults()
	if err := cfg.Validate(); err != nil {
		d.journals[journal.Audit].Add(journal.Entry{Actor: "user", Action: "Configuration rejected", Target: "config", Result: "error", Details: err.Error()})
		return err
	}
	d.mu.Lock()
	d.cfg = cfg
	demoResolve(d.domainCache, compile.DomainNames(cfg))
	d.mu.Unlock()
	d.journals[journal.Audit].Add(journal.Entry{Actor: "user", Action: "Apply configuration", Target: "config", Result: "ok", Details: "demo: configuration saved in memory"})
	return nil
}

// NoTokenFrom returns the trusted-network policy of the current demo configuration.
func (d *Demo) NoTokenFrom() []netip.Prefix {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cfg.API.NoTokenPrefixes()
}

// Status synthesizes runtime observations for the configured connections.
func (d *Demo) Status(context.Context) daemon.Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := daemon.Status{
		Started: d.started, Mode: string(d.cfg.Intercept.Mode), Connections: len(d.cfg.Connections),
		Groups: len(d.cfg.Groups), Clients: len(d.cfg.Clients), OnlineDevices: len(d.cfg.Clients) + 3,
		DNS: d.cfg.DNS.Listen, Firewall: true,
	}
	for _, g := range d.cfg.Groups {
		st.Entries += len(g.Entries)
	}
	st.GeoIP = daemon.GeoInfo{}
	for _, db := range d.cfg.GeoIP.Databases {
		if db.Enabled && db.IsCategory() {
			st.GeoIP.Categories++
		} else if db.Enabled {
			st.GeoIP.Active++
			if b, ok := geoip.FindBuiltin(db.ID); ok && b.Attribution != "" {
				st.GeoIP.Attributions = append(st.GeoIP.Attributions, daemon.GeoAttribution{Text: b.Attribution, URL: b.AttributionURL})
			}
		}
	}
	if st.GeoIP.Active > 0 {
		st.GeoIP.Loaded, st.GeoIP.Built, st.GeoIP.Type = true, time.Now().Format("2006-01")+"-01", "Synthetic demo"
	} else {
		st.GeoIP.Error = "no enabled country databases"
	}
	st.ConnCountries = map[string]string{}
	st.Health = map[string]daemon.ConnHealth{}
	demoCountries := []string{"FI", "NL", "DE", "SE", "EE", "LV"}
	for i, c := range d.cfg.Connections {
		st.ConnCountries[c.ID] = demoCountries[i%len(demoCountries)]
		st.Health[c.ID] = daemon.ConnHealth{OK: true, Checked: time.Now().Add(-4 * time.Second), Since: d.started, Latency: int64(60 + 23*i)}
		if c.Type == model.ConnTrustTunnel {
			st.TrustTunnel = append(st.TrustTunnel, daemon.TrustTunnelInfo{ID: c.ID, Running: true, Starts: 1})
		}
	}
	// Simulate failure of the first connection and traffic through its fallback.
	for i, c := range d.cfg.Connections {
		if c.Fallback == "" {
			continue
		}
		if st.Failover == nil {
			st.Failover = map[string]daemon.FailoverInfo{}
		}
		chain := d.cfg.Chain(c.ID)
		active := c.ID
		if i == 0 {
			active = chain[1]
		}
		st.Failover[c.ID] = daemon.FailoverInfo{Active: active, Chain: chain}
		for j, m := range chain {
			if m == model.TargetDirect {
				continue
			}
			h := daemon.ConnHealth{OK: true, Checked: time.Now().Add(-4 * time.Second), Since: d.started, Latency: int64(60 + 23*j)}
			if m == d.cfg.Connections[0].ID {
				h = daemon.ConnHealth{Checked: h.Checked, Since: time.Now().Add(-3 * time.Minute), Error: "dial tcp 203.0.113.10:51820: i/o timeout"}
			}
			st.Health[m] = h
		}
	}
	return st
}

// Devices returns synthetic LAN neighbors for the client view.
func (d *Demo) Devices() []daemon.Device {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []daemon.Device{
		{MAC: "a4:83:e7:10:20:30", IP: "192.168.7.21", Hostname: "demo-printer", HostnameSource: "dhcp"},
		{MAC: "f0:18:98:44:55:66", IP: "192.168.7.35", Hostname: "demo-nas.local", HostnameSource: "dns"},
		{MAC: "3c:22:fb:77:88:99", IP: "192.168.7.52"},
	}
	for i, c := range d.cfg.Clients {
		ip := c.IP
		if ip == "" {
			ip = "192.168.7." + string(rune('1'+i%9)) + "0"
		}
		out = append(out, daemon.Device{MAC: c.MAC, IP: ip, Client: c.Name, Mode: c.Mode})
	}
	return out
}

// demoHosts supplies synthetic statistics with group domains routed to the
// selected connection and a few unrelated hosts.
func (d *Demo) demoHosts(tag string) []string {
	var out []string
	for _, g := range d.cfg.Groups {
		if g.Target == tag {
			for _, e := range g.Entries {
				out = append(out, strings.TrimPrefix(e, "*."), "www."+strings.TrimPrefix(e, "*."))
			}
		}
	}
	if tag == "direct" {
		out = append(out, "ya.ru", "mail.ru", "vk.com", "gosuslugi.ru", "ozon.ru", "wb.ru", "avito.ru")
	}
	if len(out) == 0 {
		out = []string{"api.example.com", "cdn.example.net"}
	}
	return out
}

func seed(s string) float64 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return float64(h.Sum32()%1000) / 1000
}

// Stats generates synthetic waves at different frequencies per connection
// for inspecting the UI without a router.
func (d *Demo) Stats(withSeries bool) stats.Snapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	tags := []string{"direct"}
	for _, c := range d.cfg.Connections {
		tags = append(tags, c.ID)
	}
	var snap stats.Snapshot
	for _, tag := range tags {
		k := seed(tag)
		hosts := d.demoHosts(tag)
		o := stats.Outbound{Tag: tag}
		for i := 899; i >= 0; i-- {
			t := now.Add(-time.Duration(i) * stats.SampleInterval)
			x := float64(t.Unix()) / 60
			wave := 0.5 + 0.5*math.Sin(x*(0.7+k)+k*10)
			burst := math.Max(0, math.Sin(x*(3.1+k*2)))
			p := stats.Point{
				T:      t.UnixMilli(),
				Unique: int(float64(len(hosts))*(0.3+0.7*wave)) + int(burst*4),
				Down:   (2e6 + 18e6*wave*wave + 30e6*math.Pow(burst, 8)) * (0.5 + k),
				Up:     (0.3e6 + 1.5e6*wave) * (0.5 + k),
			}
			o.PeakDown = math.Max(o.PeakDown, p.Down)
			o.PeakUniq = max(o.PeakUniq, p.Unique)
			o.DownTotal += int64(p.Down / 8 * stats.SampleInterval.Seconds())
			o.UpTotal += int64(p.Up / 8 * stats.SampleInterval.Seconds())
			if withSeries {
				o.Series = append(o.Series, p)
			}
			if i == 0 {
				o.Unique, o.Down, o.Up, o.Active = p.Unique, p.Down, p.Up, p.Unique*3
			}
		}
		o.Top, _ = d.hostsLocked(tag, "", 5, true)
		snap.Outbounds = append(snap.Outbounds, o)
	}
	x := float64(now.Unix()) / 30
	snap.System = stats.System{CPU: math.Round(8 + 6*math.Sin(x)), MemPct: math.Round(41 + 3*math.Sin(x/3)), MemMB: 991}
	processCPU := math.Round((2.4+math.Sin(x))*10) / 10
	snap.System.Process = &stats.ProcessUsage{CPU: &processCPU, RSSBytes: uint64(38+2*math.Sin(x/3)) << 20}
	return snap
}

// Hosts generates filtered per-host traffic for the selected demo outbound.
func (d *Demo) Hosts(tag, query string, limit int) ([]stats.Host, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.hostsLocked(tag, query, limit, false)
}

// hostsLocked filters and ranks synthetic hosts, returning the match count before limiting.
// The caller holds d.mu while configuration-derived host names are read.
func (d *Demo) hostsLocked(tag, query string, limit int, byRequests bool) ([]stats.Host, int) {
	names := d.demoHosts(tag)
	now := time.Now().UnixMilli()
	var out []stats.Host
	for i, n := range names {
		if query != "" && !strings.Contains(n, strings.ToLower(query)) {
			continue
		}
		k := seed(n)
		out = append(out, stats.Host{
			Host: n, Down: int64(k * 4e9 / float64(i+1)), Up: int64(k * 6e7 / float64(i+1)),
			Requests: int64(k*900) + 3, LastSeen: now - int64(k*36e5*float64(i)),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if byRequests {
			return out[i].Requests > out[j].Requests
		}
		return out[i].Down > out[j].Down
	})
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total
}

// demoGeo is a synthetic address table; the demo never downloads databases.
var demoGeo = map[string]string{"77.88.0.0/16": "RU", "8.8.8.0/24": "US", "1.1.1.0/24": "AU", "185.199.108.0/22": "NL", "2a02:6b8::/32": "RU"}

// demoCategories labels the same synthetic ranges for category databases.
var demoCategories = map[string]string{"77.88.0.0/16": "RU-WHITELIST", "8.8.8.0/24": "GOOGLE", "1.1.1.0/24": "CLOUDFLARE", "185.199.108.0/22": "RU-BLOCKED"}

func demoLabel(table map[string]string, ip netip.Addr) string {
	for cidr, c := range table {
		if netip.MustParsePrefix(cidr).Contains(ip.Unmap()) {
			return c
		}
	}
	return ""
}

func demoKind(db model.GeoIPDatabase) string {
	if db.IsCategory() {
		return geoip.KindCategory
	}
	return geoip.KindCountry
}

// GeoDatabases reports configured databases as loaded synthetic data.
func (d *Demo) GeoDatabases() []daemon.GeoDatabaseInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []daemon.GeoDatabaseInfo{}
	for _, db := range d.cfg.GeoIP.Databases {
		info := daemon.GeoDatabaseInfo{ID: db.ID, Name: db.DisplayName(), Builtin: db.IsBuiltin(), Kind: demoKind(db), Enabled: db.Enabled, Local: db.File != "", Loaded: db.Enabled}
		if db.Enabled || d.geoFetched[db.ID] {
			info.Present, info.Built, info.Type, info.Version = true, time.Now().Format("2006-01")+"-01", "Synthetic demo", "demo"
			info.Checked, info.Updated = d.started, d.started
		}
		out = append(out, info)
	}
	return out
}

// UpdateGeo marks databases as downloaded without network access.
func (d *Demo) UpdateGeo(_ context.Context, id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.geoFetched == nil {
		d.geoFetched = map[string]bool{}
	}
	for _, db := range d.cfg.GeoIP.Databases {
		if db.ID == id || id == "" && db.Enabled {
			d.geoFetched[db.ID] = true
			if id != "" {
				return nil
			}
		}
	}
	if id != "" {
		return fmt.Errorf("unknown database %q", id)
	}
	return nil
}

// LookupGeo answers from the synthetic table for every selected database.
func (d *Demo) LookupGeo(ip netip.Addr, id string) (daemon.GeoLookup, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := daemon.GeoLookup{IP: ip.Unmap().String(), Results: []daemon.GeoLookupResult{}}
	for _, db := range d.cfg.GeoIP.Databases {
		if id == "" && !db.Enabled || id != "" && db.ID != id {
			continue
		}
		r := daemon.GeoLookupResult{ID: db.ID, Name: db.DisplayName(), Enabled: db.Enabled, Kind: demoKind(db), Country: demoLabel(demoGeo, ip)}
		if db.IsCategory() {
			r.Country = demoLabel(demoCategories, ip)
		}
		if !db.Enabled && !d.geoFetched[db.ID] {
			r.Country, r.Error = "", "database not downloaded yet"
		}
		switch {
		case r.Country == "":
		case db.IsCategory():
			if !slices.Contains(out.Categories, r.Country) {
				out.Categories = append(out.Categories, r.Country)
			}
		case out.Country == "":
			out.Country, out.Database = r.Country, db.ID
		}
		out.Results = append(out.Results, r)
	}
	if id != "" && len(out.Results) == 0 {
		return out, fmt.Errorf("unknown database %q", id)
	}
	return out, nil
}
