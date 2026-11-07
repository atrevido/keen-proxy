//go:build keenproxy_lite

package engine

import (
	"context"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport"
	"github.com/sagernet/sing-box/dns/transport/hosts"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/protocol/block"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/group"
	"github.com/sagernet/sing-box/protocol/redirect"
	"github.com/sagernet/sing-box/protocol/shadowsocks"
	"github.com/sagernet/sing-box/protocol/socks"
	"github.com/sagernet/sing-box/protocol/trojan"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing-box/protocol/wireguard"
)

// BuildVariant identifies the protocol registry compiled into this executable.
const BuildVariant = "lite"

// Context registers WireGuard, VLESS, Trojan, Shadowsocks, and SOCKS (including
// TrustTunnel support) for memory-constrained keenproxy_lite builds. Other
// protocol types are rejected as unknown.
func Context(ctx context.Context) context.Context {
	in := inbound.NewRegistry()
	redirect.RegisterRedirect(in)
	redirect.RegisterTProxy(in)
	direct.RegisterInbound(in)

	out := outbound.NewRegistry()
	direct.RegisterOutbound(out)
	block.RegisterOutbound(out)
	group.RegisterSelector(out)
	group.RegisterURLTest(out)
	socks.RegisterOutbound(out)
	shadowsocks.RegisterOutbound(out)
	trojan.RegisterOutbound(out)
	vless.RegisterOutbound(out)

	ep := endpoint.NewRegistry()
	wireguard.RegisterEndpoint(ep)

	dnsReg := dns.NewTransportRegistry()
	transport.RegisterUDP(dnsReg)
	transport.RegisterTCP(dnsReg)
	transport.RegisterTLS(dnsReg)
	transport.RegisterHTTPS(dnsReg)
	hosts.RegisterTransport(dnsReg)
	local.RegisterTransport(dnsReg)

	return box.Context(ctx, in, out, ep, dnsReg, service.NewRegistry(), certificate.NewRegistry())
}
