package compile

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"

	"github.com/ordo-systems/keen-proxy/internal/engine"
	"github.com/ordo-systems/keen-proxy/internal/model"
)

const (
	uuid   = "11111111-2222-3333-4444-555555555555"
	zero32 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // base64url of 32 zero bytes
)

// ttLink builds a minimal TrustTunnel link (TLV with single-byte varints).
func ttLink() string {
	var b []byte
	field := func(tag byte, v string) { b = append(append(b, tag, byte(len(v))), v...) }
	field(0x01, "vpn.example.com")
	field(0x02, "203.0.113.50:443")
	field(0x05, "user")
	field(0x06, "pass")
	return "tt://?" + base64.RawURLEncoding.EncodeToString(b)
}

func allConnectionsYAML() string {
	vmess := "vmess://" + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(
		`{"v":"2","add":"vm.example.com","port":"443","id":"%s","net":"ws","path":"/ws","tls":"tls"}`, uuid)))
	ss := "ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:pw")) + "@203.0.113.5:8388"
	return fmt.Sprintf(`
lan: {interface: br0}
dns: {upstreams: ["https://1.1.1.1/dns-query"]}
intercept: {mode: tproxy}
connections:
  - {id: reality, type: link, link: "vless://%[1]s@203.0.113.1:443?security=reality&sni=www.microsoft.com&fp=chrome&pbk=%[2]s&sid=ab12&flow=xtls-rprx-vision"}
  - {id: vless-ws, type: link, link: "vless://%[1]s@cdn.example.com:443?security=tls&type=ws&path=/ws&host=cdn.example.com"}
  - {id: vmess, type: link, link: "%[3]s"}
  - {id: trojan, type: link, link: "trojan://secret@203.0.113.4:443?sni=tr.example.com"}
  - {id: ss, type: link, link: "%[4]s"}
  - {id: hy2, type: link, link: "hysteria2://pw@203.0.113.6:443?sni=hy.example.com&obfs=salamander&obfs-password=x"}
  - {id: hy2-hop, type: link, link: "hy2://pw@hy.example.com:20000-30000?sni=hy.example.com"}
  - {id: tuic, type: link, link: "tuic://%[1]s:pw@203.0.113.7:443?congestion_control=bbr&sni=tu.example.com"}
  - {id: anytls, type: link, link: "anytls://pw@203.0.113.8:443?sni=any.example.com"}
  - {id: socks, type: link, link: "socks5://u:p@203.0.113.9:1080"}
  - id: raw-http
    type: singbox
    outbound: {type: http, server: 203.0.113.10, server_port: 3128}
  - id: raw-wg
    type: singbox
    endpoint:
      type: wireguard
      address: ["10.0.0.2/32"]
      private_key: "%[5]s="
      peers: [{address: 203.0.113.11, port: 51820, public_key: "%[5]s=", allowed_ips: ["0.0.0.0/0"]}]
  - id: tt
    type: trusttunnel
    trusttunnel: {link: "%[6]s"}
groups:
  - {name: All, target: reality, entries: ["example.org"]}
networks:
  - {name: Home, cidr: 192.168.7.0/24, mode: split}
`, uuid, zero32, vmess, ss, zero32, ttLink())
}

func TestAllConnectionTypesAccepted(t *testing.T) {
	cfg, err := model.Parse([]byte(allConnectionsYAML()))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out, err := Compile(Input{Config: cfg, RuleSetDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range out.RuleSets {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := engine.Context(context.Background())
	opts, err := sjson.UnmarshalExtendedContext[option.Options](ctx, out.SingBox)
	if err != nil {
		t.Fatalf("sing-box could not parse configuration: %v", err)
	}
	instance, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatalf("sing-box rejected configuration: %v", err)
	}
	instance.Close()

	var parsed struct {
		Outbounds []map[string]any `json:"outbounds"`
		Endpoints []map[string]any `json:"endpoints"`
	}
	json.Unmarshal(out.SingBox, &parsed)
	types := map[string]string{}
	for _, o := range append(parsed.Outbounds, parsed.Endpoints...) {
		types[o["tag"].(string)] = o["type"].(string)
	}
	for tag, want := range map[string]string{
		"reality": "vless", "vmess": "vmess", "trojan": "trojan", "ss": "shadowsocks", "hy2": "hysteria2",
		"tuic": "tuic", "anytls": "anytls", "socks": "socks", "raw-http": "http", "raw-wg": "wireguard", "tt": "socks",
	} {
		if types[tag] != want {
			t.Errorf("%s: type %q, expected %q", tag, types[tag], want)
		}
	}

	if len(out.TrustTunnel) != 1 || out.TrustTunnel[0].SocksPort != 17100 ||
		!strings.Contains(out.TrustTunnel[0].Config, `address = "127.0.0.1:17100"`) {
		t.Errorf("TrustTunnel: %+v", out.TrustTunnel)
	}
	bypass := map[string]bool{}
	for _, b := range out.Plan.Bypass {
		bypass[b.String()] = true
	}
	for _, ip := range []string{"203.0.113.1/32", "203.0.113.11/32", "203.0.113.50/32"} {
		if !bypass[ip] {
			t.Errorf("server address %s must be in Bypass", ip)
		}
	}
}

func TestBadConnections(t *testing.T) {
	for conn, want := range map[string]string{
		`{id: x, type: link, link: "vless://u@h:443?type=xhttp"}`:           "Xray",
		`{id: x, type: singbox, outbound: {server: h}}`:                     "missing type",
		`{id: x, type: singbox, outbound: {type: http, tag: y}}`:            "tag",
		`{id: x, type: trusttunnel, trusttunnel: {link: "tt://?AQ"}}`:       "truncated",
		`{id: x, type: openvpn}`:                                            "unknown type",
		`{id: x, type: link, link: "socks5://h:1", outbound: {type: http}}`: "multiple",
	} {
		_, err := model.Parse([]byte("connections:\n  - " + conn + "\n"))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, expected %q", conn, err, want)
		}
	}
}
