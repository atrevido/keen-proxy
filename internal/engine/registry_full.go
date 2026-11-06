//go:build !keenproxy_lite

package engine

import (
	"context"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/redirect"
)

// Context registers the protocols available to sing-box.
//
// Outbounds, endpoints, and DNS transports use the full sing-box include set.
// Build tags (see Makefile TAGS) determine supported protocols; excluded
// protocols register stubs that return explanatory errors.
//
// Inbounds are limited to interception and DNS; server protocols are unnecessary.
func Context(ctx context.Context) context.Context {
	in := inbound.NewRegistry()
	redirect.RegisterRedirect(in)
	redirect.RegisterTProxy(in)
	direct.RegisterInbound(in)

	return box.Context(ctx, in,
		include.OutboundRegistry(),
		include.EndpointRegistry(),
		include.DNSTransportRegistry(),
		service.NewRegistry(),
		certificate.NewRegistry())
}
