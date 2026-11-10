package compile

import (
	"fmt"
	"maps"
	"net"
	"net/netip"
	"strconv"

	"github.com/ordo-systems/keen-proxy/internal/amneziawg"
	"github.com/ordo-systems/keen-proxy/internal/links"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/ordo-systems/keen-proxy/internal/trusttunnel"
)

// TrustTunnelClient describes an external trusttunnel_client process owned by the daemon.
type TrustTunnelClient struct {
	ID        string
	Binary    string
	SocksPort uint16
	Config    string // trusttunnel_client.toml
}

// connOut collects protocol objects and runtime dependencies for all connections.
// AmneziaWGClient identifies one local TCP proxy and its private profile.
type AmneziaWGClient struct {
	ID        string
	SocksPort uint16
	Profile   string
	ClientID  string
}

type connOut struct {
	amneziaWG   []AmneziaWGClient
	outbounds   []obj
	endpoints   []obj
	trustTunnel []TrustTunnelClient
	servers     []netip.Addr      // server addresses excluded from LAN interception
	hosts       map[string]string // connection ID to server address or hostname for country lookup
}

// connections assigns stable tags and collects server addresses for interception bypass.
func connections(c *model.Config) (connOut, error) {
	return connectionsWithAWGPassword(c, "")
}

func connectionsWithAWGPassword(c *model.Config, password string) (connOut, error) {
	out := connOut{hosts: map[string]string{}}
	var current string
	addServer := func(host string) {
		if _, seen := out.hosts[current]; !seen && host != "" {
			out.hosts[current] = host
		}
		if a, err := netip.ParseAddr(host); err == nil {
			out.servers = append(out.servers, a.Unmap())
		}
	}
	for _, cn := range c.Connections {
		current = cn.ID
		switch cn.Type {
		case model.ConnWireGuard:
			out.endpoints = append(out.endpoints, wireGuard(cn))
			host, _, _ := net.SplitHostPort(cn.WireGuard.Peer.Endpoint)
			addServer(host)
		case model.ConnLink:
			o, err := links.Parse(cn.Link)
			if err != nil {
				return out, fmt.Errorf("connection %q: %w", cn.ID, err)
			}
			o["tag"] = cn.ID
			out.outbounds = append(out.outbounds, o)
			if host, ok := o["server"].(string); ok {
				addServer(host)
			}
		case model.ConnSingBox:
			if cn.Outbound != nil {
				o := maps.Clone(cn.Outbound)
				o["tag"] = cn.ID
				out.outbounds = append(out.outbounds, o)
				if host, ok := o["server"].(string); ok {
					addServer(host)
				}
			} else {
				e := maps.Clone(cn.Endpoint)
				e["tag"] = cn.ID
				out.endpoints = append(out.endpoints, e)
				peers, _ := e["peers"].([]any)
				for _, p := range peers {
					if pm, ok := p.(map[string]any); ok {
						if host, ok := pm["address"].(string); ok {
							addServer(host)
						}
					}
				}
			}
		case model.ConnAmneziaWG:
			awg := cn.AmneziaWG
			profile, err := amneziawg.Parse(awg.Profile)
			if err != nil {
				return out, fmt.Errorf("connection %q: %w", cn.ID, err)
			}
			out.amneziaWG = append(out.amneziaWG, AmneziaWGClient{ID: cn.ID, SocksPort: awg.SocksPort, Profile: profile.Config, ClientID: profile.ClientID})
			socks := obj{"type": "socks", "tag": cn.ID, "server": "127.0.0.1", "server_port": awg.SocksPort, "version": "5", "network": "tcp"}
			if password != "" {
				socks["username"], socks["password"] = "keenproxy", password
			}
			out.outbounds = append(out.outbounds, socks)
			// Only LAN traffic is intercepted. The sidecar's own UDP does not require a bypass.
			out.hosts[cn.ID], _, _ = net.SplitHostPort(profile.Endpoint)
		case model.ConnTrustTunnel:
			tt := cn.TrustTunnel
			ep, err := trusttunnel.ParseLink(tt.Link)
			if err != nil {
				return out, fmt.Errorf("connection %q: %w", cn.ID, err)
			}
			socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(tt.SocksPort)))
			out.trustTunnel = append(out.trustTunnel, TrustTunnelClient{
				ID: cn.ID, Binary: tt.Binary, SocksPort: tt.SocksPort, Config: trusttunnel.ClientConfig(ep, socks),
			})
			out.outbounds = append(out.outbounds, obj{
				"type": "socks", "tag": cn.ID, "server": "127.0.0.1", "server_port": tt.SocksPort, "version": "5",
			})
			for _, a := range ep.Addresses {
				if host, _, err := net.SplitHostPort(a); err == nil {
					addServer(host)
				}
			}
		}
	}
	return out, nil
}

// wireGuard translates a validated tunnel into a sing-box userspace endpoint.
func wireGuard(cn model.Connection) obj {
	wg := cn.WireGuard
	host, port, _ := net.SplitHostPort(wg.Peer.Endpoint)
	p, _ := strconv.Atoi(port)
	allowed := wg.Peer.AllowedIPs
	if len(allowed) == 0 {
		allowed = []string{"0.0.0.0/0", "::/0"}
	}
	peer := obj{"address": host, "port": p, "public_key": wg.Peer.PublicKey, "allowed_ips": allowed}
	if wg.Peer.PresharedKey != "" {
		peer["pre_shared_key"] = wg.Peer.PresharedKey
	}
	if wg.Peer.Keepalive > 0 {
		peer["persistent_keepalive_interval"] = wg.Peer.Keepalive
	}
	ep := obj{"type": "wireguard", "tag": cn.ID, "address": wg.Address, "private_key": wg.PrivateKey, "peers": []obj{peer}}
	if wg.MTU > 0 {
		ep["mtu"] = wg.MTU
	}
	return ep
}
