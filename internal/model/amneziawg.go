package model

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"net/netip"
	"slices"

	"github.com/ordo-systems/keen-proxy/internal/amneziawg"
)

// AmneziaWG stores a sanitized native profile and the sidecar's stable TCP port.
type AmneziaWG struct {
	Profile   string `yaml:"profile" json:"profile"`
	SocksPort uint16 `yaml:"socks_port,omitempty" json:"socks_port,omitempty"`
}

// occupiedAWGPorts reserves ports already used by other managed listeners.
func (c *Config) occupiedAWGPorts() map[uint16]bool {
	dns, _ := netip.ParseAddrPort(c.DNS.Listen)
	api, _ := netip.ParseAddrPort(c.API.Listen)
	ports := map[uint16]bool{}
	for _, port := range []uint16{dns.Port(), api.Port(), c.Intercept.TCPPort, c.Intercept.UDPPort} {
		ports[port] = true
	}
	for _, cn := range c.Connections {
		if cn.TrustTunnel != nil {
			ports[cn.TrustTunnel.SocksPort] = true
		}
	}
	return ports
}

func (c *Config) defaultAmneziaWG() {
	ports := c.occupiedAWGPorts()
	for _, cn := range c.Connections {
		if cn.AmneziaWG != nil && cn.AmneziaWG.SocksPort != 0 {
			ports[cn.AmneziaWG.SocksPort] = true
		}
	}
	port := 18100
	for _, cn := range c.Connections {
		awg := cn.AmneziaWG
		if awg == nil {
			continue
		}
		if parsed, err := amneziawg.Parse(awg.Profile); err == nil {
			awg.Profile = parsed.Config
		}
		if awg.SocksPort == 0 {
			for port <= 65535 && ports[uint16(port)] {
				port++
			}
			if port <= 65535 {
				awg.SocksPort = uint16(port)
				ports[awg.SocksPort] = true
			}
		}
	}
}

func (c *Config) validateAmneziaWG(add func(string, ...any)) {
	ports := c.occupiedAWGPorts()
	clients := map[string]bool{}
	for _, cn := range c.Connections {
		awg := cn.AmneziaWG
		if awg == nil {
			continue
		}
		profile, err := amneziawg.Parse(awg.Profile)
		if err != nil {
			add("connection %q: %v", cn.ID, err)
		} else {
			if clients[profile.ClientID] {
				add("connection %q: AmneziaWG client identity is already in use", cn.ID)
			}
			clients[profile.ClientID] = true
			if c.UsesWireGuardIdentity(profile.ClientID) {
				add("connection %q: AmneziaWG reuses a WireGuard client identity", cn.ID)
			}
		}
		if awg.SocksPort < 1024 || ports[awg.SocksPort] {
			add("connection %q: AmneziaWG SOCKS port must be unprivileged and unique", cn.ID)
		}
		ports[awg.SocksPort] = true
		if slices.Contains(c.Chain(cn.ID), TargetDirect) {
			add("connection %q: AmneziaWG does not allow a direct fallback", cn.ID)
		}
	}
}

// UsesWireGuardIdentity prevents an AWG test from roaming an existing WireGuard peer.
func (c *Config) UsesWireGuardIdentity(id string) bool {
	for _, cn := range c.Connections {
		if cn.WireGuard != nil && wgIdentity(cn.WireGuard.PrivateKey) == id {
			return true
		}
		for _, raw := range []map[string]any{cn.Endpoint, cn.Outbound} {
			if raw["type"] == "wireguard" {
				key, _ := raw["private_key"].(string)
				if wgIdentity(key) == id {
					return true
				}
			}
		}
	}
	return false
}

func wgIdentity(key string) string {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return ""
	}
	private, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(private.PublicKey().Bytes())
}
