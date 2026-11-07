package api

import (
	"context"
	"fmt"
	"github.com/ordo-systems/keen-proxy/internal/compile"
	"github.com/ordo-systems/keen-proxy/internal/daemon"
	"github.com/ordo-systems/keen-proxy/internal/domaincache"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/miekg/dns"
	"hash/fnv"
	"strings"
	"time"
)

// demoResolve produces documentation-range addresses and errors for .invalid names.
func demoResolve(c *domaincache.Cache, names []string) {
	for _, name := range names {
		if strings.HasSuffix(name, ".invalid") {
			c.SetError(name, "Demo: DNS NXDOMAIN")
			continue
		}
		hash := fnv.New32a()
		hash.Write([]byte(name))
		ip := fmt.Sprintf("203.0.113.%d", 1+hash.Sum32()%200)
		rr, _ := dns.NewRR(fmt.Sprintf("%s. 120 IN A %s", name, ip))
		m := new(dns.Msg)
		m.Answer = []dns.RR{rr}
		c.Observe(name, m, "demo")
	}
}

// Domains returns synthetic cache records for applied groups, with an empty conflict map.
func (d *Demo) Domains() daemon.DomainStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	records := []domaincache.Record{}
	for _, r := range d.domainCache.Snapshot(nil) {
		if compile.MatchesDomain(d.cfg, r.Name) {
			records = append(records, r)
		}
	}
	return daemon.DomainStatus{Records: records, Conflicts: map[string][]string{}, Demo: true, Application: "demo"}
}

// RefreshDomains resolves synthetic names; preview mode uses a separate cache.
func (d *Demo) RefreshDomains(ctx context.Context, names []string, preview bool) (daemon.DomainStatus, error) {
	cfg := d.Config()
	clean := []string{}
	for _, name := range names {
		e, err := model.ParseEntry(name)
		if err != nil || e.Kind != model.EntryDomain {
			return daemon.DomainStatus{}, fmt.Errorf("invalid domain")
		}
		if !preview && !compile.MatchesDomain(cfg, e.Domain) {
			return daemon.DomainStatus{}, fmt.Errorf("domain not applied")
		}
		clean = append(clean, e.Domain)
	}
	if preview {
		c := domaincache.New(time.Now)
		demoResolve(c, clean)
		return daemon.DomainStatus{Records: c.Snapshot(nil), Conflicts: map[string][]string{}, Demo: true, Application: "preview"}, nil
	}
	demoResolve(d.domainCache, clean)
	return d.Domains(), nil
}
