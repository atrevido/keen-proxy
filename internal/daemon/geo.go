package daemon

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"
	"time"

	"github.com/atrevido/keenetic-proxy/internal/compile"
	"github.com/atrevido/keenetic-proxy/internal/geoip"
	"github.com/atrevido/keenetic-proxy/internal/model"
)

// GeoInfo reports country database status for monitoring and diagnostics.
type GeoInfo struct {
	Loaded bool   `json:"loaded"`
	Built  string `json:"built,omitempty"` // database build date, YYYY-MM-DD
	Type   string `json:"type,omitempty"`
	Error  string `json:"error,omitempty"`
}

// geoState is owned by Daemon and protected by d.mu.
type geoState struct {
	db       *geoip.DB
	err      string
	sets     map[string][]byte // country code to geoip-<code>.json for the current database
	conns    map[string]string // connection ID to server country code
	resolved map[string]string // cached server hostname to IP mapping
}

func (d *Daemon) geoPath(cfg *model.Config) string {
	if cfg != nil && cfg.GeoIP.File != "" {
		return cfg.GeoIP.File
	}
	return filepath.Join(filepath.Dir(d.opt.RuleSetDir), "geoip", "country.mmdb")
}

// openGeoLocked opens the local database while d.mu is held. Failure is
// nonfatal: country rules temporarily match no addresses.
func (d *Daemon) openGeoLocked(cfg *model.Config) {
	db, err := geoip.Open(d.geoPath(cfg))
	if err != nil {
		d.geo.err = "country database not loaded: " + err.Error()
		return
	}
	if d.geo.db != nil {
		d.geo.db.Close()
	}
	d.geo.db, d.geo.err, d.geo.sets = db, "", nil
	d.log.Info("country database loaded", "built", db.Built.Format("2006-01-02"), "type", db.Type)
}

// writeCountrySetsLocked writes configured geoip-<code>.json files under d.mu.
// Prefixes are cached per database version to avoid repeated full scans.
func (d *Daemon) writeCountrySetsLocked(codes []string) error {
	if d.geo.sets == nil {
		d.geo.sets = map[string][]byte{}
	}
	var missing []string
	for _, c := range codes {
		if _, ok := d.geo.sets[c]; !ok {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		var prefixes map[string][]netip.Prefix
		if d.geo.db != nil {
			p, err := d.geo.db.Prefixes(missing)
			if err != nil {
				return err
			}
			prefixes = p
		}
		for _, c := range missing {
			d.geo.sets[c] = compile.CountryRuleSet(prefixes[c])
		}
	}
	for _, c := range codes {
		path := filepath.Join(d.opt.RuleSetDir, "geoip-"+strings.ToLower(c)+".json")
		if err := writeFile(path, d.geo.sets[c], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// runGeoUpdates checks database freshness daily and downloads updates directly;
// the router's own traffic bypasses interception.
func (d *Daemon) runGeoUpdates(ctx context.Context) {
	client := &http.Client{Timeout: 3 * time.Minute}
	check := func() {
		d.mu.Lock()
		custom := d.cfg != nil && d.cfg.GeoIP.File != ""
		path, url := d.geoPath(d.cfg), ""
		if d.cfg != nil {
			url = d.cfg.GeoIP.URL
		}
		d.mu.Unlock()
		if custom || !geoip.NeedsUpdate(path) {
			d.refreshConnCountries(ctx)
			return
		}
		d.log.Info("updating country database", "path", path)
		if err := geoip.Download(ctx, client, url, path); err != nil {
			d.mu.Lock()
			if d.geo.db == nil {
				d.geo.err = "failed to download country database: " + err.Error()
			}
			d.mu.Unlock()
			d.log.Warn("country database download failed", "err", err)
			d.audit("system", "Country database update failed", "geoip", "warn", err.Error())
			return
		}
		d.mu.Lock()
		d.openGeoLocked(d.cfg)
		if out, err := d.compileLocked(); err != nil {
			d.log.Error("country rule sets after database update", "err", err)
		} else {
			d.log.Info("country rule sets updated", "countries", out.Countries)
		}
		d.audit("system", "Country database updated", "geoip", "ok", d.geoInfoLocked().Built)
		d.mu.Unlock()
		d.refreshConnCountries(ctx)
	}
	check()
	tick := time.NewTicker(24 * time.Hour)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			check()
		}
	}
}

// refreshConnCountries resolves server countries for connection flags,
// performing hostname lookups outside the daemon lock.
func (d *Daemon) refreshConnCountries(ctx context.Context) {
	d.mu.Lock()
	hosts := map[string]string{}
	for k, v := range d.serverHosts {
		hosts[k] = v
	}
	db := d.geo.db
	d.mu.Unlock()
	if db == nil {
		return
	}
	res := map[string]string{}
	for id, host := range hosts {
		addr, err := netip.ParseAddr(host)
		if err != nil {
			lctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			ips, lerr := net.DefaultResolver.LookupNetIP(lctx, "ip4", host)
			cancel()
			if lerr != nil || len(ips) == 0 {
				continue
			}
			addr = ips[0]
		}
		if cc := db.Country(addr); cc != "" {
			res[id] = cc
		}
	}
	d.mu.Lock()
	d.geo.conns = res
	d.mu.Unlock()
}

func (d *Daemon) geoInfoLocked() GeoInfo {
	info := GeoInfo{Error: d.geo.err}
	if d.geo.db != nil {
		info.Loaded, info.Built, info.Type = true, d.geo.db.Built.Format("2006-01-02"), d.geo.db.Type
	}
	return info
}
