package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/compile"
	"github.com/ordo-systems/keen-proxy/internal/geoip"
	"github.com/ordo-systems/keen-proxy/internal/model"
)

// geoCheckEvery is how often the updater looks for databases whose check is due.
const geoCheckEvery = 10 * time.Minute

// GeoInfo summarizes the active country databases for monitoring and diagnostics.
type GeoInfo struct {
	Loaded bool   `json:"loaded"`
	Built  string `json:"built,omitempty"` // primary database build date, YYYY-MM-DD
	Type   string `json:"type,omitempty"`  // primary database type
	Error  string `json:"error,omitempty"`
	// Active counts the enabled country databases that answer lookups;
	// Categories counts the enabled category databases in use.
	Active     int `json:"active"`
	Categories int `json:"categories"`
	// Attributions lists credits required by the active databases' licenses.
	Attributions []GeoAttribution `json:"attributions,omitempty"`
}

// GeoAttribution is a license credit the UI displays while a database is in use.
type GeoAttribution struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

// GeoDatabaseInfo reports one configured database for the management page.
type GeoDatabaseInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Builtin bool   `json:"builtin"`
	Kind    string `json:"kind"` // country or category
	Enabled bool   `json:"enabled"`
	Local   bool   `json:"local"`  // custom file that is never downloaded
	Loaded  bool   `json:"loaded"` // answers lookups now
	Present bool   `json:"present"`
	Built   string `json:"built,omitempty"`
	Type    string `json:"type,omitempty"`
	Version string `json:"version,omitempty"`
	// Checked and Updated are the latest download attempt and installation.
	Checked time.Time `json:"checked,omitzero"`
	Updated time.Time `json:"updated,omitzero"`
	// Next is the next automatic check of an enabled downloadable database.
	Next     time.Time `json:"next,omitzero"`
	Failures int       `json:"failures,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// GeoLookup answers a test lookup of one address.
type GeoLookup struct {
	IP string `json:"ip"`
	// Country and Database are the effective answer: the first database in
	// Results that knows the address.
	Country  string `json:"country,omitempty"`
	Database string `json:"database,omitempty"`
	// Categories are the labels of the address in the category databases.
	Categories []string          `json:"categories,omitempty"`
	Results    []GeoLookupResult `json:"results"`
}

// GeoLookupResult is one database's answer for a test lookup.
type GeoLookupResult struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Kind    string `json:"kind"`
	// Country holds the database answer: an ISO code, or a label for categories.
	Country string `json:"country,omitempty"`
	Error   string `json:"error,omitempty"`
}

// geoState is owned by Daemon and protected by d.mu.
type geoState struct {
	set      *geoip.Set        // enabled country databases that opened, in priority order
	cats     *geoip.Set        // enabled category databases that opened
	errs     map[string]string // database ID to load error of the latest reload
	sets     map[string][]byte // country code to geoip-<code>.json for the current set
	catSets  map[string][]byte // category label to geocat-<label>.json for the current cats
	conns    map[string]string // connection ID to server country code
	resolved map[string]string // cached server hostname to IP mapping
}

// geoSource returns the download source of a database, or nil for local files.
func geoSource(db model.GeoIPDatabase) geoip.Source {
	if b, ok := geoip.FindBuiltin(db.ID); ok {
		return b.Source
	}
	if db.URL != "" {
		return geoip.URLSource{URL: db.URL}
	}
	return nil
}

// geoOpenError explains why a database file could not be opened.
func geoOpenError(db model.GeoIPDatabase, err error) string {
	if geoSource(db) != nil && errors.Is(err, fs.ErrNotExist) {
		return "database not downloaded yet"
	}
	return "database failed to open: " + err.Error()
}

func (d *Daemon) geoFile(db model.GeoIPDatabase) string {
	if db.File != "" {
		return db.File
	}
	return d.geoStore.Path(db.ID)
}

// reloadGeoLocked opens the enabled databases while d.mu is held. Databases
// that fail to open are skipped; with none open, country rules match nothing.
func (d *Daemon) reloadGeoLocked(cfg *model.Config) {
	var members, cats []geoip.Member
	errs := map[string]string{}
	for _, db := range cfg.GeoIP.Databases {
		if !db.Enabled {
			continue
		}
		g, err := geoip.Open(d.geoFile(db))
		if err != nil {
			errs[db.ID] = geoOpenError(db, err)
			continue
		}
		if db.IsCategory() {
			cats = append(cats, geoip.Member{ID: db.ID, DB: g})
		} else {
			members = append(members, geoip.Member{ID: db.ID, DB: g})
		}
		d.log.Info("IP database loaded", "id", db.ID, "built", g.Built.Format("2006-01-02"), "type", g.Type)
	}
	d.geo.set.Close()
	d.geo.cats.Close()
	d.geo.set, d.geo.errs, d.geo.sets = geoip.NewSet(members...), errs, nil
	d.geo.cats, d.geo.catSets = geoip.NewSet(cats...), nil
}

// writeCountrySetsLocked writes configured geoip-<code>.json files under d.mu.
// Prefixes are cached per database set to avoid repeated full scans.
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
		prefixes, err := d.geo.set.Prefixes(missing)
		if err != nil {
			return err
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

// writeCategorySetsLocked writes configured geocat-<label>.json files under d.mu,
// merging all enabled category databases.
func (d *Daemon) writeCategorySetsLocked(labels []string) error {
	if d.geo.catSets == nil {
		d.geo.catSets = map[string][]byte{}
	}
	var missing []string
	for _, l := range labels {
		if _, ok := d.geo.catSets[l]; !ok {
			missing = append(missing, l)
		}
	}
	if len(missing) > 0 {
		prefixes, err := d.geo.cats.Union(missing)
		if err != nil {
			return err
		}
		for _, l := range missing {
			d.geo.catSets[l] = compile.CountryRuleSet(prefixes[l])
		}
	}
	for _, l := range labels {
		path := filepath.Join(d.opt.RuleSetDir, compile.CategoryTagPrefix+strings.ToLower(l)+".json")
		if err := writeFile(path, d.geo.catSets[l], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// wakeGeo asks the updater to check databases now, for example after Apply.
func (d *Daemon) wakeGeo() {
	select {
	case d.geoWake <- struct{}{}:
	default:
	}
}

// runGeoUpdates downloads due databases directly (the router's own traffic
// bypasses interception). Checks run every geoCheckEvery so failed downloads
// are retried with backoff instead of waiting a day.
func (d *Daemon) runGeoUpdates(ctx context.Context) {
	tick := time.NewTicker(geoCheckEvery)
	defer tick.Stop()
	for {
		if err := d.updateGeo(ctx, "", false); err != nil && !errors.Is(err, geoip.ErrBusy) {
			d.log.Warn("IP database update failed", "err", err)
		}
		d.refreshConnCountries(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-d.geoWake:
		}
	}
}

// UpdateGeo downloads databases now, ignoring check intervals and backoff.
// An empty id updates every enabled database; a specific id may be disabled,
// so it can be tested before enabling it.
func (d *Daemon) UpdateGeo(ctx context.Context, id string) error {
	return d.updateGeo(ctx, id, true)
}

func (d *Daemon) updateGeo(ctx context.Context, id string, manual bool) error {
	if !d.geoStore.TryLock() {
		return geoip.ErrBusy
	}
	defer d.geoStore.Unlock()
	d.mu.Lock()
	var dbs []model.GeoIPDatabase
	if d.cfg != nil {
		dbs = append(dbs, d.cfg.GeoIP.Databases...)
	}
	d.mu.Unlock()
	d.pruneGeo(dbs)

	var errs []error
	var installed []string // downloaded files that may now need loading
	reload, found := false, id == ""
	for _, db := range dbs {
		if id != "" && db.ID != id {
			continue
		}
		found = true
		src := geoSource(db)
		if src == nil {
			// Local files are never downloaded; a manual update rereads them.
			reload = reload || manual && db.Enabled
			continue
		}
		if id == "" && !db.Enabled || !manual && !d.geoStore.Due(db.ID, src) {
			continue
		}
		d.log.Info("checking IP database", "id", db.ID)
		changed, err := d.geoStore.Update(ctx, db.ID, src)
		name := db.DisplayName()
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			d.log.Warn("IP database download failed", "id", db.ID, "err", err)
			// Automatic retries audit only the first failure of a series.
			if manual || d.geoStore.State(db.ID).Failures == 1 {
				d.audit("system", "IP database update failed", "geoip", "warn", name+": "+err.Error())
			}
		case changed:
			d.audit("system", "IP database updated", "geoip", "ok", name)
		}
		// A file can be installed even when saving its state fails.
		if changed {
			installed = append(installed, db.ID)
		}
	}
	if !found {
		return fmt.Errorf("unknown database %q", id)
	}
	d.mu.Lock()
	// Check enablement in the current configuration: Apply may have enabled a
	// database while its download was running.
	if d.cfg != nil && (reload || geoEnabled(d.cfg, installed)) {
		d.reloadGeoLocked(d.cfg)
		if out, err := d.compileLocked(); err != nil {
			errs = append(errs, fmt.Errorf("routing rule sets: %w", err))
			d.log.Error("IP rule sets after database update", "err", err)
		} else {
			d.log.Info("IP rule sets updated", "countries", out.Countries)
		}
	}
	d.mu.Unlock()
	return errors.Join(errs...)
}

// geoEnabled reports whether any of ids names an enabled database in cfg.
func geoEnabled(cfg *model.Config, ids []string) bool {
	for _, db := range cfg.GeoIP.Databases {
		if db.Enabled && slices.Contains(ids, db.ID) {
			return true
		}
	}
	return false
}

func (d *Daemon) legacyGeoPath() string { return filepath.Join(d.geoStore.Dir, "country.mmdb") }

// adoptLegacyGeo moves the single-database layout's country.mmdb to the
// first enabled downloadable country database that has no file yet, so an
// upgraded router keeps country routing until the first download succeeds.
// Without state the adopted file is still checked for updates right away.
// A local database that points at the file keeps it.
func (d *Daemon) adoptLegacyGeo(cfg *model.Config) {
	legacy := d.legacyGeoPath()
	g, err := geoip.Open(legacy)
	if err != nil {
		return
	}
	g.Close()
	for _, db := range cfg.GeoIP.Databases {
		if db.File != "" && filepath.Clean(db.File) == legacy {
			return // a local database still uses the file in place
		}
	}
	for _, db := range cfg.GeoIP.Databases {
		if !db.Enabled || db.IsCategory() || db.File != "" || geoSource(db) == nil {
			continue
		}
		if path := d.geoStore.Path(db.ID); !fileExists(path) {
			if err := os.Rename(legacy, path); err != nil {
				d.log.Warn("IP database migration failed", "id", db.ID, "err", err)
			} else {
				d.log.Info("IP database migrated", "id", db.ID)
			}
		}
		return
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// pruneGeo removes downloads of deleted databases and the single-database
// layout's country.mmdb unless a local database still points at a file.
func (d *Daemon) pruneGeo(dbs []model.GeoIPDatabase) {
	keep := map[string]bool{}
	legacy := d.legacyGeoPath()
	inUse := false
	for _, db := range dbs {
		keep[db.ID] = true
		file := filepath.Clean(db.File)
		inUse = inUse || file == legacy
		// A local entry may point at a file downloaded under another ID.
		if id, ok := strings.CutSuffix(file, ".mmdb"); ok && db.File != "" && filepath.Dir(file) == filepath.Clean(d.geoStore.Dir) {
			keep[filepath.Base(id)] = true
		}
	}
	if len(dbs) == 0 {
		return
	}
	d.geoStore.Prune(keep)
	if !inUse {
		os.Remove(legacy)
	}
}

// GeoDatabases reports every configured database in priority order,
// including disabled ones and their download state.
func (d *Daemon) GeoDatabases() []GeoDatabaseInfo {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []GeoDatabaseInfo{}
	if d.cfg == nil {
		return out
	}
	loaded := d.loadedGeoLocked()
	for _, db := range d.cfg.GeoIP.Databases {
		info := GeoDatabaseInfo{ID: db.ID, Name: db.DisplayName(), Builtin: db.IsBuiltin(), Kind: geoKind(db), Enabled: db.Enabled, Local: geoSource(db) == nil, Error: d.geo.errs[db.ID]}
		if g, ok := loaded[db.ID]; ok {
			info.Loaded, info.Present, info.Built, info.Type = true, true, g.Built.Format("2006-01-02"), g.Type
		} else if g, err := geoip.Open(d.geoFile(db)); err == nil {
			// Disabled databases are not kept open; read their metadata briefly.
			info.Present, info.Built, info.Type = true, g.Built.Format("2006-01-02"), g.Type
			g.Close()
		}
		if src := geoSource(db); src != nil {
			st := d.geoStore.State(db.ID)
			if st.Source == src.Key() {
				info.Version, info.Checked, info.Updated, info.Failures = st.Version, st.Checked, st.Updated, st.Failures
				if st.Error != "" {
					info.Error = st.Error
				}
			}
			if db.Enabled {
				info.Next = st.Next(geoip.Interval(src))
				if st.Source != src.Key() || info.Next.IsZero() {
					info.Next = time.Now()
				}
			}
		}
		out = append(out, info)
	}
	return out
}

// loadedGeoLocked maps IDs of open databases of both kinds to their readers.
func (d *Daemon) loadedGeoLocked() map[string]*geoip.DB {
	loaded := map[string]*geoip.DB{}
	for _, m := range append(append([]geoip.Member{}, d.geo.set.Members()...), d.geo.cats.Members()...) {
		loaded[m.ID] = m.DB
	}
	return loaded
}

func geoKind(db model.GeoIPDatabase) string {
	if db.IsCategory() {
		return geoip.KindCategory
	}
	return geoip.KindCountry
}

// LookupGeo tests an address against the enabled databases, or against one
// database by ID even when it is disabled.
func (d *Daemon) LookupGeo(ip netip.Addr, id string) (GeoLookup, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := GeoLookup{IP: ip.Unmap().String(), Results: []GeoLookupResult{}}
	if d.cfg == nil {
		return out, errors.New("configuration not loaded")
	}
	loaded := d.loadedGeoLocked()
	found := false
	for _, db := range d.cfg.GeoIP.Databases {
		if id == "" && !db.Enabled || id != "" && db.ID != id {
			continue
		}
		found = true
		r := GeoLookupResult{ID: db.ID, Name: db.DisplayName(), Enabled: db.Enabled, Kind: geoKind(db)}
		if g, ok := loaded[db.ID]; ok {
			r.Country = g.Country(ip)
		} else if g, err := geoip.Open(d.geoFile(db)); err == nil {
			// Disabled databases are opened only for the test.
			r.Country = g.Country(ip)
			g.Close()
		} else {
			r.Error = geoOpenError(db, err)
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
	if id != "" && !found {
		return out, fmt.Errorf("unknown database %q", id)
	}
	return out, nil
}

// refreshConnCountries resolves server countries for connection flags. Host
// lookups run without the daemon lock; database lookups hold it so a reload
// cannot close a database in use.
func (d *Daemon) refreshConnCountries(ctx context.Context) {
	d.mu.Lock()
	hosts := map[string]string{}
	for k, v := range d.serverHosts {
		hosts[k] = v
	}
	d.mu.Unlock()
	addrs := map[string]netip.Addr{}
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
		addrs[id] = addr
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	res := map[string]string{}
	for id, addr := range addrs {
		if cc, _ := d.geo.set.Country(addr); cc != "" {
			res[id] = cc
		}
	}
	d.geo.conns = res
}

func (d *Daemon) geoInfoLocked() GeoInfo {
	info := GeoInfo{}
	var errs []string
	if d.cfg != nil {
		for _, db := range d.cfg.GeoIP.Databases {
			if msg := d.geo.errs[db.ID]; msg != "" {
				errs = append(errs, db.DisplayName()+": "+msg)
			}
		}
	}
	members := d.geo.set.Members()
	info.Active, info.Categories = len(members), d.geo.cats.Len()
	if len(members) > 0 {
		info.Loaded, info.Built, info.Type = true, members[0].DB.Built.Format("2006-01-02"), members[0].DB.Type
	} else if len(errs) == 0 {
		errs = append(errs, "no enabled country databases")
	}
	for _, m := range append(append([]geoip.Member{}, members...), d.geo.cats.Members()...) {
		if b, ok := geoip.FindBuiltin(m.ID); ok && b.Attribution != "" {
			info.Attributions = append(info.Attributions, GeoAttribution{Text: b.Attribution, URL: b.AttributionURL})
		}
	}
	info.Error = strings.Join(errs, "; ")
	return info
}
