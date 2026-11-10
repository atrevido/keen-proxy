package api

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/compile"
	"github.com/ordo-systems/keen-proxy/internal/journal"
	"github.com/ordo-systems/keen-proxy/internal/model"
)

// startJournals seeds synthetic history and adds one route per second
// so the journal UI remains active without a router.
func (d *Demo) startJournals(firewallFixture []byte) {
	d.journals = journal.NewSet("")
	recent := time.Now()
	now := recent.Add(-4 * time.Hour)
	audit := d.journals[journal.Audit]
	history := []journal.Entry{
		{Actor: "system", Action: "Start keenProxy", Target: "core", Result: "ok", Details: "interception tproxy, sources 3, DNS 0.0.0.0:5354"},
		{Actor: "system", Action: "Country database updated", Target: "geoip", Result: "ok", Details: now.Format("2006-01") + "-01"},
		{Actor: "watchdog", Action: "Device addresses updated", Target: "clients", Result: "info", Details: "added: 192.168.7.35 (f0:18:98:44:55:66); online: 6"},
		{Actor: "user", Action: "Panel login", Target: "auth", Result: "ok", Details: "token login, 192.168.7.50"},
		{Actor: "user", Action: "Configuration rejected", Target: "config", Result: "error", Details: "invalid configuration: groups[5] \"Steam\": unknown target \"nl2\" (from 192.168.7.50)"},
		{Actor: "user", Action: "Apply configuration", Target: "config", Result: "ok", Details: "groups: 5 → 6; core not restarted (from 192.168.7.50)"},
	}
	for i := range history {
		// Emit history chronologically, from startup a few hours ago to recent events.
		now = now.Add(time.Duration(rand.IntN(25)+5) * time.Minute)
		history[i].Time = now
	}
	firewallEntries, firewallErr := demoFirewallAudit(recent, firewallFixture)
	history = append(history, firewallEntries...)
	history = append(history, journal.Entry{Time: recent.Add(-3 * time.Minute), Actor: "watchdog", Action: "Switch to fallback", Target: "fi", Result: "warn",
		Details: "«Finland WG» unavailable for 1 min 4 s (dial tcp 203.0.113.10:51820: i/o timeout) — traffic via «Netherlands Reality»"})
	// Journal pagination uses insertion IDs, so seed all audit sources in time order.
	slices.SortStableFunc(history, func(a, b journal.Entry) int { return a.Time.Compare(b.Time) })
	for _, e := range history {
		audit.Add(e)
	}
	sys := d.journals[journal.System]
	for i, e := range []journal.Entry{
		{Level: "INFO", Source: "keenproxy", Message: "keenProxy started mode=tproxy sources=3 dns=0.0.0.0:5354"},
		{Level: "INFO", Source: "keenproxy", Message: "country database loaded built=" + time.Now().Format("2006-01") + "-01 type=DBIP-Country-Lite"},
		{Level: "WARN", Source: "sing-box", Message: "outbound/wireguard[fi]: peer 203.0.113.10:51820: handshake did not complete after 5 seconds, retrying"},
		{Level: "INFO", Source: "keenproxy", Message: "configuration applied and saved"},
		{Level: "ERROR", Source: "sing-box", Message: "inbound/redirect[redirect-in]: process connection from 192.168.7.35:51244: dial tcp 142.250.74.46:443: i/o timeout"},
		{Level: "ERROR", Source: "keenproxy", Message: "rule restoration failed trigger=netfilter_hook outcome=verify_failed error=synthetic"},
	} {
		e.Time = time.Now().Add(-time.Duration(60-i*9) * time.Minute)
		sys.Add(e)
	}
	if firewallErr != nil {
		sys.Add(journal.Entry{Level: "WARN", Source: "keenproxy", Message: firewallErr.Error()})
	}
	for i := 0; i < 300; i++ {
		d.addDemoRoute(time.Now().Add(-time.Duration(300-i) * 3 * time.Second))
	}
	go func() {
		for range time.Tick(time.Second) {
			for n := rand.IntN(3); n >= 0; n-- {
				d.addDemoRoute(time.Now())
			}
		}
	}()
}

var demoClients = []string{"192.168.7.21", "192.168.7.35", "192.168.7.50", "192.168.7.52", "192.168.7.104"}

// addDemoRoute generates a synthetic connection with sing-box-style rule metadata.
func (d *Demo) addDemoRoute(t time.Time) {
	d.mu.Lock()
	cfg := d.cfg
	d.mu.Unlock()
	e := journal.Entry{DestinationPort: 443, Time: t, Client: demoClients[rand.IntN(len(demoClients))] + ":" + strconv.Itoa(40000+rand.IntN(25000)), Network: "tcp"}
	if rand.IntN(6) == 0 {
		e.Network = "udp"
	}
	var groups []int
	for i, g := range cfg.Groups {
		if g.Target != model.TargetBlock && len(g.Entries) > 0 {
			groups = append(groups, i)
		}
	}
	source := "source_ip_cidr=192.168.7.0/24"
	if len(cfg.Networks) > 0 {
		source = "source_ip_cidr=" + cfg.Networks[0].CIDR
	}
	switch r := rand.IntN(10); {
	case r < 6 && len(groups) > 0:
		gi := groups[rand.IntN(len(groups))]
		g := cfg.Groups[gi]
		e.Host = g.Entries[rand.IntN(len(g.Entries))]
		if rand.IntN(2) == 0 {
			e.Host = "www." + e.Host
		}
		e.Outbound = outboundOf(g.Target)
		e.RuleRaw = source + " rule_set=group-" + strconv.Itoa(gi)
	case r < 8:
		e.Host = []string{"ya.ru", "mail.ru", "vk.com", "gosuslugi.ru", "ozon.ru", "dzen.ru"}[rand.IntN(6)]
		e.Outbound = "direct"
		e.RuleRaw = source
		for _, g := range cfg.Groups {
			if g.IsGeo() && g.Except {
				e.RuleRaw = "" // A Russian address misses the inverted country rule and uses final.
			}
		}
	default:
		e.Host = []string{"api.github.com", "cdn.jsdelivr.net", "fonts.gstatic.com", "discord.com", "www.figma.com"}[rand.IntN(5)]
		e.Outbound, e.RuleRaw = "direct", source
		for _, g := range cfg.Groups {
			if g.IsGeo() && g.Except {
				e.Outbound = outboundOf(g.Target)
				e.RuleRaw = source + " && !(rule_set=" + tagsOf(g) + ")"
				break
			}
		}
	}
	e.IP = fmt.Sprintf("%d.%d.%d.%d", 5+rand.IntN(200), rand.IntN(255), rand.IntN(255), 1+rand.IntN(254))
	e.Rule = journal.DescribeRule(cfg, e.RuleRaw)
	d.journals[journal.Routes].Add(e)
	if t.Unix()%3 == 0 {
		// A browser often opens several connections to the same destination.
		e.Client = strings.Split(e.Client, ":")[0] + ":" + strconv.Itoa(65000+rand.IntN(500))
		e.Time = t.Add(time.Millisecond)
		d.journals[journal.Routes].Add(e)
	}
}

func outboundOf(target string) string {
	if target == "" || target == model.TargetDirect {
		return "direct"
	}
	return target
}

func tagsOf(g model.Group) string {
	var tags []string
	for _, c := range g.Countries {
		tags = append(tags, "geoip-"+strings.ToLower(c))
	}
	for _, c := range g.Categories {
		tags = append(tags, compile.CategoryTagPrefix+strings.ToLower(c))
	}
	if len(tags) == 1 {
		return tags[0]
	}
	return "[" + strings.Join(tags, " ") + "]"
}

// Journal returns the selected in-memory demo journal.
func (d *Demo) Journal(k journal.Kind) *journal.Journal { return d.journals[k] }

// Mask creates a log redactor from the current demo configuration.
func (d *Demo) Mask() func(string) string { return journal.Masker(d.Config()) }
