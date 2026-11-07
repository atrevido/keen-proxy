package api

import (
	"encoding/base64"
	"encoding/json"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"net/http"
	"testing"
)

func TestWireGuardKeyValidation(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	valid := func() *model.WireGuard {
		return &model.WireGuard{PrivateKey: key, Peer: model.WGPeer{PublicKey: key}}
	}
	for _, wg := range []*model.WireGuard{nil, valid(), {PrivateKey: key, Peer: model.WGPeer{PublicKey: key, PresharedKey: key}}} {
		if err := validateWGKeys(wg); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"private", "public", "preshared"} {
		wg := valid()
		switch field {
		case "private":
			wg.PrivateKey = "bad"
		case "public":
			wg.Peer.PublicKey = "bad"
		case "preshared":
			wg.Peer.PresharedKey = "bad"
		}
		if err := validateWGKeys(wg); err == nil {
			t.Fatalf("accepted invalid %s key", field)
		}
	}
	if validWGKey(base64.StdEncoding.EncodeToString(make([]byte, 31))) {
		t.Fatal("accepted short key")
	}
}

func TestConfigValidationRejectsInvalidEngineProfiles(t *testing.T) {
	for _, profile := range []model.Connection{
		{ID: "bad", Name: "bad", Type: model.ConnSingBox, Outbound: map[string]any{"type": "not-a-protocol"}},
		{ID: "bad", Name: "bad", Type: model.ConnSingBox, Outbound: map[string]any{"type": "vless", "server": "vpn.example.test", "server_port": 443, "uuid": "bad"}},
		{ID: "bad", Name: "bad", Type: model.ConnWireGuard, WireGuard: &model.WireGuard{PrivateKey: "bad", Address: []string{"10.1.0.2/32"}, Peer: model.WGPeer{Endpoint: "vpn.example.test:51820", PublicKey: "bad"}}},
	} {
		s, d := newServer()
		d.cfg.Connections = []model.Connection{profile}
		raw, _ := json.Marshal(d.cfg)
		w := do(t, s.Handler(), "POST", "/api/config/validate", string(raw), "secret", lan)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid profile accepted: %s / %s", profile.Type, w.Body.String())
		}
	}
}

func TestConfigValidationAcceptsWizardProtocols(t *testing.T) {
	for _, o := range []map[string]any{
		{"type": "vless", "server": "vpn.example.test", "server_port": 443, "uuid": "00000000-0000-4000-8000-000000000000", "tls": map[string]any{"enabled": true, "server_name": "cover.example.test", "utls": map[string]any{"enabled": true, "fingerprint": "chrome"}, "reality": map[string]any{"enabled": true, "public_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "short_id": "ab12"}}},
		{"type": "trojan", "server": "vpn.example.test", "server_port": 443, "password": "example", "tls": map[string]any{"enabled": true}},
		{"type": "hysteria2", "server": "vpn.example.test", "server_port": 443, "password": "example", "tls": map[string]any{"enabled": true}},
		{"type": "shadowsocks", "server": "vpn.example.test", "server_port": 443, "method": "aes-128-gcm", "password": "example"},
		{"type": "socks", "server": "vpn.example.test", "server_port": 1080, "version": "5"},
	} {
		s, d := newServer()
		d.cfg.Connections = []model.Connection{{ID: "one", Name: "One", Type: model.ConnSingBox, Outbound: o}}
		raw, _ := json.Marshal(d.cfg)
		w := do(t, s.Handler(), "POST", "/api/config/validate", string(raw), "secret", lan)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", o["type"], w.Code, w.Body.String())
		}
	}
}
