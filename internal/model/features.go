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
		add("schema_version: поддерживается версия 1, получена %d", c.SchemaVersion)
	}
	d := c.DNS
	if d.Route != "" && d.Route != "direct" && d.Route != "connection" {
		add("dns.route: нужно direct или connection")
	}
	if d.Failure != "" && d.Failure != "strict" && d.Failure != "direct" {
		add("dns.failure: нужно strict или direct")
	}
	if d.Route == "" && (d.Connection != "" || d.Failure != "") {
		add("dns.route: выберите маршрут явно")
	}
	if d.Route != "" {
		if len(d.Upstreams) == 0 {
			add("dns.upstreams: нужен хотя бы один сервер")
		}
		for _, raw := range d.Upstreams {
			u, e := ParseUpstream(raw)
			if e != nil {
				continue
			}
			if d.Route == "connection" && u.Kind == UpstreamUDP {
				add("dns: через соединение нужен DoH или DoT")
			}
			if _, e := netip.ParseAddr(u.Host); e != nil {
				if _, e := netip.ParseAddr(d.Bootstrap[u.Host]); e != nil {
					add("dns.bootstrap: нужен IP для %s", u.Host)
				}
			}
		}
	}
	for host, ip := range d.Bootstrap {
		if _, e := netip.ParseAddr(ip); e != nil {
			add("dns.bootstrap[%s]: неверный IP", host)
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
					add("dns.connection: raw тип %q не гарантирует туннель", typ)
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
			add("dns.connection: неизвестное соединение %q", d.Connection)
		}
		if d.Failure != "direct" {
			for _, id := range c.Chain(d.Connection) {
				if id == TargetDirect {
					add("dns: strict запрещает direct в цепочке соединения")
				}
			}
		}
	} else if d.Connection != "" {
		add("dns.connection: допустимо только при route=connection")
	}
	seen := map[string]bool{}
	if len(c.Filters.Sources) > 8 {
		add("filters: не более 8 источников")
	}
	for _, s := range c.Filters.Sources {
		if s.ID == "" || seen[s.ID] {
			add("filters: пустой/повторяющийся id %q", s.ID)
		}
		seen[s.ID] = true
		u, e := url.Parse(s.URL)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			add("filters[%s]: нужен HTTPS URL без пароля", s.ID)
		}
	}
	for _, s := range c.Filters.Exceptions {
		e, err := ParseEntry(s)
		if err != nil || e.Kind != EntryDomain {
			add("filters.exceptions: нужен домен: %s", s)
		}
	}
	for _, g := range c.Groups {
		if p := g.Preset; p != nil {
			if p.ID == "" || p.Version == "" {
				add("preset: нужны id/version")
			}
			for _, s := range p.Base {
				if _, err := ParseEntry(s); err != nil {
					add("preset.base: %v", err)
				}
			}
		}
	}
}
