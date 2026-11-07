package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/atrevido/keenetic-proxy/internal/compile"
	"github.com/atrevido/keenetic-proxy/internal/dnsfront"
	"github.com/atrevido/keenetic-proxy/internal/domaincache"
	"github.com/atrevido/keenetic-proxy/internal/model"
	"github.com/miekg/dns"
)

// DomainStatus reports cached DNS routes and file-write status, without certifying
// that the engine has reloaded those files.
type DomainStatus struct {
	Records     []domaincache.Record `json:"records"`
	Conflicts   map[string][]string  `json:"conflicts"`
	Limited     bool                 `json:"limited"`
	Error       string               `json:"error,omitempty"`
	WrittenAt   time.Time            `json:"written_at"`
	Application string               `json:"application"`
	Demo        bool                 `json:"demo,omitempty"`
}

// observeDomain retains device DNS answers only for currently configured domains.
func (d *Daemon) observeDomain(name string, m *dns.Msg) {
	cfg := d.live.Load()
	name = domaincache.Normalize(name)
	if cfg != nil && compile.MatchesDomain(cfg, name) {
		d.domains.Observe(name, m, "observed")
	}
}

// domainRecords filters fresh cache records against the current configuration.
func (d *Daemon) domainRecords() []domaincache.Record {
	cfg := d.live.Load()
	out := []domaincache.Record{}
	if cfg == nil {
		return out
	}
	matcher := compile.NewDomainMatcher(cfg)
	for _, r := range d.domains.Snapshot(nil) {
		if matcher.Match(r.Name) {
			out = append(out, r)
		}
	}
	return out
}

// Domains returns cache records, shared-IP conflicts, and the latest write status.
func (d *Daemon) Domains() DomainStatus {
	records := d.domainRecords()
	st := DomainStatus{Records: records, Conflicts: map[string][]string{}, Limited: d.domains.Limited(), Application: "files_written_reload_pending"}
	st.Conflicts = domainConflicts(d.live.Load(), records)
	for _, r := range records {
		st.Limited = st.Limited || r.Limited
	}
	d.domainMu.Lock()
	st.WrittenAt = d.domainWritten
	st.Error = d.domainError
	d.domainMu.Unlock()
	return st
}
func recordsMap(records []domaincache.Record) map[string][]string {
	out := map[string][]string{}
	for _, r := range records {
		for _, a := range r.Addresses {
			out[r.Name] = append(out[r.Name], a.IP)
		}
	}
	return out
}

// flushDomains serializes with Apply, preventing stale writes after group edits.
func (d *Daemon) flushDomains() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cfg == nil {
		return
	}
	d.writeDNSLocked()
	if d.domainError == "" {
		d.cleanDNSFilesLocked()
	}
}

// writeDNSLocked writes changed DNS fallback sets while d.mu is held, and records
// write progress under domainMu.
func (d *Daemon) writeDNSLocked() {
	sets := compile.DNSRuleSets(d.cfg, recordsMap(d.domainRecords()))
	fingerprint, _ := json.Marshal(sets)
	d.domainMu.Lock()
	defer d.domainMu.Unlock()
	if bytes.Equal(fingerprint, d.domainFingerprint) && d.domainError == "" {
		return
	}
	for name, raw := range sets {
		if err := writeFile(filepath.Join(d.opt.RuleSetDir, name), raw, 0o644); err != nil {
			d.domainError = err.Error()
			return
		}
	}
	d.domainFingerprint = fingerprint
	d.domainWritten = time.Now()
	d.domainError = ""
}

// domainGeneration cancels in-flight refreshes when the applied configuration changes.
type domainGeneration struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// resetDomainGeneration publishes a fresh refresh lifetime and cancels the previous one.
func (d *Daemon) resetDomainGeneration() {
	ctx, cancel := context.WithCancel(context.Background())
	old := d.domainGeneration.Swap(&domainGeneration{ctx, cancel})
	if old != nil {
		old.cancel()
	}
}

// RefreshDomains resolves up to 100 names with bounded concurrency. Preview uses
// an isolated cache; applied refreshes require names in the live configuration.
func (d *Daemon) RefreshDomains(ctx context.Context, names []string, preview bool) (DomainStatus, error) {
	if g := d.domainGeneration.Load(); g != nil {
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()
		stop := context.AfterFunc(g.ctx, cancel)
		defer stop()
		ctx = cctx
	}

	if len(names) == 0 || len(names) > 100 {
		return DomainStatus{}, fmt.Errorf("between 1 and 100 domains required")
	}
	cfg := d.live.Load()
	if cfg == nil {
		return DomainStatus{}, fmt.Errorf("daemon is not running")
	}
	matcher := compile.NewDomainMatcher(cfg)
	clean := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		e, err := model.ParseEntry(name)
		if err != nil || e.Kind != model.EntryDomain {
			return DomainStatus{}, fmt.Errorf("invalid domain: %s", name)
		}
		if !preview && !matcher.Match(e.Domain) {
			return DomainStatus{}, fmt.Errorf("domain is not in the applied list: %s", e.Domain)
		}
		if !seen[e.Domain] {
			clean = append(clean, e.Domain)
			seen[e.Domain] = true
		}
	}
	cache := d.domains
	if preview {
		cache = domaincache.New(time.Now)
	}
	dnsCfg := d.dnsConfig(cfg.DNS)
	dnsCfg.Timeout = 3 * time.Second
	resolver := dnsfront.New(dnsCfg)
	defer resolver.CloseIdle()
	var wg sync.WaitGroup
	for _, name := range clean {
		if d.live.Load() != cfg {
			wg.Wait()
			return DomainStatus{}, fmt.Errorf("configuration changed")
		}
		select {
		case d.domainSlots <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return DomainStatus{}, ctx.Err()
		}
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			defer func() { <-d.domainSlots }()
			q := new(dns.Msg)
			q.SetQuestion(dns.Fqdn(name), dns.TypeA)
			qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			resp := resolver.ResolveRaw(qctx, q)
			if resp.Rcode != dns.RcodeSuccess || resp.Truncated {
				cache.SetError(name, "DNS: "+dns.RcodeToString[resp.Rcode])
				return
			}
			cache.Observe(name, resp, "resolved")
		}(name)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return DomainStatus{}, ctx.Err()
	}
	if preview {
		return DomainStatus{Records: cache.Snapshot(nil), Conflicts: map[string][]string{}, Application: "preview"}, nil
	}
	d.flushDomains()
	return d.Domains(), nil
}

// runDomains flushes cache expiry each second and schedules nonoverlapping refresh
// batches with a minimum retry interval.
func (d *Daemon) runDomains(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	busy := make(chan struct{}, 1)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		d.flushDomains()
		select {
		case busy <- struct{}{}:
		default:
			continue
		}
		go func() {
			defer func() { <-busy }()
			cfg := d.live.Load()
			if cfg == nil {
				return
			}
			names := refreshDomainNames(cfg, d.domainRecords(), time.Now())
			existing := map[string]domaincache.Record{}
			for _, r := range d.domainRecords() {
				existing[r.Name] = r

			}
			due := []string{}
			now := time.Now()
			for _, name := range names {
				r := existing[name]
				if now.Sub(r.AttemptedAt) < 30*time.Second {
					continue
				}
				fresh := false
				for _, a := range r.Addresses {
					if a.ExpiresAt.After(now.Add(5 * time.Second)) {
						fresh = true
						break
					}
				}
				if !fresh {
					due = append(due, name)
				}
				if len(due) == 100 {
					break
				}
			}
			if len(due) > 0 {
				_, _ = d.RefreshDomains(ctx, due, false)
			}
		}()
	}
}
func containsName(names []string, name string) bool {
	for _, n := range names {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

// refreshDomainNames keeps configured roots and recently observed subdomains warm;
// service refreshes do not count as device use.
func refreshDomainNames(cfg *model.Config, records []domaincache.Record, now time.Time) []string {
	names := compile.DomainNames(cfg)
	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
	}
	for _, r := range records {
		if !seen[r.Name] && !r.ObservedAt.IsZero() && now.Sub(r.ObservedAt) < 10*time.Minute {
			names = append(names, r.Name)
			seen[r.Name] = true
		}
	}
	return names
}

// domainConflicts identifies cached IPs shared by distinct group rule-sets.
func domainConflicts(cfg *model.Config, records []domaincache.Record) map[string][]string {
	out := map[string][]string{}
	if cfg == nil {
		return out
	}
	groups := map[string]map[string]bool{}
	for _, g := range cfg.Groups {
		if g.Disabled {
			continue
		}
		if g.IsCountry() {
			continue
		}
		m := compile.NewDomainMatcher(&model.Config{Groups: []model.Group{g}})
		for _, r := range records {
			if !m.Match(r.Name) {
				continue
			}
			for _, a := range r.Addresses {
				if groups[a.IP] == nil {
					groups[a.IP] = map[string]bool{}
				}
				groups[a.IP][compile.DomainTag(g)] = true
				out[a.IP] = appendUnique(out[a.IP], g.Name+" · "+r.Name)
			}
		}
	}
	for ip := range out {
		if len(groups[ip]) < 2 {
			delete(out, ip)
		}
	}
	return out
}
func appendUnique(ss []string, s string) []string {
	for _, x := range ss {
		if x == s {
			return ss
		}
	}
	return append(ss, s)
}

// dnsRouteNote explains potential DNS-derived routing for connection journal entries.
func dnsRouteNote(cfg *model.Config, records []domaincache.Record, raw, ip string) string {
	if cfg == nil || !strings.Contains(raw, "dns-") {
		return ""
	}
	names := []string{}
	for _, g := range cfg.Groups {
		if g.Disabled {
			continue
		}
		if !strings.Contains(raw, compile.DomainTag(g)) {
			continue
		}
		m := compile.NewDomainMatcher(&model.Config{Groups: []model.Group{g}})
		for _, r := range records {
			if !m.Match(r.Name) {
				continue
			}
			for _, a := range r.Addresses {
				if a.IP == ip {
					names = appendUnique(names, r.Name)
				}
			}
		}
	}
	sort.Strings(names)
	if len(names) > 4 {
		names = append(names[:4], "…")
	}
	note := " · DNS: " + strings.Join(names, ", ")
	if len(domainConflicts(cfg, records)[ip]) > 1 {
		note += " · ambiguous IP, using group order"
	}
	return note
}

// Called after Apply/rollback is finished, never while the old engine still uses its files.
func (d *Daemon) cleanDNSFilesLocked() {
	keep := compile.DNSRuleSets(d.cfg, nil)
	entries, err := os.ReadDir(d.opt.RuleSetDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasPrefix(name, "dns-") && strings.HasSuffix(name, ".json") {
			if _, ok := keep[name]; !ok {
				if err := os.Remove(filepath.Join(d.opt.RuleSetDir, name)); err != nil && !os.IsNotExist(err) {
					d.log.Warn("failed to remove stale DNS rule set", "err", err)
				}
			}
		}
	}
}
