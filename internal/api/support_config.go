package api

import (
	"net/netip"
	"slices"
	"strings"

	"github.com/ordo-systems/keen-proxy/internal/model"
)

type supportFields = map[string]any

// supportConfig exports an explicit policy projection. User-controlled strings
// can only leave this boundary as per-archive aliases or fixed enum values.
func supportConfig(c *model.Config, alias func(kind, value string) string) any {
	p := supportProjection(alias)
	return supportFields{
		"restorable": false, "schema_version": c.SchemaVersion,
		"api": supportFields{
			"listen_port":             supportListenPort(c.API.Listen),
			"tokenless_network_count": len(c.API.NoTokenFrom),
		},
		"lan": supportFields{"interface": p("interface", c.LAN.Interface), "wan": p("interface", c.LAN.WAN)},
		"dns": p.dns(c.DNS),
		"intercept": supportFields{
			"mode":     supportEnum(string(c.Intercept.Mode), string(model.InterceptRedirect), string(model.InterceptTProxy)),
			"tcp_port": c.Intercept.TCPPort, "udp_port": c.Intercept.UDPPort,
			"fwmark": c.Intercept.FWMark, "route_table": c.Intercept.RouteTable,
		},
		"failover": supportFields{
			"interval": c.Failover.Interval, "down_after": c.Failover.DownAfter, "up_after": c.Failover.UpAfter,
		},
		"connections": supportList(c.Connections, p.connection),
		"groups":      supportList(c.Groups, p.group),
		"networks":    supportList(c.Networks, p.network),
		"clients":     supportList(c.Clients, p.client),
		"geoip":       supportFields{"databases": supportList(c.GeoIP.Databases, p.geoDatabase)},
		"filters": supportFields{
			"sources": supportList(c.Filters.Sources, p.filterSource), "exception_count": len(c.Filters.Exceptions),
		},
	}
}

// supportProjection shares the archive's alias namespaces across config sections.
type supportProjection func(kind, value string) string

func supportList[T, U any](items []T, project func(T) U) []U {
	out := make([]U, 0, len(items))
	for _, item := range items {
		out = append(out, project(item))
	}
	return out
}

func supportEnum(value string, allowed ...string) string {
	if recognized := known(value, allowed...); recognized != "" {
		return recognized
	}
	if value == "" && slices.Contains(allowed, "") {
		return ""
	}
	return "unknown"
}

func supportListenPort(value string) uint16 {
	address, _ := netip.ParseAddrPort(value)
	return address.Port()
}

func (p supportProjection) target(value string) string {
	switch value {
	case "", model.TargetDirect, model.TargetBlock:
		return value
	default:
		return p("connection", value)
	}
}

func (p supportProjection) dns(d model.DNS) supportFields {
	return supportFields{
		"route": supportEnum(d.Route, "", "direct", "connection"), "connection": p.target(d.Connection),
		"failure": supportEnum(d.Failure, "", "direct", "strict"), "listen_port": supportListenPort(d.Listen),
		"block_encrypted": d.BlockEncrypted, "firefox_canary": d.FirefoxCanary,
		"system_fallback_configured": d.Fallback != "", "bootstrap_count": len(d.Bootstrap),
		"upstreams": supportList(d.Upstreams, p.upstream),
	}
}

func (p supportProjection) upstream(raw string) supportFields {
	u, err := model.ParseUpstream(raw)
	if err != nil {
		return supportFields{"transport": "unknown"}
	}
	return supportFields{"transport": u.Kind, "address": p.address(u.Host), "port": u.Port}
}

func (p supportProjection) address(host string) string {
	if addr, err := netip.ParseAddr(host); err == nil {
		return p("ip", addr.Unmap().String())
	}
	return p("host", strings.ToLower(strings.TrimSuffix(host, ".")))
}

func (p supportProjection) connection(c model.Connection) supportFields {
	kind := supportEnum(c.Type, model.ConnWireGuard, model.ConnLink, model.ConnSingBox, model.ConnTrustTunnel)
	return supportFields{
		"id": p.target(c.ID), "type": kind, "fallback": p.target(c.Fallback),
		"protocol": supportProtocol(c, kind),
	}
}

func supportProtocol(c model.Connection, kind string) string {
	switch kind {
	case model.ConnWireGuard, model.ConnTrustTunnel:
		return kind
	case model.ConnLink:
		scheme, _, _ := strings.Cut(c.Link, "://")
		return supportEnum(strings.ToLower(scheme), "vless", "vmess", "trojan", "ss", "hysteria2", "hy2", "tuic", "anytls", "socks", "socks5")
	case model.ConnSingBox:
		object := c.Outbound
		if c.Endpoint != nil {
			object = c.Endpoint
		}
		value, _ := object["type"].(string)
		return supportEnum(value, "wireguard", "socks", "http", "shadowsocks", "vmess", "vless", "trojan",
			"tuic", "hysteria", "hysteria2", "ssh", "naive", "anytls")
	default:
		return "unknown"
	}
}

func (p supportProjection) group(g model.Group) supportFields {
	out := supportFields{
		"id": p("group", g.Name), "target": p.target(g.Target),
		"disabled": g.Disabled, "except": g.Except,
		"entries":       supportList(g.Entries, p.entry),
		"country_count": len(g.Countries), "category_count": len(g.Categories),
	}
	if g.Preset != nil {
		out["preset"] = supportFields{"id": p("preset", g.Preset.ID), "auto_update": g.Preset.AutoUpdate}
	}
	return out
}

func (p supportProjection) entry(raw string) supportFields {
	e, err := model.ParseEntry(raw)
	if err != nil {
		return supportFields{"kind": "unknown"}
	}
	if e.Kind == model.EntryDomain {
		return supportFields{"host": p("host", e.Domain)}
	}
	return p.prefix(e.Prefix)
}

func (p supportProjection) prefix(prefix netip.Prefix) supportFields {
	if !prefix.IsValid() {
		return supportFields{"kind": "unknown"}
	}
	if prefix.IsSingleIP() {
		return supportFields{"ip": p("ip", prefix.Addr().Unmap().String())}
	}
	return supportFields{"prefix": p("prefix", prefix.Masked().String()), "bits": prefix.Bits(), "ipv6": prefix.Addr().Is6()}
}

func (p supportProjection) network(n model.Network) supportFields {
	prefix, _ := netip.ParsePrefix(n.CIDR)
	return supportFields{
		"id": p("network", n.Name), "range": p.prefix(prefix),
		"mode": supportMode(n.Mode), "target": p.target(n.Target),
	}
}

func (p supportProjection) client(c model.Client) supportFields {
	ip := ""
	if addr, err := netip.ParseAddr(c.IP); err == nil {
		ip = p("ip", addr.Unmap().String())
	}
	return supportFields{
		"id": p("client", strings.ToLower(c.MAC)), "static_ip": ip,
		"mode": supportMode(c.Mode), "target": p.target(c.Target),
	}
}

func supportMode(mode model.Mode) string {
	return supportEnum(string(mode), string(model.ModeInherit), string(model.ModeSplit), string(model.ModeFull), string(model.ModeOff))
}

func (p supportProjection) geoDatabase(d model.GeoIPDatabase) supportFields {
	return supportFields{
		"id": p("geo-database", d.ID), "enabled": d.Enabled,
		"builtin": d.IsBuiltin(), "category": d.IsCategory(), "local_file": d.File != "",
	}
}

func (p supportProjection) filterSource(s model.FilterSource) supportFields {
	return supportFields{"id": p("filter", s.ID), "enabled": s.Enabled, "auto_update": s.AutoUpdate}
}
