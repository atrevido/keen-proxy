package netfilter

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

func testPlan(mode model.InterceptMode) Plan {
	return Plan{
		Mode: mode, LAN: "br0", DNSPort: 5354, TCPPort: 7901, UDPPort: 7902, FWMark: 0x2000,
		Sources:  []netip.Prefix{netip.MustParsePrefix("192.168.7.104/32")},
		Excluded: []netip.Addr{netip.MustParseAddr("192.168.7.88")},
		Bypass:   []netip.Prefix{netip.MustParsePrefix("192.168.0.0/16")},
	}
}

func TestRenderTProxy(t *testing.T) {
	want := `*nat
:KEENPROXY_DNS - [0:0]
:KEENPROXY_TCP - [0:0]
-A KEENPROXY_DNS -i br0 -p udp -m udp --dport 53 -j REDIRECT --to-ports 5354
-A KEENPROXY_DNS -i br0 -p tcp -m tcp --dport 53 -j REDIRECT --to-ports 5354
-A KEENPROXY_TCP -d 192.168.0.0/16 -j RETURN
-A KEENPROXY_TCP -s 192.168.7.88/32 -j RETURN
-A KEENPROXY_TCP -i br0 -s 192.168.7.104/32 -p tcp -j REDIRECT --to-ports 7901
COMMIT
*mangle
:KEENPROXY_UDP - [0:0]
-A KEENPROXY_UDP -d 192.168.0.0/16 -j RETURN
-A KEENPROXY_UDP -p udp -m udp --dport 53 -j RETURN
-A KEENPROXY_UDP -p udp -m udp --dport 443 -j RETURN
-A KEENPROXY_UDP -s 192.168.7.88/32 -j RETURN
-A KEENPROXY_UDP -i br0 -s 192.168.7.104/32 -p udp -j TPROXY --on-port 7902 --on-ip 127.0.0.1 --tproxy-mark 0x2000/0x2000
COMMIT
*filter
:KEENPROXY_QUIC - [0:0]
:KEENPROXY_DNS_GUARD - [0:0]
-A KEENPROXY_QUIC -s 192.168.7.88/32 -j RETURN
-A KEENPROXY_QUIC -i br0 -s 192.168.7.104/32 -p udp -m udp --dport 443 -j REJECT --reject-with icmp-port-unreachable
COMMIT
`
	if got := testPlan(model.InterceptTProxy).Render(); got != want {
		t.Errorf("Render() =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderRedirectHasNoMangle(t *testing.T) {
	p := testPlan(model.InterceptRedirect)
	got := p.Render()
	for _, bad := range []string{"*mangle", "TPROXY"} {
		if strings.Contains(got, bad) {
			t.Errorf("redirect mode must not contain %q:\n%s", bad, got)
		}
	}
	for _, j := range p.Jumps() {
		if j.Table == "mangle" {
			t.Errorf("unexpected jump %+v", j)
		}
	}
}

func TestRenderNoSourcesKeepsDNSOnly(t *testing.T) {
	p := testPlan(model.InterceptTProxy)
	p.Sources = nil
	got := p.Render()
	if strings.Contains(got, "REDIRECT --to-ports 7901") || strings.Contains(got, "TPROXY") || strings.Contains(got, "REJECT") {
		t.Errorf("traffic must not be intercepted without participants:\n%s", got)
	}
	if !strings.Contains(got, "--dport 53 -j REDIRECT --to-ports 5354") {
		t.Errorf("DNS must always be intercepted:\n%s", got)
	}
}
