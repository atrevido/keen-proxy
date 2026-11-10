// Package daemon coordinates configuration, compilation, sing-box, DNS, and
// firewall lifecycle. Its watchdog restores runtime state, while Apply updates
// configuration and restarts only components whose inputs changed.
package daemon

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/catalog"
	"github.com/ordo-systems/keen-proxy/internal/compile"
	"github.com/ordo-systems/keen-proxy/internal/devices"
	"github.com/ordo-systems/keen-proxy/internal/dnsfront"
	"github.com/ordo-systems/keen-proxy/internal/domaincache"
	"github.com/ordo-systems/keen-proxy/internal/engine"
	"github.com/ordo-systems/keen-proxy/internal/filterlist"
	"github.com/ordo-systems/keen-proxy/internal/geoip"
	"github.com/ordo-systems/keen-proxy/internal/journal"
	"github.com/ordo-systems/keen-proxy/internal/lists"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/ordo-systems/keen-proxy/internal/netfilter"
	"github.com/ordo-systems/keen-proxy/internal/stats"
	"github.com/ordo-systems/keen-proxy/internal/supervise"
)

// Options supplies local paths, runtime dependencies, and watchdog settings.
type Options struct {
	ConfigPath    string
	ARPPath       string        // /proc/net/arp
	RuleSetDir    string        // local sing-box client address rule-sets
	AWGBinary     string        // optional local override for the packaged client
	WatchInterval time.Duration // watchdog interval for firewall and neighbor checks
	Firewall      *netfilter.Firewall
	Log           *slog.Logger
	Journals      journal.Set                                                          // nil creates in-memory journals
	SingBoxLog    string                                                               // sing-box log destination (see engine.LogOutput)
	DeviceNames   func(context.Context, map[string]netip.Addr) map[string]devices.Name // optional local name source
}

// Daemon owns runtime components and coordinates serialized configuration changes.
type Daemon struct {
	domainGeneration  atomic.Pointer[domainGeneration]
	filters           *filterlist.Manager
	featureMu         sync.Mutex
	dnsInfo           DNSInfo
	domains           *domaincache.Cache
	domainSlots       chan struct{}
	domainMu          sync.Mutex
	domainError       string
	domainWritten     time.Time
	domainFingerprint []byte
	opt               Options
	log               *slog.Logger

	mu          sync.Mutex // serializes apply and protects runtime state below, except atomic live
	cfg         *model.Config
	eng         engine.Engine
	dns         *dnsfront.Server
	route       netfilter.TProxyRoute
	routeOn     bool
	plan        netfilter.Plan
	planOn      bool
	neighbors   map[string]netip.Addr
	deviceNames map[string]cachedDeviceName
	pendingAWG  *model.Config
	awgPassword string
	awg         map[string]*awgProcess
	tt          []compile.TrustTunnelClient
	ttProcs     map[string]*supervise.Process
	ttStop      context.CancelFunc
	ttWG        sync.WaitGroup
	started     time.Time
	restored    int // number of watchdog firewall restorations
	stats       *stats.Tracker
	geo         geoState
	// geoStore owns downloaded IP databases; geoWake triggers a check.
	geoStore *geoip.Store
	geoWake  chan struct{}
	// serverHosts maps connection IDs to server hosts from the latest compilation.
	serverHosts map[string]string
	journals    journal.Set
	// live publishes the current configuration to route logging without d.mu.
	live atomic.Pointer[model.Config]
	// coreRestarted records whether the latest applyLocked restarted the engine.
	coreRestarted bool
	// Fallback chains, selected members, and probe results from the latest
	// compilation (see failover.go).
	chains  []compile.FailoverChain
	active  map[string]string
	health  map[string]*health
	allDown map[string]bool
}

// New creates a daemon with default paths and in-memory journals when omitted.
// It does not start listeners or install interception rules.
func New(opt Options) *Daemon {
	if opt.AWGBinary == "" {
		opt.AWGBinary = awgBinary
	}
	if opt.DeviceNames == nil {
		opt.DeviceNames = devices.NewNameLookup()
	}
	if opt.WatchInterval == 0 {
		opt.WatchInterval = 30 * time.Second
	}
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	if opt.RuleSetDir == "" {
		opt.RuleSetDir = "/opt/var/lib/keenproxy/rule-sets"
	}
	if opt.Journals == nil {
		opt.Journals = journal.NewSet("")
	}
	d := &Daemon{
		awgPassword: rand.Text(), domains: domaincache.New(time.Now), domainSlots: make(chan struct{}, 4), opt: opt, log: opt.Log, stats: stats.NewTracker(), journals: opt.Journals,
		active: map[string]string{}, health: map[string]*health{}, allDown: map[string]bool{},
	}
	d.filters = filterlist.New(filepath.Join(filepath.Dir(opt.RuleSetDir), "filters"), nil)
	d.geoStore = geoip.NewStore(filepath.Join(filepath.Dir(opt.RuleSetDir), "geoip"))
	d.geoWake = make(chan struct{}, 1)
	d.resetDomainGeneration()
	d.stats.OnRoute = d.onRoute
	d.eng.Tracker = d.stats
	d.eng.LogOutput = opt.SingBoxLog
	return d
}

// Run serves until ctx is canceled. Interception starts last and stops first
// so clients retain access to a DNS resolver during lifecycle transitions.
func (d *Daemon) Run(ctx context.Context) error {
	cfg, err := model.Load(d.opt.ConfigPath)
	if err != nil {
		return err
	}
	catalog.AutoUpdate(cfg)
	go d.stats.Run(ctx)
	d.mu.Lock()
	d.started = time.Now()
	d.adoptLegacyGeo(cfg)
	d.reloadGeoLocked(cfg)
	d.neighbors = d.readNeighbors(cfg)
	cfg, err = d.startConfigLocked(ctx, cfg)
	d.mu.Unlock()
	defer d.shutdown()
	if err != nil {
		d.audit("system", "Start keenProxy", "core", "error", err.Error())
		return err
	}
	d.log.Info("keenProxy started", "mode", cfg.Intercept.Mode, "sources", len(d.plan.Sources), "dns", d.dns.Addr())
	d.audit("system", "Start keenProxy", "core", "ok",
		fmt.Sprintf("interception %s, sources %d, DNS %s", cfg.Intercept.Mode, len(d.plan.Sources), d.dns.Addr()))
	go d.runGeoUpdates(ctx)
	go d.runFailover(ctx)
	go d.runDomains(ctx)
	go d.runFilterUpdates(ctx)
	go d.runDeviceNames(ctx)

	tick := time.NewTicker(d.opt.WatchInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			d.log.Info("stopping keenProxy")
			d.audit("system", "Stop keenProxy", "core", "info", "interception rules removed, network uses direct routing")
			return nil
		case <-tick.C:
			d.watch(ctx)
		}
	}
}

// Apply validates, applies, and persists configuration. Application failures
// trigger rollback attempts; persistence failures may leave changes active
// without saving them.
func (d *Daemon) Apply(ctx context.Context, cfg *model.Config) error {
	who := actorOf(ctx)
	by := who.Who
	from := ""
	if who.From != "" {
		from = " (from " + who.From + ")"
	}
	cfg.Defaults()
	catalog.AutoUpdate(cfg)
	if err := cfg.Validate(); err != nil {
		d.audit(by, "Configuration rejected", "config", "error", err.Error()+from)
		return err
	}
	// Compile first so protocol errors do not disturb the running configuration.
	out, err := compile.Compile(compile.Input{Config: cfg, RuleSetDir: d.opt.RuleSetDir})
	if err != nil {
		d.audit(by, "Configuration rejected", "config", "error", err.Error()+from)
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	prev := d.cfg
	if prev == nil && hasAWG(cfg) {
		return errors.New("daemon is still starting")
	}
	if prev != nil && (hasAWG(prev) || hasAWG(cfg) || d.pendingAWG != nil) {
		return d.applyAWGLocked(cfg, out)
	}
	changes := configChanges(prev, cfg)
	if err := d.applyLocked(ctx, cfg); err != nil {
		d.log.Error("new configuration failed to apply, restoring previous configuration", "err", err)
		details := err.Error() + "; previous configuration restored"
		if prev != nil {
			if rbErr := d.applyLocked(ctx, prev); rbErr != nil {
				d.log.Error("rollback also failed", "err", rbErr)
				details = err.Error() + "; rollback also failed: " + rbErr.Error()
			}
		}
		d.audit(by, "Configuration apply failed", "config", "error", details+from)
		return err
	}
	raw, err := cfg.Marshal()
	if err != nil {
		return err
	}
	if previous, readErr := os.ReadFile(d.opt.ConfigPath); readErr == nil {
		if err := writeFile(d.opt.ConfigPath+".previous", previous, 0o600); err != nil {
			return fmt.Errorf("previous configuration was not saved: %w", err)
		}
	}
	if err := writeFile(d.opt.ConfigPath, raw, 0o600); err != nil {
		d.audit(by, "Configuration applied but not saved", "config", "warn", err.Error()+from)
		return fmt.Errorf("configuration applied but not saved: %w", err)
	}
	d.log.Info("configuration applied and saved")
	core := "core not restarted"
	if d.coreRestarted {
		core = "core restarted"
	}
	d.audit(by, "Apply configuration", "config", "ok", changes+"; "+core+from)
	if prev == nil || !reflect.DeepEqual(prev.GeoIP, cfg.GeoIP) {
		// New or re-enabled databases download without waiting for the next check.
		d.wakeGeo()
	}
	go d.refreshConnCountries(context.Background())
	return nil
}

// applyLocked reconciles runtime state with cfg while d.mu is held.
func (d *Daemon) applyLocked(ctx context.Context, cfg *model.Config) error {
	prev := d.cfg
	d.resetDomainGeneration()
	if prev != nil && !reflect.DeepEqual(prev.GeoIP, cfg.GeoIP) {
		d.reloadGeoLocked(cfg)
	}
	d.cfg = cfg
	d.live.Store(cfg)
	d.coreRestarted = false
	out, err := d.compileLocked()
	if err != nil {
		return err
	}

	prepared, awgErr := d.prepareAWG(out.AmneziaWG)
	if awgErr != nil {
		d.log.Error("AmneziaWG sidecar configuration failed", "err", awgErr)
	} else {
		d.replaceAWG(out.AmneziaWG, prepared)
	}

	// Start TrustTunnel clients first; sing-box SOCKS outbounds depend on them.
	if !reflect.DeepEqual(out.TrustTunnel, d.tt) {
		d.stopTrustTunnel()
		d.startTrustTunnel(out.TrustTunnel)
		d.tt = out.TrustTunnel
	}

	if restarted, err := d.eng.Apply(out.SingBox); err != nil {
		return err
	} else if restarted && prev != nil {
		d.coreRestarted = true
		d.log.Info("sing-box core restarted with new configuration")
	}
	d.reselectLocked()
	d.filters.Configure(cfg.Filters)

	if d.dns == nil || prev == nil || !reflect.DeepEqual(prev.DNS, cfg.DNS) || !reflect.DeepEqual(prev.Groups, cfg.Groups) {
		if d.dns != nil {
			d.dns.Close()
			d.dns = nil
		}
		if err := d.startDNS(); err != nil {
			return fmt.Errorf("DNS: %w", err)
		}
	}

	newRoute := netfilter.TProxyRoute{Mark: cfg.Intercept.FWMark, Table: cfg.Intercept.RouteTable}
	wantRoute := cfg.Intercept.Mode == model.InterceptTProxy
	if d.routeOn && (!wantRoute || newRoute != d.route) {
		d.route.Remove()
		d.routeOn = false
	}
	if wantRoute && !d.routeOn {
		if err := newRoute.Apply(); err != nil {
			return err
		}
		d.routeOn = true
	}
	d.route = newRoute

	// Interception mode changes require removing the complete previous chain set.
	if d.planOn && d.plan.Mode != out.Plan.Mode {
		d.removeFirewall()
	}
	d.plan = out.Plan
	if err := d.opt.Firewall.Apply(ctx, d.plan); err != nil {
		d.removeFirewall()
		return fmt.Errorf("interception rules: %w", err)
	}
	d.planOn = true
	return nil
}

// shutdown removes interception before stopping the services it depends on.
func (d *Daemon) shutdown() {
	if g := d.domainGeneration.Load(); g != nil {
		g.cancel()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.planOn {
		d.removeFirewall()
	}
	if d.routeOn {
		d.route.Remove()
		d.routeOn = false
	}
	if d.dns != nil {
		d.dns.Close()
	}
	d.eng.Close()
	d.stopTrustTunnel()
	d.replaceAWG(nil, nil)
	d.geo.set.Close()
	d.geo.cats.Close()
	d.geo.set, d.geo.cats = nil, nil
}

// startDNS installs runtime matchers and opens both DNS transports before interception.
func (d *Daemon) startDNS() error {
	var blocks []model.Group
	for _, g := range d.cfg.Groups {
		if !g.Disabled && g.Target == model.TargetBlock {
			blocks = append(blocks, g)
		}
	}
	m := lists.New(blocks)
	cfg := d.dnsConfig(d.cfg.DNS)
	cfg.Observe = d.observeDomain
	cfg.DropAAAA = true
	cfg.Blocked = func(name string) bool { return m.Domain(name) != lists.NoMatch }
	canary := d.cfg.DNS.FirefoxCanary
	cfg.BlockReason = func(name string) string {
		if canary && strings.EqualFold(strings.TrimSuffix(name, "."), "use-application-dns.net") {
			return "firefox-canary"
		}
		return d.filters.Match(name)
	}
	cfg.OnRoute = d.recordDNSRoute
	cfg.OnBlock = d.recordDNSBlock
	d.dns = dnsfront.New(cfg)
	return d.dns.Start()
}

// watch refreshes neighbor addresses and restores missing interception rules.
func (d *Daemon) watch(ctx context.Context) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if n := d.readNeighbors(d.cfg); !maps.Equal(n, d.neighbors) {
		d.audit("watchdog", "Device addresses updated", "clients", "info", neighborsChange(d.neighbors, n))
		d.neighbors = n
		out, err := d.compileLocked()
		if err != nil {
			d.log.Error("recompile", "err", err)
			return
		}
		// Client addresses live in watched rule-set files, so sing-box can
		// pick up changes without restarting.
		if restarted, err := d.eng.Apply(out.SingBox); err != nil {
			d.log.Error("restart sing-box", "err", err)
		} else if restarted {
			d.log.Warn("sing-box configuration changed after address updates, core restarted")
			d.reselectLocked()
		}
		d.plan = out.Plan
		if err := d.opt.Firewall.Apply(ctx, d.plan); err != nil {
			d.log.Error("rule update", "err", err)
		}
		return
	}
	if !d.opt.Firewall.Healthy(ctx, d.plan) {
		if err := d.opt.Firewall.Apply(ctx, d.plan); err != nil {
			d.log.Error("rule restoration", "err", err)
			d.audit("watchdog", "Interception rules not restored", "firewall", "error", err.Error())
			return
		}
		d.restored++
		d.log.Warn("interception rules disappeared (NDM likely rebuilt firewall) — restored")
		d.audit("watchdog", "Interception rules restored", "firewall", "warn",
			fmt.Sprintf("firewall rules were missing — NDM likely rebuilt it; restorations since start: %d", d.restored))
	}
}

// compileLocked compiles configuration and writes local rule-sets while d.mu
// is held. sing-box loads the files at startup and watches subsequent changes.
func (d *Daemon) compileLocked() (compile.Output, error) {
	out, err := compile.Compile(compile.Input{Config: d.cfg, Neighbors: d.neighbors, RuleSetDir: d.opt.RuleSetDir, AWGPassword: d.awgPassword})
	if err != nil {
		return out, err
	}
	if err := os.MkdirAll(d.opt.RuleSetDir, 0o755); err != nil {
		return out, err
	}
	for name, raw := range out.RuleSets {
		if strings.HasPrefix(name, "dns-") {
			continue
		}
		if err := writeFile(filepath.Join(d.opt.RuleSetDir, name), raw, 0o644); err != nil {
			return out, err
		}
	}
	if err := d.writeCountrySetsLocked(out.Countries); err != nil {
		return out, err
	}
	if err := d.writeCategorySetsLocked(out.Categories); err != nil {
		return out, err
	}
	d.writeDNSLocked()
	if d.domainError != "" {
		return out, fmt.Errorf("DNS rule-sets: %s", d.domainError)
	}
	d.serverHosts = out.ServerHosts
	d.chains = out.Failover
	return out, nil
}

// writeFile atomically replaces a file through a temporary file and rename.
// Identical contents are left untouched to avoid unnecessary watcher reloads.
func writeFile(path string, data []byte, perm os.FileMode) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// startTrustTunnel starts external clients and retains their supervisor handles.
func (d *Daemon) startTrustTunnel(clients []compile.TrustTunnelClient) {
	d.ttProcs = map[string]*supervise.Process{}
	if len(clients) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.ttStop = cancel
	dir := filepath.Join(filepath.Dir(d.opt.RuleSetDir), "trusttunnel")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		d.log.Error("TrustTunnel directory", "err", err)
		return
	}
	for _, c := range clients {
		path := filepath.Join(dir, c.ID+".toml")
		// Configuration contains secrets, so restrict access to the owner.
		if err := writeFile(path, []byte(c.Config), 0o600); err != nil {
			d.log.Error("TrustTunnel configuration", "connection", c.ID, "err", err)
			continue
		}
		if _, err := os.Stat(c.Binary); err != nil {
			d.log.Error("trusttunnel_client not found, connection will not work", "connection", c.ID, "binary", c.Binary)
			continue
		}
		p := &supervise.Process{Name: "trusttunnel/" + c.ID, Path: c.Binary, Args: []string{"--config", path}, Log: d.log}
		d.ttProcs[c.ID] = p
		d.ttWG.Add(1)
		go func() { defer d.ttWG.Done(); p.Run(ctx) }()
		d.log.Info("TrustTunnel started", "connection", c.ID, "socks", c.SocksPort)
	}
}

// stopTrustTunnel cancels supervisors and waits for all client processes to stop.
func (d *Daemon) stopTrustTunnel() {
	if d.ttStop != nil {
		d.ttStop()
		d.ttWG.Wait()
		d.ttStop = nil
	}
	d.ttProcs = nil
}

// readNeighbors builds the MAC-to-IP snapshot for the configured LAN interface.
func (d *Daemon) readNeighbors(cfg *model.Config) map[string]netip.Addr {
	n, err := devices.ReadARP(d.opt.ARPPath, cfg.LAN.Interface)
	if err != nil {
		d.log.Warn("ARP unavailable, clients without static IP are excluded", "err", err)
		return map[string]netip.Addr{}
	}
	return n
}

// removeFirewall removes active interception rules using a fresh bounded context.
func (d *Daemon) removeFirewall() {
	// Use a fresh context because cleanup must run after the serving context is canceled.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := d.opt.Firewall.Remove(ctx, d.plan); err != nil && !errors.Is(err, context.Canceled) {
		d.log.Warn("remove rules", "err", err)
	}
	d.planOn = false
}

// NoTokenFrom returns trusted dashboard networks from the atomic configuration
// snapshot, avoiding locks and configuration copies on every API request.
func (d *Daemon) NoTokenFrom() []netip.Prefix {
	if cfg := d.live.Load(); cfg != nil {
		return cfg.API.NoTokenPrefixes()
	}
	return nil
}

// Config returns a deep copy of the current configuration.
func (d *Daemon) Config() *model.Config {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cfg == nil {
		return nil
	}
	return d.cfg.Clone()
}

// Status summarizes daemon, engine, interception, and connection health for the API.
type Status struct {
	AWGPending    bool              `json:"amneziawg_pending,omitempty"`
	Started       time.Time         `json:"started"`
	Mode          string            `json:"mode"`
	Sources       int               `json:"sources"`
	Connections   int               `json:"connections"`
	Groups        int               `json:"groups"`
	Entries       int               `json:"entries"`
	Clients       int               `json:"clients"`
	OnlineDevices int               `json:"online_devices"`
	DNS           string            `json:"dns"`
	Firewall      bool              `json:"firewall"`
	Restored      int               `json:"restored"`
	TrustTunnel   []TrustTunnelInfo `json:"trusttunnel"`
	GeoIP         GeoInfo           `json:"geoip"`
	// ConnCountries maps connection IDs to server country codes for flags.
	ConnCountries map[string]string `json:"conn_countries"`
	// Failover maps fallback-enabled connection IDs to their selected members.
	Failover map[string]FailoverInfo `json:"failover"`
	// Health maps connection IDs to their latest probe results.
	Health map[string]ConnHealth `json:"health"`
}

// TrustTunnelInfo reports the supervised client process associated with a connection.
type TrustTunnelInfo struct {
	ID      string `json:"id"`
	Running bool   `json:"running"`
	Starts  int64  `json:"starts"`
}

// Status snapshots runtime health under d.mu and checks active firewall entry jumps.
func (d *Daemon) Status(ctx context.Context) Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := Status{AWGPending: d.pendingAWG != nil, Started: d.started, Restored: d.restored, OnlineDevices: len(d.neighbors)}
	if d.cfg == nil {
		return st
	}
	st.Mode = string(d.cfg.Intercept.Mode)
	st.Sources = len(d.plan.Sources)
	st.Connections = len(d.cfg.Connections)
	st.Groups = len(d.cfg.Groups)
	st.Clients = len(d.cfg.Clients)
	for _, g := range d.cfg.Groups {
		st.Entries += len(g.Entries)
	}
	if d.dns != nil {
		st.DNS = d.dns.Addr()
	}
	st.Firewall = d.planOn && d.opt.Firewall.Healthy(ctx, d.plan)
	st.GeoIP = d.geoInfoLocked()
	st.ConnCountries = d.geo.conns
	st.Failover, st.Health = d.failoverInfoLocked()
	for _, c := range d.tt {
		info := TrustTunnelInfo{ID: c.ID}
		if p := d.ttProcs[c.ID]; p != nil {
			info.Running, info.Starts = true, p.Starts()
		}
		st.TrustTunnel = append(st.TrustTunnel, info)
	}
	return st
}

// Stats returns connection traffic history and summaries.
func (d *Daemon) Stats(withSeries bool) stats.Snapshot { return d.stats.Snapshot(withSeries) }

// Hosts returns per-host traffic for one connection.
func (d *Daemon) Hosts(tag, query string, limit int) ([]stats.Host, int) {
	return d.stats.Hosts(tag, query, limit)
}

// Device combines an ARP neighbor, configured client identity, and observed hostname.
type Device struct {
	MAC            string     `json:"mac"`
	IP             string     `json:"ip"`
	Client         string     `json:"client,omitempty"` // configured client name
	Mode           model.Mode `json:"mode,omitempty"`
	Hostname       string     `json:"hostname,omitempty"`
	HostnameSource string     `json:"hostname_source,omitempty"`
}

// Devices merges current LAN neighbors with configured client names and identities.
func (d *Daemon) Devices() []Device {
	d.mu.Lock()
	defer d.mu.Unlock()
	known := map[string]model.Client{}
	if d.cfg != nil {
		for _, c := range d.cfg.Clients {
			if mac, err := net.ParseMAC(c.MAC); err == nil {
				known[mac.String()] = c
			}
		}
	}
	var out []Device
	for mac, ip := range d.neighbors {
		dev := Device{MAC: mac, IP: ip.String()}
		if cached, ok := d.deviceNames[mac]; ok && cached.ip == ip && time.Now().Before(cached.expires) {
			dev.Hostname, dev.HostnameSource = cached.name.Hostname, cached.name.Source
		}
		if c, ok := known[mac]; ok {
			dev.Client, dev.Mode = c.Name, c.Mode
		}
		out = append(out, dev)
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := netip.ParseAddr(out[i].IP)
		b, _ := netip.ParseAddr(out[j].IP)
		return a.Less(b)
	})
	return out
}
