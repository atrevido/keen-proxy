package model

import (
	"net/netip"
	"net/url"
)

// Preset records catalog provenance; Base preserves the installed entries for
// three-way updates that retain local edits.
type Preset struct {
	ID         string   `yaml:"id" json:"id"`
	Version    string   `yaml:"version" json:"version"`
	Base       []string `yaml:"base" json:"base"`
	AutoUpdate bool     `yaml:"auto_update,omitempty" json:"auto_update"`
}

// Filters selects downloadable DNS blocklists and local domain exceptions.
type Filters struct {
	Sources    []FilterSource `yaml:"sources,omitempty" json:"sources"`
	Exceptions []string       `yaml:"exceptions,omitempty" json:"exceptions"`
}

// FilterSource identifies one HTTPS blocklist and its refresh policy.
type FilterSource struct {
	ID         string `yaml:"id" json:"id"`
	Name       string `yaml:"name" json:"name"`
	URL        string `yaml:"url" json:"url"`
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	AutoUpdate bool   `yaml:"auto_update,omitempty" json:"auto_update"`
}

// validateFeatures checks schema, DNS route safety, filter sources, and preset metadata.
func (c *Config) validateFeatures(add func(string, ...any)) {
	if c.SchemaVersion != 1 {
		add("schema_version: version 1 is supported, got %d", c.SchemaVersion)
	}
	d := c.DNS
	if d.Route != "" && d.Route != "direct" && d.Route != "connection" {
		add("dns.route: expected direct or connection")
	}
	if d.Failure != "" && d.Failure != "strict" && d.Failure != "direct" {
		add("dns.failure: expected strict or direct")
	}
	if d.Route == "" && (d.Connection != "" || d.Failure != "") {
		add("dns.route: select a route explicitly")
	}
	if d.Route != "" {
		if len(d.Upstreams) == 0 {
			add("dns.upstreams: at least one server is required")
		}
		for _, raw := range d.Upstreams {
			u, e := ParseUpstream(raw)
			if e != nil {
				continue
			}
			if d.Route == "connection" && u.Kind == UpstreamUDP {
				add("dns: routing through a connection requires DoH or DoT")
			}
			if _, e := netip.ParseAddr(u.Host); e != nil {
				if _, e := netip.ParseAddr(d.Bootstrap[u.Host]); e != nil {
					add("dns.bootstrap: IP required for %s", u.Host)
				}
			}
		}
	}
	for host, ip := range d.Bootstrap {
		if _, e := netip.ParseAddr(ip); e != nil {
			add("dns.bootstrap[%s]: invalid IP", host)
		}
	}
	if d.Route == "connection" {
		// Routing containers may hide a direct exit under a non-reserved tag.
		for _, id := range c.Chain(d.Connection) {
			for _, cn := range c.Connections {
				if cn.ID != id || cn.Type != ConnSingBox {
					continue
				}
				typ, _ := cn.Outbound["type"].(string)
				if cn.Endpoint != nil {
					typ, _ = cn.Endpoint["type"].(string)
				}
				switch typ {
				case "wireguard", "socks", "http", "shadowsocks", "vmess", "vless", "trojan", "tuic", "hysteria", "hysteria2", "ssh", "naive", "anytls":
				default:
					add("dns.connection: raw type %q does not guarantee a tunnel", typ)
				}
			}
		}

		found := false
		for _, cn := range c.Connections {
			if cn.ID == d.Connection {
				found = true
			}
		}
		if !found {
			add("dns.connection: unknown connection %q", d.Connection)
		}
		if d.Failure != "direct" {
			for _, id := range c.Chain(d.Connection) {
				if id == TargetDirect {
					add("dns: strict forbids direct in the connection chain")
				}
			}
		}
	} else if d.Connection != "" {
		add("dns.connection: only allowed with route=connection")
	}
	seen := map[string]bool{}
	if len(c.Filters.Sources) > 8 {
		add("filters: at most 8 sources")
	}
	for _, s := range c.Filters.Sources {
		if s.ID == "" || seen[s.ID] {
			add("filters: empty or duplicate id %q", s.ID)
		}
		seen[s.ID] = true
		u, e := url.Parse(s.URL)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			add("filters[%s]: HTTPS URL without a password is required", s.ID)
		}
	}
	for _, s := range c.Filters.Exceptions {
		e, err := ParseEntry(s)
		if err != nil || e.Kind != EntryDomain {
			add("filters.exceptions: domain required: %s", s)
		}
	}
	for _, g := range c.Groups {
		if p := g.Preset; p != nil {
			if p.ID == "" || p.Version == "" {
				add("preset: id/version required")
			}
			for _, s := range p.Base {
				if _, err := ParseEntry(s); err != nil {
					add("preset.base: %v", err)
				}
			}
		}
	}
}
