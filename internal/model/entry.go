package model

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// EntryKind distinguishes domain suffixes from address/prefix matches.
type EntryKind int

const (
	EntryDomain EntryKind = iota // domain and all subdomains
	EntryPrefix                  // IP address or network prefix
)

// Entry is a normalized domain or address match used by routing groups.
type Entry struct {
	Kind   EntryKind
	Domain string       // lowercase punycode without a trailing dot
	Prefix netip.Prefix // single addresses use /32 or /128
}

// String returns the canonical form stored in configuration.
func (e Entry) String() string {
	if e.Kind == EntryPrefix {
		return e.Prefix.String()
	}
	return e.Domain
}

// ParseEntry accepts domains ("example.com", "*.example.com", ".example.com"),
// IP addresses, and CIDR prefixes in kvas.list format. Country matching belongs
// in Group.Countries rather than entries.
func ParseEntry(s string) (Entry, error) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if s == "" {
		return Entry{}, fmt.Errorf("пустая запись")
	}
	if l := strings.ToLower(s); strings.HasPrefix(l, "geoip:") || strings.HasPrefix(l, "!geoip:") || strings.HasPrefix(l, "country:") {
		return Entry{}, fmt.Errorf("%q: страны задаются отдельной группой (countries), а не записью", s)
	}
	if p, err := netip.ParsePrefix(s); err == nil {
		return Entry{Kind: EntryPrefix, Prefix: p.Masked()}, nil
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return Entry{Kind: EntryPrefix, Prefix: netip.PrefixFrom(a, a.BitLen())}, nil
	}

	d := strings.TrimPrefix(strings.TrimPrefix(s, "*"), ".")
	d = strings.TrimSuffix(d, ".")
	ascii, err := idna.Lookup.ToASCII(d)
	if err != nil || !strings.Contains(ascii, ".") {
		return Entry{}, fmt.Errorf("не домен, не IP и не подсеть: %q", s)
	}
	return Entry{Kind: EntryDomain, Domain: strings.ToLower(ascii)}, nil
}

// UpstreamKind selects the supported DNS wire transport.
type UpstreamKind string

const (
	UpstreamUDP   UpstreamKind = "udp"
	UpstreamTLS   UpstreamKind = "tls"
	UpstreamHTTPS UpstreamKind = "https"
)

// Upstream is a parsed DNS transport with explicit host, port, and optional DoH path.
type Upstream struct {
	Kind UpstreamKind
	Host string
	Port uint16
	Path string // HTTPS only
}

// ParseUpstream accepts https://host[:port]/path, tls://host[:port],
// udp://host[:port], or a bare IP address (UDP port 53).
func ParseUpstream(s string) (Upstream, error) {
	if a, err := netip.ParseAddr(s); err == nil {
		return Upstream{Kind: UpstreamUDP, Host: a.String(), Port: 53}, nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return Upstream{}, err
	}
	up := Upstream{Kind: UpstreamKind(u.Scheme), Host: u.Hostname()}
	switch up.Kind {
	case UpstreamUDP:
		up.Port = 53
	case UpstreamTLS:
		up.Port = 853
	case UpstreamHTTPS:
		up.Port = 443
		up.Path = u.Path
		if up.Path == "" {
			up.Path = "/dns-query"
		}
	default:
		return Upstream{}, fmt.Errorf("неизвестная схема %q (нужно https, tls или udp)", u.Scheme)
	}
	if up.Host == "" {
		return Upstream{}, fmt.Errorf("нет адреса сервера в %q", s)
	}
	if p := u.Port(); p != "" {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return Upstream{}, fmt.Errorf("порт %q: %w", p, err)
		}
		up.Port = uint16(n)
	}
	return up, nil
}

// Addr returns the host:port used to connect to the upstream.
func (u Upstream) Addr() string {
	return net.JoinHostPort(u.Host, strconv.Itoa(int(u.Port)))
}
