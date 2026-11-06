// Package model defines the canonical keenProxy configuration. internal/compile
// derives sing-box configuration and firewall rules from this model.
package model

import (
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/atrevido/keenetic-proxy/internal/links"
	"github.com/atrevido/keenetic-proxy/internal/trusttunnel"
)

// Built-in route targets may be used instead of a connection ID.
const (
	TargetDirect = "direct"
	TargetBlock  = "block"
)

// Mode controls whether a source uses split routing, a full tunnel, or bypass.
type Mode string

const (
	ModeInherit Mode = ""      // client inherits its network mode
	ModeSplit   Mode = "split" // only matching groups use tunnels
	ModeFull    Mode = "full"  // all traffic uses the selected connection
	ModeOff     Mode = "off"   // kernel routing bypasses keenProxy
)

// InterceptMode selects kernel interception mechanisms for TCP and UDP.
type InterceptMode string

const (
	// InterceptRedirect intercepts TCP with REDIRECT and rejects QUIC.
	InterceptRedirect InterceptMode = "redirect"
	// InterceptTProxy uses REDIRECT for TCP and TPROXY for UDP.
	InterceptTProxy InterceptMode = "tproxy"
)

// Config is the persisted schema shared by the compiler, daemon, and API.
type Config struct {
	SchemaVersion int          `yaml:"schema_version" json:"schema_version"`
	Filters       Filters      `yaml:"filters,omitempty" json:"filters"`
	API           API          `yaml:"api" json:"api"`
	LAN           LAN          `yaml:"lan" json:"lan"`
	DNS           DNS          `yaml:"dns" json:"dns"`
	Intercept     Intercept    `yaml:"intercept" json:"intercept"`
	GeoIP         GeoIP        `yaml:"geoip" json:"geoip"`
	Failover      Failover     `yaml:"failover" json:"failover"`
	Connections   []Connection `yaml:"connections" json:"connections"`
	Groups        []Group      `yaml:"groups" json:"groups"`
	Networks      []Network    `yaml:"networks" json:"networks"`
	Clients       []Client     `yaml:"clients" json:"clients"`
}

// API configures the web interface and HTTP API.
type API struct {
	Listen    string `yaml:"listen" json:"listen"`         // 0.0.0.0:8086
	TokenFile string `yaml:"token_file" json:"token_file"` // login token created on first startup
	// NoTokenFrom permits dashboard access without a token from these CIDR ranges.
	NoTokenFrom []string `yaml:"no_token_from,omitempty" json:"no_token_from,omitempty"`
}

// NoTokenPrefixes parses API.NoTokenFrom, skipping invalid ranges that Validate rejects.
func (a API) NoTokenPrefixes() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range a.NoTokenFrom {
		if p, err := netip.ParsePrefix(strings.TrimSpace(s)); err == nil {
			out = append(out, p.Masked())
		}
	}
	return out
}

// LAN identifies the client-facing and provider network interfaces.
type LAN struct {
	Interface string `yaml:"interface" json:"interface"` // br0
	WAN       string `yaml:"wan" json:"wan"`             // provider interface excluded from interception
}

// DNS configures local interception, upstream transport, and failure policy.
type DNS struct {
	// Route is empty for legacy system fallback, or explicitly direct/connection.
	Route      string `yaml:"route,omitempty" json:"route,omitempty"`
	Connection string `yaml:"connection,omitempty" json:"connection,omitempty"`
	Failure    string `yaml:"failure,omitempty" json:"failure,omitempty"`
	// Bootstrap pins upstream hostnames to IPs without an initial DNS lookup.
	Bootstrap      map[string]string `yaml:"bootstrap,omitempty" json:"bootstrap,omitempty"`
	BlockEncrypted bool              `yaml:"block_encrypted,omitempty" json:"block_encrypted,omitempty"`
	FirefoxCanary  bool              `yaml:"firefox_canary,omitempty" json:"firefox_canary,omitempty"`
	Listen         string            `yaml:"listen" json:"listen"`       // frontend address receiving LAN port 53 DNAT
	Upstreams      []string          `yaml:"upstreams" json:"upstreams"` // https://…, tls://…, udp://… in priority order
	// Fallback is the system resolver (Keenetic ndnproxy) used when all upstreams fail.
	Fallback string `yaml:"fallback" json:"fallback"`
}

// GeoIP configures the country database used by country groups and connection flags.
type GeoIP struct {
	// URL supplies monthly database updates; {month} expands to YYYY-MM.
	// The default source is DB-IP IP to Country Lite (CC BY 4.0).
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
	// File selects a local MMDB with country.iso_code (such as GeoLite2 Country)
	// and disables database downloads.
	File string `yaml:"file,omitempty" json:"file,omitempty"`
}

// Failover configures health probes for connections with Connection.Fallback.
type Failover struct {
	// URL is requested through each connection; any HTTP response counts as success.
	URL string `yaml:"url" json:"url"`
	// Interval is the probe interval in seconds.
	Interval int `yaml:"interval" json:"interval"`
	// DownAfter is the continuous failure duration in seconds before switching to a fallback.
	DownAfter int `yaml:"down_after" json:"down_after"`
	// UpAfter is the continuous success duration in seconds before returning
	// to a higher-priority connection.
	UpAfter int `yaml:"up_after" json:"up_after"`
}

// Intercept assigns listener ports and policy-routing identifiers for interception.
type Intercept struct {
	Mode       InterceptMode `yaml:"mode" json:"mode"`
	TCPPort    uint16        `yaml:"tcp_port" json:"tcp_port"`
	UDPPort    uint16        `yaml:"udp_port" json:"udp_port"`
	FWMark     uint32        `yaml:"fwmark" json:"fwmark"`
	RouteTable int           `yaml:"route_table" json:"route_table"`
}

// Connection types select exactly one protocol description.
const (
	ConnWireGuard   = "wireguard"   // WireGuard through sing-box
	ConnLink        = "link"        // vless://, vmess://, trojan://, ss://, hysteria2://, tuic://, anytls://, socks5:// link
	ConnSingBox     = "singbox"     // raw sing-box outbound or endpoint
	ConnTrustTunnel = "trusttunnel" // TrustTunnel through external trusttunnel_client
)

// Connection defines one outbound protocol and its optional fallback target.
type Connection struct {
	ID        string     `yaml:"id" json:"id"`
	Name      string     `yaml:"name" json:"name"`
	Type      string     `yaml:"type" json:"type"`
	WireGuard *WireGuard `yaml:"wireguard,omitempty" json:"wireguard,omitempty"`
	Link      string     `yaml:"link,omitempty" json:"link,omitempty"`
	// Outbound or Endpoint contains a sing-box object with its tag derived from ID.
	// A singbox connection must set exactly one of these fields.
	Outbound    map[string]any `yaml:"outbound,omitempty" json:"outbound,omitempty"`
	Endpoint    map[string]any `yaml:"endpoint,omitempty" json:"endpoint,omitempty"`
	TrustTunnel *TrustTunnel   `yaml:"trusttunnel,omitempty" json:"trusttunnel,omitempty"`
	// Fallback names another connection or direct. After Failover.DownAfter of
	// failure, traffic follows this chain to the next available member.
	Fallback string `yaml:"fallback,omitempty" json:"fallback,omitempty"`
}

// Chain returns a connection and its fallbacks in priority order, or just the
// connection if none are configured. Validate rejects cyclic chains.
func (c *Config) Chain(id string) []string {
	chain := []string{id}
	for cur := id; ; {
		var next string
		for _, cn := range c.Connections {
			if cn.ID == cur {
				next = cn.Fallback
				break
			}
		}
		if next == "" || slices.Contains(chain, next) {
			return chain
		}
		chain = append(chain, next)
		cur = next
	}
}

// TrustTunnel configures an external client exposing a local SOCKS5 listener.
type TrustTunnel struct {
	Link      string `yaml:"link" json:"link"`                                 // tt://?…
	Binary    string `yaml:"binary,omitempty" json:"binary,omitempty"`         // path to trusttunnel_client
	SocksPort uint16 `yaml:"socks_port,omitempty" json:"socks_port,omitempty"` // local client SOCKS5 port
}

// WireGuard configures a userspace tunnel and its single peer.
type WireGuard struct {
	PrivateKey string   `yaml:"private_key" json:"private_key"`
	Address    []string `yaml:"address" json:"address"`
	MTU        uint32   `yaml:"mtu,omitempty" json:"mtu,omitempty"` // bytes; zero uses the engine default
	Peer       WGPeer   `yaml:"peer" json:"peer"`
}

// WGPeer supplies the remote endpoint, keys, and allowed tunnel destinations.
type WGPeer struct {
	Endpoint     string   `yaml:"endpoint" json:"endpoint"` // host:port
	PublicKey    string   `yaml:"public_key" json:"public_key"`
	PresharedKey string   `yaml:"preshared_key,omitempty" json:"preshared_key,omitempty"`
	AllowedIPs   []string `yaml:"allowed_ips,omitempty" json:"allowed_ips,omitempty"`
	Keepalive    uint16   `yaml:"keepalive,omitempty" json:"keepalive,omitempty"` // seconds; zero omits persistent keepalive
}

// Group routes either domain/address entries or countries to one target.
// Country matching uses GeoIP prefixes; Except negates the country union.
type Group struct {
	Disabled  bool     `yaml:"disabled,omitempty" json:"disabled,omitempty"`
	Preset    *Preset  `yaml:"preset,omitempty" json:"preset,omitempty"`
	Name      string   `yaml:"name" json:"name"`
	Target    string   `yaml:"target" json:"target"` // connection ID, direct, or block
	Entries   []string `yaml:"entries,omitempty" json:"entries"`
	Countries []string `yaml:"countries,omitempty" json:"countries,omitempty"` // uppercase ISO country codes
	Except    bool     `yaml:"except,omitempty" json:"except,omitempty"`
}

// IsCountry reports whether the group matches countries rather than entries.
func (g Group) IsCountry() bool { return len(g.Countries) > 0 }

// Network applies a routing policy to a CIDR unless a client overrides it.
type Network struct {
	Name   string `yaml:"name" json:"name"`
	CIDR   string `yaml:"cidr" json:"cidr"`
	Mode   Mode   `yaml:"mode" json:"mode"`
	Target string `yaml:"target,omitempty" json:"target,omitempty"` // connection for full mode and inherited client targets
}

// Client identifies a device by MAC and optionally overrides its network policy.
type Client struct {
	Name   string `yaml:"name" json:"name"`
	MAC    string `yaml:"mac" json:"mac"`
	IP     string `yaml:"ip,omitempty" json:"ip,omitempty"` // static address; otherwise resolved from ARP/DHCP
	Mode   Mode   `yaml:"mode,omitempty" json:"mode,omitempty"`
	Target string `yaml:"target,omitempty" json:"target,omitempty"`
}

// Defaults fills unset fields and normalizes country codes before validation.
func (c *Config) Defaults() {
	if c.SchemaVersion == 0 {
		c.SchemaVersion = 1
	}
	for i := range c.Groups {
		for j, cc := range c.Groups[i].Countries {
			c.Groups[i].Countries[j] = strings.ToUpper(strings.TrimSpace(cc))
		}
	}
	if c.API.Listen == "" {
		c.API.Listen = "0.0.0.0:8086"
	}
	if c.API.TokenFile == "" {
		c.API.TokenFile = "/opt/etc/keenproxy/token"
	}
	if c.LAN.Interface == "" {
		c.LAN.Interface = "br0"
	}
	if c.DNS.Listen == "" {
		c.DNS.Listen = "0.0.0.0:5354"
	}
	if c.DNS.Fallback == "" {
		c.DNS.Fallback = "127.0.0.1:53"
	}
	if c.Failover.URL == "" {
		// Plain HTTP avoids requiring root certificates that may be absent
		// on the router.
		c.Failover.URL = "http://cp.cloudflare.com/generate_204"
	}
	if c.Failover.Interval == 0 {
		c.Failover.Interval = 15
	}
	if c.Failover.DownAfter == 0 {
		c.Failover.DownAfter = 60
	}
	if c.Failover.UpAfter == 0 {
		c.Failover.UpAfter = 120
	}
	if c.Intercept.Mode == "" {
		c.Intercept.Mode = InterceptRedirect
	}
	if c.Intercept.TCPPort == 0 {
		c.Intercept.TCPPort = 7901
	}
	if c.Intercept.UDPPort == 0 {
		c.Intercept.UDPPort = 7902
	}
	if c.Intercept.FWMark == 0 {
		c.Intercept.FWMark = 0x2000
	}
	if c.Intercept.RouteTable == 0 {
		c.Intercept.RouteTable = 120
	}
	port := uint16(17100)
	for i := range c.Connections {
		tt := c.Connections[i].TrustTunnel
		if tt == nil {
			continue
		}
		if tt.Binary == "" {
			tt.Binary = "/opt/bin/trusttunnel_client"
		}
		if tt.SocksPort == 0 {
			tt.SocksPort = port
		}
		port++
	}
}

// Validate checks the complete configuration and aggregates errors.
func (c *Config) Validate() error {
	var errs []string
	add := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }
	c.validateFeatures(add)

	switch c.Intercept.Mode {
	case InterceptRedirect, InterceptTProxy:
	default:
		add("intercept.mode: неизвестный режим %q", c.Intercept.Mode)
	}
	if c.Intercept.RouteTable < 1 || c.Intercept.RouteTable > 252 {
		// Keenetic BusyBox ip accepts table IDs up to 255; 253–255 are reserved.
		add("intercept.route_table: %d вне диапазона 1–252", c.Intercept.RouteTable)
	}
	if _, err := netip.ParseAddrPort(c.DNS.Listen); err != nil {
		add("dns.listen: %v", err)
	}
	if _, err := netip.ParseAddrPort(c.API.Listen); err != nil {
		add("api.listen: %v", err)
	}
	for i, s := range c.API.NoTokenFrom {
		if _, err := netip.ParsePrefix(strings.TrimSpace(s)); err != nil {
			add("api.no_token_from[%d]: %q — нужна сеть вида 192.168.1.0/24", i, s)
		}
	}
	for i, u := range c.DNS.Upstreams {
		if _, err := ParseUpstream(u); err != nil {
			add("dns.upstreams[%d]: %v", i, err)
		}
	}

	conns := map[string]bool{}
	for i, cn := range c.Connections {
		switch {
		case cn.ID == "":
			add("connections[%d]: пустой id", i)
		case cn.ID == TargetDirect || cn.ID == TargetBlock:
			add("connections[%d]: id %q зарезервирован", i, cn.ID)
		case conns[cn.ID]:
			add("connections[%d]: повтор id %q", i, cn.ID)
		}
		conns[cn.ID] = true
		validateConnection(cn, add)
	}
	for i, cn := range c.Connections {
		where := fmt.Sprintf("connections[%d] %q", i, cn.ID)
		switch {
		case cn.Fallback == "" || cn.Fallback == TargetDirect:
		case cn.Fallback == cn.ID:
			add("%s: резервное не может быть самим соединением", where)
		case cn.Fallback == TargetBlock:
			add("%s: резервным может быть соединение или direct, но не block", where)
		case !conns[cn.Fallback]:
			add("%s: неизвестное резервное %q", where, cn.Fallback)
		default:
			// Chain stops before repeats; a fallback on its final member indicates a cycle.
			chain := c.Chain(cn.ID)
			if next := fallbackOf(c, chain[len(chain)-1]); next != "" {
				add("%s: резервные идут по кругу: %s → %s", where, strings.Join(chain, " → "), next)
			}
		}
	}
	if u, err := url.Parse(c.Failover.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		add("failover.url: нужен адрес http:// или https://, а не %q", c.Failover.URL)
	}
	if c.Failover.Interval < 1 || c.Failover.DownAfter < c.Failover.Interval || c.Failover.UpAfter < c.Failover.Interval {
		add("failover: interval от 1 с, down_after и up_after не меньше interval")
	}
	target := func(where, t string, allowEmpty bool) {
		if t == "" && allowEmpty {
			return
		}
		if t != TargetDirect && t != TargetBlock && !conns[t] {
			add("%s: неизвестная цель %q", where, t)
		}
	}

	for i, g := range c.Groups {
		where := fmt.Sprintf("groups[%d] %q", i, g.Name)
		target(where, g.Target, false)
		if g.IsCountry() && len(g.Entries) > 0 {
			add("%s: в группе либо записи, либо страны — разнесите их по разным группам", where)
		}
		if g.Except && !g.IsCountry() {
			add("%s: «все, кроме» без стран — выберите хотя бы одну страну", where)
		}
		for j, cc := range g.Countries {
			if len(cc) != 2 || cc[0] < 'A' || cc[0] > 'Z' || cc[1] < 'A' || cc[1] > 'Z' {
				add("%s countries[%d]: %q — нужен двухбуквенный код ISO заглавными, например RU", where, j, cc)
			}
		}
		for j, e := range g.Entries {
			if _, err := ParseEntry(e); err != nil {
				add("%s entries[%d]: %v", where, j, err)
			}
		}
	}
	for i, n := range c.Networks {
		where := fmt.Sprintf("networks[%d] %q", i, n.Name)
		if _, err := netip.ParsePrefix(n.CIDR); err != nil {
			add("%s: cidr: %v", where, err)
		}
		validateMode(where, n.Mode, false, add)
		target(where, n.Target, n.Mode != ModeFull)
		if n.Target == TargetBlock {
			add("%s: сеть нельзя целиком направить в block, выключите её (mode: off)", where)
		}
	}
	macs := map[string]bool{}
	for i, cl := range c.Clients {
		where := fmt.Sprintf("clients[%d] %q", i, cl.Name)
		mac, err := net.ParseMAC(cl.MAC)
		if err != nil {
			add("%s: mac: %v", where, err)
		} else if macs[mac.String()] {
			add("%s: повтор mac %s", where, mac)
		} else {
			macs[mac.String()] = true
		}
		if cl.IP != "" {
			if _, err := netip.ParseAddr(cl.IP); err != nil {
				add("%s: ip: %v", where, err)
			}
		}
		validateMode(where, cl.Mode, true, add)
		target(where, cl.Target, true)
		if cl.Target == TargetBlock {
			add("%s: target block для устройства не поддерживается", where)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("конфиг некорректен:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

func fallbackOf(c *Config, id string) string {
	for _, cn := range c.Connections {
		if cn.ID == id {
			return cn.Fallback
		}
	}
	return ""
}

func validateMode(where string, m Mode, allowInherit bool, add func(string, ...any)) {
	switch m {
	case ModeSplit, ModeFull, ModeOff:
	case ModeInherit:
		if !allowInherit {
			add("%s: не задан mode", where)
		}
	default:
		add("%s: неизвестный mode %q", where, m)
	}
}

// validateConnection checks that the selected protocol has exactly one valid description.
func validateConnection(cn Connection, add func(string, ...any)) {
	where := fmt.Sprintf("connection %q", cn.ID)
	set := 0
	for _, has := range []bool{cn.WireGuard != nil, cn.Link != "", cn.Outbound != nil, cn.Endpoint != nil, cn.TrustTunnel != nil} {
		if has {
			set++
		}
	}
	if set > 1 {
		add("%s: задано несколько описаний соединения, нужно одно", where)
	}
	switch cn.Type {
	case ConnWireGuard:
		if cn.WireGuard == nil {
			add("%s: нет секции wireguard", where)
			return
		}
		validateWireGuard(cn.ID, cn.WireGuard, add)
	case ConnLink:
		if cn.Link == "" {
			add("%s: нет link", where)
		} else if _, err := links.Parse(cn.Link); err != nil {
			add("%s: link: %v", where, err)
		}
	case ConnSingBox:
		obj := cn.Outbound
		if obj == nil {
			obj = cn.Endpoint
		}
		if obj == nil {
			add("%s: нужен outbound или endpoint", where)
			return
		}
		if t, _ := obj["type"].(string); t == "" {
			add("%s: в объекте sing-box нет type", where)
		}
		if _, ok := obj["tag"]; ok {
			add("%s: tag не задаётся, он берётся из id", where)
		}
	case ConnTrustTunnel:
		if cn.TrustTunnel == nil {
			add("%s: нет секции trusttunnel", where)
		} else if _, err := trusttunnel.ParseLink(cn.TrustTunnel.Link); err != nil {
			add("%s: %v", where, err)
		}
	default:
		add("%s: неизвестный тип %q (wireguard, link, singbox, trusttunnel)", where, cn.Type)
	}
}

// validateWireGuard checks tunnel addresses, endpoint syntax, and required key fields.
func validateWireGuard(id string, wg *WireGuard, add func(string, ...any)) {
	if wg.PrivateKey == "" {
		add("connection %q: пустой private_key", id)
	}
	if len(wg.Address) == 0 {
		add("connection %q: нет address", id)
	}
	for _, a := range wg.Address {
		if _, err := netip.ParsePrefix(a); err != nil {
			add("connection %q: address %q: %v", id, a, err)
		}
	}
	if _, _, err := net.SplitHostPort(wg.Peer.Endpoint); err != nil {
		add("connection %q: peer.endpoint: %v", id, err)
	}
	if wg.Peer.PublicKey == "" {
		add("connection %q: пустой peer.public_key", id)
	}
}

// Load reads, defaults, and validates a YAML configuration file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse accepts exactly one YAML document, then applies defaults and validation.
func Parse(raw []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("разбор конфига: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("нужен ровно один YAML документ")
	}
	c.Defaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Marshal serializes configuration as YAML for persistence.
func (c *Config) Marshal() ([]byte, error) {
	return yaml.Marshal(c)
}

// Clone deep-copies the small configuration through YAML, including nested
// raw sing-box objects.
func (c *Config) Clone() *Config {
	raw, err := c.Marshal()
	if err != nil {
		panic(err)
	}
	var out Config
	if err := yaml.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return &out
}
