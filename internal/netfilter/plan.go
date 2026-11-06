// Package netfilter renders and applies kernel traffic interception rules.
//
// Rules use dedicated KEENPROXY_* chains loaded with iptables-restore --noflush
// atomically per table. Built-in chain jumps are installed idempotently, so
// Apply can restore rules after NDM rebuilds without touching unrelated rules.
package netfilter

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

const (
	ChainDNSGuard = "KEENPROXY_DNS_GUARD"
	ChainDNS      = "KEENPROXY_DNS"  // nat: LAN client DNS to the keenProxy frontend
	ChainTCP      = "KEENPROXY_TCP"  // nat: participating TCP to sing-box redirect inbound
	ChainUDP      = "KEENPROXY_UDP"  // mangle: participating UDP to sing-box TPROXY inbound
	ChainQUIC     = "KEENPROXY_QUIC" // filter: reject QUIC to encourage TCP fallback
)

// Plan contains interception inputs produced by internal/compile.
type Plan struct {
	BlockEncryptedDNS bool
	Mode              model.InterceptMode
	LAN               string // client ingress interface, such as br0
	DNSPort           uint16
	TCPPort           uint16
	UDPPort           uint16
	FWMark            uint32
	// Sources lists participating addresses and networks; other devices bypass
	// userspace interception entirely.
	Sources []netip.Prefix
	// Excluded bypasses individual devices inside Sources (off mode).
	Excluded []netip.Addr
	// Bypass lists destinations that must never be intercepted.
	Bypass []netip.Prefix
}

// Jump connects a built-in chain to an owned interception chain.
type Jump struct {
	Table, From, To string
}

// DefaultBypass returns private, special-purpose, and multicast IPv4 ranges.
func DefaultBypass() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/4", "240.0.0.0/4",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}

// Jumps returns installation order; each jump is inserted at the head of its chain.
func (p Plan) Jumps() []Jump {
	j := []Jump{
		{"nat", "PREROUTING", ChainTCP},
		{"nat", "PREROUTING", ChainDNS},
		{"filter", "FORWARD", ChainQUIC},
	}
	// Always present so toggling the guard cannot leave an orphan INPUT jump.
	j = append(j, Jump{"filter", "INPUT", ChainDNSGuard}, Jump{"filter", "FORWARD", ChainDNSGuard})
	if p.Mode == model.InterceptTProxy {
		j = append(j, Jump{"mangle", "PREROUTING", ChainUDP})
	}
	return j
}

// Render builds iptables-restore --noflush input. Declaring a chain
// (":NAME - [0:0]") creates or flushes it without altering other chains.
func (p Plan) Render() string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	in := "-i " + p.LAN

	w("*nat")
	w(":%s - [0:0]", ChainDNS)
	w(":%s - [0:0]", ChainTCP)
	for _, proto := range []string{"udp", "tcp"} {
		w("-A %s %s -p %s -m %s --dport 53 -j REDIRECT --to-ports %d", ChainDNS, in, proto, proto, p.DNSPort)
	}
	if len(p.Sources) > 0 {
		for _, d := range p.Bypass {
			w("-A %s -d %s -j RETURN", ChainTCP, d)
		}
		for _, e := range p.Excluded {
			w("-A %s -s %s -j RETURN", ChainTCP, host(e))
		}
		for _, s := range p.Sources {
			w("-A %s %s -s %s -p tcp -j REDIRECT --to-ports %d", ChainTCP, in, s, p.TCPPort)
		}
	}
	w("COMMIT")

	if p.Mode == model.InterceptTProxy {
		mark := fmt.Sprintf("0x%x/0x%x", p.FWMark, p.FWMark)
		w("*mangle")
		w(":%s - [0:0]", ChainUDP)
		if len(p.Sources) > 0 {
			for _, d := range p.Bypass {
				w("-A %s -d %s -j RETURN", ChainUDP, d)
			}
			w("-A %s -p udp -m udp --dport 53 -j RETURN", ChainUDP)
			w("-A %s -p udp -m udp --dport 443 -j RETURN", ChainUDP) // QUIC is rejected in the filter table
			for _, e := range p.Excluded {
				w("-A %s -s %s -j RETURN", ChainUDP, host(e))
			}
			for _, s := range p.Sources {
				w("-A %s %s -s %s -p udp -j TPROXY --on-port %d --on-ip 127.0.0.1 --tproxy-mark %s",
					ChainUDP, in, s, p.UDPPort, mark)
			}
		}
		w("COMMIT")
	}

	w("*filter")
	w(":%s - [0:0]", ChainQUIC)
	w(":%s - [0:0]", ChainDNSGuard)
	if p.BlockEncryptedDNS {
		for _, proto := range []string{"tcp", "udp"} {
			w("-A %s %s -p %s --dport 853 -j REJECT", ChainDNSGuard, in, proto)
			w("-A %s %s -p %s -m conntrack --ctdir ORIGINAL --ctorigdstport 853 -j REJECT", ChainDNSGuard, in, proto)
		}
	}
	for _, e := range p.Excluded {
		w("-A %s -s %s -j RETURN", ChainQUIC, host(e))
	}
	for _, s := range p.Sources {
		w("-A %s %s -s %s -p udp -m udp --dport 443 -j REJECT --reject-with icmp-port-unreachable", ChainQUIC, in, s)
	}
	w("COMMIT")
	return b.String()
}

func host(a netip.Addr) string {
	return netip.PrefixFrom(a, a.BitLen()).String()
}
