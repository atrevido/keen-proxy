// Package compile deterministically converts configuration into sing-box JSON
// and a firewall plan without runtime side effects.
package compile

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/ordo-systems/keen-proxy/internal/netfilter"
)

const (
	tagTCPIn  = "tcp-in"
	tagUDPIn  = "udp-in"
	tagDirect = "direct"
)

// Input combines validated configuration with runtime address information.
type Input struct {
	Config *model.Config
	// Neighbors maps MAC addresses to ARP/DHCP addresses for clients without static IPs.
	Neighbors map[string]netip.Addr
	// RuleSetDir holds local rule-set files referenced by the generated configuration.
	RuleSetDir string
}

// Output contains the engine configuration and runtime artifacts to apply.
type Output struct {
	// SingBox depends on configuration, not current client addresses, so address
	// changes do not require an engine restart.
	SingBox []byte
	// RuleSets maps filenames to local rule-set contents. sing-box watches these
	// files for changes, including updates to client class addresses.
	RuleSets map[string][]byte
	// Countries lists ISO codes whose geoip-<code>.json files the daemon must
	// populate with country prefixes in RuleSetDir.
	Countries []string
	// TrustTunnel lists external clients the daemon must keep running.
	TrustTunnel []TrustTunnelClient
	// ServerHosts maps connection IDs to server addresses or hostnames.
	ServerHosts map[string]string
	// Failover describes chains the daemon probes and switches through selectors.
	Failover []FailoverChain
	Plan     netfilter.Plan
}

// Client classes depend only on configured connections; address updates change
// file contents without changing the class set.
const (
	classOff   = "clients-off"
	classSplit = "clients-split"
	classFull  = "clients-full-" // + connection ID
)

// noSource keeps empty classes from matching every source.
const noSource = "255.255.255.255/32"

// policy assigns routing behavior to one client or network prefix.
type policy struct {
	source netip.Prefix
	mode   model.Mode
	target string
}

// Compile builds runtime artifacts from a configuration that has passed Validate.
func Compile(in Input) (Output, error) {
	c := in.Config
	clients, networks, excluded := policies(in)

	var sources []netip.Prefix
	for _, p := range append(append([]policy{}, clients...), networks...) {
		if p.mode == model.ModeSplit || p.mode == model.ModeFull {
			sources = append(sources, p.source)
		}
	}
	conns, err := connections(c)
	if err != nil {
		return Output{}, err
	}
	bypass := netfilter.DefaultBypass()
	for _, a := range conns.servers {
		bypass = append(bypass, netip.PrefixFrom(a, a.BitLen()))
	}

	chains, tags := failover(c)
	sb, err := json.MarshalIndent(singBox(c, in.RuleSetDir, networks, conns, chains, tags), "", "  ")
	if err != nil {
		return Output{}, err
	}
	_, _, _, countries := groupRuleSets(c.Groups, in.RuleSetDir)
	sets, err := clientRuleSets(c, clients)
	if err != nil {
		return Output{}, err
	}
	for name, raw := range DNSRuleSets(c, nil) {
		sets[name] = raw
	}
	return Output{
		SingBox:     sb,
		RuleSets:    sets,
		Countries:   countries,
		TrustTunnel: conns.trustTunnel,
		ServerHosts: conns.hosts,
		Failover:    chains,
		Plan: netfilter.Plan{
			BlockEncryptedDNS: c.DNS.BlockEncrypted,
			Mode:              c.Intercept.Mode,
			LAN:               c.LAN.Interface,
			DNSPort:           mustPort(c.DNS.Listen),
			TCPPort:           c.Intercept.TCPPort,
			UDPPort:           c.Intercept.UDPPort,
			FWMark:            c.Intercept.FWMark,
			Sources:           sources,
			Excluded:          excluded,
			Bypass:            bypass,
		},
	}, nil
}

// policies resolves client inheritance and network rules. Client rules take
// precedence over networks; excluded clients bypass interception even inside
// participating networks.
func policies(in Input) (clients, networks []policy, excluded []netip.Addr) {
	c := in.Config
	type netPol struct {
		prefix netip.Prefix
		n      model.Network
	}
	var nets []netPol
	for _, n := range c.Networks {
		p, _ := netip.ParsePrefix(n.CIDR)
		nets = append(nets, netPol{p.Masked(), n})
		networks = append(networks, policy{p.Masked(), n.Mode, n.Target})
	}
	for _, cl := range c.Clients {
		ip, ok := clientIP(cl, in.Neighbors)
		if !ok {
			continue // client has no current address
		}
		var network *model.Network
		for i := range nets {
			if nets[i].prefix.Contains(ip) {
				network = &nets[i].n
				break
			}
		}
		mode, target := cl.Mode, cl.Target
		if mode == model.ModeInherit {
			if network == nil {
				continue
			}
			mode = network.Mode
		}
		if target == "" && network != nil {
			target = network.Target
		}
		pol := policy{netip.PrefixFrom(ip, ip.BitLen()), mode, target}
		if mode == model.ModeOff {
			if network != nil && network.Mode != model.ModeOff {
				excluded = append(excluded, ip)
				clients = append(clients, pol)
			}
			continue
		}
		clients = append(clients, pol)
	}
	return clients, networks, excluded
}

// clientIP prefers a configured static address over a normalized MAC neighbor lookup.
func clientIP(cl model.Client, neighbors map[string]netip.Addr) (netip.Addr, bool) {
	if cl.IP != "" {
		a, err := netip.ParseAddr(cl.IP)
		return a, err == nil
	}
	mac, err := net.ParseMAC(cl.MAC)
	if err != nil {
		return netip.Addr{}, false
	}
	a, ok := neighbors[mac.String()]
	return a, ok
}

// obj is a JSON-shaped engine configuration node.
type obj = map[string]any

// FailoverChain describes a sing-box selector and its ordered fallback members.
type FailoverChain struct {
	ID    string   // connection referenced by groups and clients
	Tag   string   // selector tag
	Chain []string // IDs in priority order, starting with ID; may end in direct
}

// FailoverTagPrefix plus a connection ID names its fallback selector.
const FailoverTagPrefix = "failover-"

// failover maps route targets to selectors while preserving connection IDs on
// actual outbounds so statistics and journals identify the selected connection.
func failover(c *model.Config) ([]FailoverChain, map[string]string) {
	var chains []FailoverChain
	tags := map[string]string{}
	for _, cn := range c.Connections {
		if cn.Fallback == "" {
			continue
		}
		fc := FailoverChain{ID: cn.ID, Tag: FailoverTagPrefix + cn.ID, Chain: c.Chain(cn.ID)}
		chains = append(chains, fc)
		tags[cn.ID] = fc.Tag
	}
	return chains, tags
}

// singBox orders global blocks, client policies, and network policies after sniffing.
func singBox(c *model.Config, ruleSetDir string, networks []policy, conns connOut, chains []FailoverChain, tags map[string]string) obj {
	inbounds := []obj{{"type": "redirect", "tag": tagTCPIn, "listen": "0.0.0.0", "listen_port": c.Intercept.TCPPort}}
	sniffIn := []string{tagTCPIn}
	if c.Intercept.Mode == model.InterceptTProxy {
		inbounds = append(inbounds, obj{"type": "tproxy", "tag": tagUDPIn, "listen": "0.0.0.0", "listen_port": c.Intercept.UDPPort, "network": "udp"})
		sniffIn = append(sniffIn, tagUDPIn)
	}

	ruleSets, blockSets, groupSets, _ := groupRuleSets(c.Groups, ruleSetDir)
	rules := []obj{
		{"inbound": sniffIn, "action": "sniff"},
		{"ip_is_private": true, "outbound": tagDirect},
	}
	for _, b := range blockSets {
		r := b.match()
		r["action"] = "reject"
		rules = append(rules, r)
	}
	clientSets, clientRules := clientRouting(c, ruleSetDir, groupSets, tags)
	ruleSets = append(clientSets, ruleSets...)
	rules = append(rules, clientRules...)
	rules = append(rules, sourceRules(networks, groupSets, tags)...)

	route := obj{"rules": rules, "final": tagDirect}
	if len(ruleSets) > 0 {
		route["rule_set"] = ruleSets
	}
	cfg := obj{
		"log":       obj{"level": "warn", "timestamp": true},
		"inbounds":  inbounds,
		"outbounds": append(append([]obj{{"type": "direct", "tag": tagDirect}}, conns.outbounds...), selectors(chains)...),
		"route":     route,
	}
	if len(conns.endpoints) > 0 {
		cfg["endpoints"] = conns.endpoints
	}
	return cfg
}

// selectors emits one selector per chain. Switching closes existing connections
// so applications reconnect through the newly selected outbound.
func selectors(chains []FailoverChain) []obj {
	var out []obj
	for _, fc := range chains {
		out = append(out, obj{
			"type": "selector", "tag": fc.Tag, "outbounds": fc.Chain, "default": fc.ID,
			"interrupt_exist_connections": true,
		})
	}
	return out
}

// groupSet associates shared match sets with a target and optional country exclusion.
type groupSet struct {
	tags   []string // one set per country for country groups
	target string
	// invert matches addresses outside the union of country sets.
	invert bool
	dns    bool
}

// match combines group sets with OR and optionally inverts their entire union.
func (g groupSet) match() obj {
	m := obj{"rule_set": g.tags}
	if g.invert {
		m["invert"] = true
	}
	if g.dns {
		return obj{"type": "logical", "mode": "and", "rules": []obj{m, {"domain_regex": []string{".+"}, "invert": true}}}
	}
	return m
}

// groupRuleSets shares inline entry sets across sources and references local
// geoip-<code> sets for countries. Explicit domain and address groups precede
// country groups regardless of their order in configuration.
func groupRuleSets(groups []model.Group, dir string) (sets []obj, block, routed []groupSet, countries []string) {
	var countryBlock, countryRouted []groupSet
	seen := map[string]bool{}
	for i, g := range groups {
		if g.Disabled {
			continue
		}
		if g.IsCountry() {
			gs := groupSet{target: g.Target, invert: g.Except}
			for _, cc := range g.Countries {
				tag := "geoip-" + strings.ToLower(cc)
				gs.tags = append(gs.tags, tag)
				if !seen[tag] {
					seen[tag] = true
					countries = append(countries, cc)
					sets = append(sets, obj{"type": "local", "tag": tag, "format": "source", "path": filepath.Join(dir, tag+".json")})
				}
			}
			if g.Target == model.TargetBlock {
				countryBlock = append(countryBlock, gs)
			} else {
				countryRouted = append(countryRouted, gs)
			}
			continue
		}
		var domains, cidrs []string
		for _, raw := range g.Entries {
			e, err := model.ParseEntry(raw)
			if err != nil {
				continue
			}
			if e.Kind == model.EntryDomain {
				domains = append(domains, e.Domain)
			} else {
				cidrs = append(cidrs, e.Prefix.String())
			}
		}
		if len(domains) == 0 && len(cidrs) == 0 {
			continue
		}
		rule := obj{}
		if len(domains) > 0 {
			rule["domain_suffix"] = domains
		}
		if len(cidrs) > 0 {
			rule["ip_cidr"] = cidrs
		}
		tag := "group-" + strconv.Itoa(i)
		sets = append(sets, obj{"type": "inline", "tag": tag, "rules": []obj{rule}})
		gs := groupSet{tags: []string{tag}, target: g.Target}
		if g.Target == model.TargetBlock {
			block = append(block, gs)
		} else {
			routed = append(routed, gs)
		}
	}

	var dnsBlock, dnsRouted []groupSet
	declaredDNS := map[string]bool{}
	for _, g := range groups {
		if g.Disabled {
			continue
		}
		if g.IsCountry() {
			continue
		}
		tag := DomainTag(g)
		if !declaredDNS[tag] {
			sets = append(sets, obj{"type": "local", "tag": tag, "format": "source", "path": filepath.Join(dir, tag+".json")})
			declaredDNS[tag] = true
		}
		gs := groupSet{tags: []string{tag}, target: g.Target, dns: true}
		if g.Target == model.TargetBlock {
			dnsBlock = append(dnsBlock, gs)
		} else {
			dnsRouted = append(dnsRouted, gs)
		}
	}
	sort.Strings(countries)
	return sets, append(append(block, countryBlock...), dnsBlock...), append(append(routed, dnsRouted...), countryRouted...), countries
}

// sourceRules preserves policy order while merging adjacent policies with
// identical routing behavior.
func sourceRules(pols []policy, groups []groupSet, tags map[string]string) []obj {
	outbound := func(target string) string { return outboundTag(target, tags) }
	var rules []obj
	for i := 0; i < len(pols); {
		j := i + 1
		for j < len(pols) && pols[j].mode == pols[i].mode && (pols[i].mode == model.ModeSplit || pols[j].target == pols[i].target) {
			j++
		}
		var src []string
		for _, p := range pols[i:j] {
			src = append(src, p.source.String())
		}
		sort.Strings(src)
		switch pols[i].mode {
		case model.ModeOff:
			rules = append(rules, obj{"source_ip_cidr": src, "outbound": tagDirect})
		case model.ModeFull:
			rules = append(rules, obj{"source_ip_cidr": src, "outbound": outbound(pols[i].target)})
		case model.ModeSplit:
			for _, g := range groups {
				if g.invert || g.dns {
					// A top-level invert would also negate the source condition.
					rules = append(rules, obj{
						"type": "logical", "mode": "and",
						"rules":    []obj{{"source_ip_cidr": src}, g.match()},
						"outbound": outbound(g.target),
					})
					continue
				}
				rules = append(rules, obj{"source_ip_cidr": src, "rule_set": g.tags, "outbound": outbound(g.target)})
			}
			rules = append(rules, obj{"source_ip_cidr": src, "outbound": tagDirect})
		}
		i = j
	}
	return rules
}

// clientClass maps a resolved client policy to its shared address set.
func clientClass(p policy) string {
	switch {
	case p.mode == model.ModeSplit:
		return classSplit
	case p.mode == model.ModeFull && p.target != "" && p.target != model.TargetDirect:
		return classFull + p.target
	default: // off and full-direct policies both bypass the tunnel
		return classOff
	}
}

// clientClasses lists stable class names even when no clients currently occupy them.
func clientClasses(c *model.Config) []string {
	classes := []string{classOff}
	for _, cn := range c.Connections {
		classes = append(classes, classFull+cn.ID)
	}
	return append(classes, classSplit)
}

// clientRouting orders client class rules before network rules: disabled
// clients, full-tunnel classes by connection, then split routing.
func clientRouting(c *model.Config, dir string, groups []groupSet, tags map[string]string) (sets, rules []obj) {
	outbound := func(target string) string { return outboundTag(target, tags) }
	for _, class := range clientClasses(c) {
		sets = append(sets, obj{"type": "local", "tag": class, "format": "source", "path": filepath.Join(dir, class+".json")})
		switch {
		case class == classOff:
			rules = append(rules, obj{"rule_set": []string{class}, "outbound": tagDirect})
		case strings.HasPrefix(class, classFull):
			rules = append(rules, obj{"rule_set": []string{class}, "outbound": outbound(strings.TrimPrefix(class, classFull))})
		case class == classSplit:
			for _, g := range groups {
				// Multiple rule_set values are ORed; use a logical rule for AND.
				rules = append(rules, obj{
					"type": "logical", "mode": "and",
					"rules":    []obj{{"rule_set": []string{class}}, g.match()},
					"outbound": outbound(g.target),
				})
			}
			rules = append(rules, obj{"rule_set": []string{class}, "outbound": tagDirect})
		}
	}
	return sets, rules
}

// clientRuleSets fills stable classes with current addresses and nonmatching empty placeholders.
func clientRuleSets(c *model.Config, clients []policy) (map[string][]byte, error) {
	byClass := map[string][]string{}
	for _, p := range clients {
		cl := clientClass(p)
		byClass[cl] = append(byClass[cl], p.source.String())
	}
	out := map[string][]byte{}
	for _, class := range clientClasses(c) {
		src := byClass[class]
		if len(src) == 0 {
			src = []string{noSource}
		}
		sort.Strings(src)
		raw, err := json.MarshalIndent(obj{"version": 3, "rules": []obj{{"source_ip_cidr": src}}}, "", "  ")
		if err != nil {
			return nil, err
		}
		out[class+".json"] = raw
	}
	return out, nil
}

// outboundTag resolves a target to direct, a fallback selector, or a connection.
func outboundTag(target string, tags map[string]string) string {
	if target == "" || target == model.TargetDirect {
		return tagDirect
	}
	if t, ok := tags[target]; ok {
		return t
	}
	return target
}

// mustPort relies on prior model validation and panics on an invalid listen address.
func mustPort(addr string) uint16 {
	ap, err := netip.ParseAddrPort(addr)
	if err != nil {
		panic(fmt.Sprintf("address %q was not checked by Validate: %v", addr, err))
	}
	return ap.Port()
}

// CountryRuleSet builds geoip-<code>.json with a nonmatching placeholder when
// no prefixes are available; an empty condition would match all traffic.
func CountryRuleSet(prefixes []netip.Prefix) []byte {
	cidrs := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		cidrs = append(cidrs, p.String())
	}
	if len(cidrs) == 0 {
		cidrs = []string{noSource}
	}
	raw, _ := json.Marshal(obj{"version": 3, "rules": []obj{{"ip_cidr": cidrs}}})
	return raw
}
