package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ordo-systems/keen-proxy/internal/model"
)

func TestSupportConfigKeepsPolicyTopologyWithoutExposingUserValues(t *testing.T) {
	c := &model.Config{
		SchemaVersion: 1,
		API:           model.API{Listen: "192.0.2.9:8086", TokenFile: "/opt/PRIVATE-token", NoTokenFrom: []string{"192.0.2.0/24"}},
		LAN:           model.LAN{Interface: "PRIVATE-lan", WAN: "PRIVATE-wan"},
		DNS: model.DNS{
			Route: "connection", Connection: "PRIVATE-main", Failure: "strict",
			Upstreams: []string{"https://PRIVATE-resolver.example:8443/PRIVATE-query?token=PRIVATE-query-token", "198.51.100.8", "PRIVATE-invalid-resolver"},
			Bootstrap: map[string]string{"PRIVATE-resolver.example": "198.51.100.8"},
		},
		Intercept: model.Intercept{Mode: model.InterceptTProxy, TCPPort: 7901},
		Failover:  model.Failover{URL: "https://PRIVATE-probe.example", Interval: 20},
		Connections: []model.Connection{
			{ID: "PRIVATE-main", Name: "PRIVATE-display", Type: model.ConnSingBox,
				Outbound: map[string]any{"type": "vless", "unknown": map[string]any{"value": "PRIVATE-nested-secret"}}, Fallback: "PRIVATE-spare"},
			{ID: "PRIVATE-spare", Type: model.ConnWireGuard, WireGuard: &model.WireGuard{
				PrivateKey: "PRIVATE-wg-key", Peer: model.WGPeer{Endpoint: "PRIVATE-endpoint.example:1234", PresharedKey: "PRIVATE-psk"},
			}, Fallback: model.TargetDirect},
			{ID: "PRIVATE-link", Type: model.ConnLink, Link: "trojan://PRIVATE-password@PRIVATE-remote.example:443"},
			{ID: "PRIVATE-trusttunnel", Type: model.ConnTrustTunnel, TrustTunnel: &model.TrustTunnel{Link: "tt://PRIVATE-credential", Binary: "/opt/PRIVATE-client"}},
			{ID: "PRIVATE-raw-endpoint", Type: model.ConnSingBox, Endpoint: map[string]any{"type": "wireguard", "address": "PRIVATE-raw-address"}},
			{ID: "PRIVATE-unknown", Type: "PRIVATE-unknown-type", Endpoint: map[string]any{"type": "PRIVATE-raw-type"}},
		},
		Groups: []model.Group{
			{Name: "PRIVATE-group", Target: "PRIVATE-main", Entries: []string{"*.Private-store.example. # PRIVATE-comment", "203.0.113.12", "203.0.113.0/24", "PRIVATE-invalid-entry"}, Preset: &model.Preset{ID: "PRIVATE-preset", Version: "PRIVATE-revision", Base: []string{"PRIVATE-base"}, AutoUpdate: true}},
			{Name: "PRIVATE-geo-group", Target: model.TargetBlock, Countries: []string{"US"}, Categories: []string{"PRIVATE-category"}, Except: true},
		},
		Networks: []model.Network{
			{Name: "PRIVATE-network", CIDR: "192.0.2.0/24", Mode: model.ModeFull, Target: "PRIVATE-spare"},
			{Name: "PRIVATE-invalid-network", CIDR: "PRIVATE-invalid-cidr", Mode: "PRIVATE-invalid-mode"},
		},
		Clients: []model.Client{
			{Name: "PRIVATE-person", MAC: "02:00:00:00:00:01", IP: "192.0.2.9", Mode: model.ModeOff},
			{Name: "PRIVATE-auto-address", MAC: "02:00:00:00:00:02", Mode: model.ModeInherit},
		},
		Filters: model.Filters{Sources: []model.FilterSource{{ID: "PRIVATE-filter", Name: "PRIVATE-filter-name", URL: "https://PRIVATE-filter.example", Enabled: true}}, Exceptions: []string{"PRIVATE-exception.example"}},
		GeoIP: model.GeoIP{URL: "https://PRIVATE-legacy.example", File: "/opt/PRIVATE-legacy-file", Databases: []model.GeoIPDatabase{
			{ID: "PRIVATE-geo", Name: "PRIVATE-geo-name", URL: "https://PRIVATE-geo.example", File: "/opt/PRIVATE-geo-file", Kind: "PRIVATE-geo-kind", Enabled: true},
		}},
	}
	alias := func(kind, value string) string {
		switch kind + ":" + value {
		case "connection:PRIVATE-main":
			return "connection-one"
		case "connection:PRIVATE-spare":
			return "connection-two"
		case "host:private-store.example":
			return "host-store"
		case "host:private-resolver.example":
			return "host-resolver"
		case "ip:198.51.100.8":
			return "ip-resolver"
		case "ip:203.0.113.12":
			return "ip-address"
		case "group:PRIVATE-group":
			return "group-store"
		case "network:PRIVATE-network":
			return "network-one"
		case "client:02:00:00:00:00:01":
			return "client-one"
		default:
			return kind + "-alias"
		}
	}
	raw, err := json.Marshal(supportConfig(c, alias))
	if err != nil {
		t.Fatal(err)
	}
	output := string(raw)
	for _, private := range []string{"PRIVATE", "private-store.example", "192.0.2.", "198.51.100.", "203.0.113.", "02:00:00:00:00:"} {
		if strings.Contains(output, private) {
			t.Errorf("projection exposed private value %q: %s", private, output)
		}
	}
	for _, want := range []string{
		`"restorable":false`, `"mode":"tproxy"`, `"mode":"full"`, `"mode":"off"`,
		`"failure":"strict"`, `"transport":"https"`, `"port":8443`, `"transport":"udp"`, `"transport":"unknown"`,
		`"address":"host-resolver"`, `"address":"ip-resolver"`,
		`"id":"connection-one"`, `"connection":"connection-one"`, `"target":"connection-one"`,
		`"id":"connection-two"`, `"fallback":"connection-two"`, `"target":"connection-two"`,
		`"fallback":"direct"`, `"target":"block"`, `"protocol":"vless"`, `"protocol":"trojan"`,
		`"id":"group-store"`, `"host":"host-store"`, `"ip":"ip-address"`, `"id":"network-one"`, `"id":"client-one"`,
		`"auto_update":true`, `"except":true`, `"interval":20`,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("projection lost %s: %s", want, output)
		}
	}
	if c.Connections[1].WireGuard.PrivateKey != "PRIVATE-wg-key" || c.Groups[0].Name != "PRIVATE-group" {
		t.Fatal("projection modified the live configuration")
	}
}
