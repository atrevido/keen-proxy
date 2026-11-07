package links

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// get looks up a dotted path such as "tls.reality.public_key".
func get(o map[string]any, path string) any {
	var cur any = o
	for _, k := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

func TestParse(t *testing.T) {
	vmess := "vmess://" + base64.StdEncoding.EncodeToString([]byte(`{"v":"2","ps":"vm","add":"vm.example.com","port":"443","id":"11111111-2222-3333-4444-555555555555","aid":"0","net":"ws","host":"cdn.example.com","path":"/ws","tls":"tls"}`))
	cases := []struct {
		link string
		want map[string]any
	}{
		{
			"vless://11111111-2222-3333-4444-555555555555@203.0.113.1:443?encryption=none&flow=xtls-rprx-vision&security=reality&sni=www.microsoft.com&fp=chrome&pbk=PUBKEY&sid=ab12&type=tcp#FI%20Reality",
			map[string]any{"type": "vless", "server": "203.0.113.1", "server_port": 443, "flow": "xtls-rprx-vision",
				"tls.server_name": "www.microsoft.com", "tls.reality.public_key": "PUBKEY", "tls.reality.short_id": "ab12", "tls.utls.fingerprint": "chrome"},
		},
		{
			"vless://uuid@host.example:8443?security=tls&type=ws&path=%2Fws%3Fed%3D2048&host=cdn.example",
			map[string]any{"transport.type": "ws", "transport.path": "/ws", "transport.max_early_data": 2048, "transport.headers.Host": "cdn.example", "tls.server_name": "host.example"},
		},
		{
			"vless://uuid@h.example:443?security=tls&type=grpc&serviceName=svc",
			map[string]any{"transport.type": "grpc", "transport.service_name": "svc"},
		},
		{vmess, map[string]any{"type": "vmess", "server": "vm.example.com", "transport.type": "ws", "transport.path": "/ws", "tls.server_name": "cdn.example.com", "security": "auto"}},
		{"trojan://secret@tr.example:443?type=tcp#t", map[string]any{"type": "trojan", "password": "secret", "tls.enabled": true, "tls.server_name": "tr.example"}},
		{"ss://" + base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:pw")) + "@ss.example:8388#x", map[string]any{"type": "shadowsocks", "method": "aes-256-gcm", "password": "pw", "server_port": 8388}},
		{"ss://" + base64.StdEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pw@1.2.3.4:443")), map[string]any{"method": "chacha20-ietf-poly1305", "server": "1.2.3.4"}},
		{"ss://2022-blake3-aes-128-gcm:a2V5%3D@ss.example:443", map[string]any{"method": "2022-blake3-aes-128-gcm", "password": "a2V5="}},
		{"hysteria2://auth@hy.example:443?sni=hy.example&obfs=salamander&obfs-password=xyz&insecure=1",
			map[string]any{"type": "hysteria2", "password": "auth", "obfs.type": "salamander", "obfs.password": "xyz", "tls.insecure": true, "server_port": 443}},
		{"hy2://pw@hy.example:20000-30000/", map[string]any{"server_ports": []string{"20000:30000"}}},
		{"tuic://uuid:pw@tu.example:443?congestion_control=bbr&alpn=h3&sni=tu.example", map[string]any{"type": "tuic", "uuid": "uuid", "password": "pw", "congestion_control": "bbr"}},
		{"anytls://pw@any.example:443?sni=any.example", map[string]any{"type": "anytls", "password": "pw", "tls.server_name": "any.example"}},
		{"socks5://u:p@10.0.0.1:1080", map[string]any{"type": "socks", "username": "u", "password": "p", "version": "5"}},
	}
	for _, c := range cases {
		o, err := Parse(c.link)
		if err != nil {
			t.Errorf("%s: %v", c.link, err)
			continue
		}
		for path, want := range c.want {
			got := get(o, path)
			gj, _ := json.Marshal(got)
			wj, _ := json.Marshal(want)
			if string(gj) != string(wj) {
				t.Errorf("%s\n  %s = %s, expected %s", c.link, path, gj, wj)
			}
		}
	}
}

func TestParseErrors(t *testing.T) {
	for link, want := range map[string]string{
		"vless://u@h:443?type=xhttp":       "Xray",
		"vless://u@h:443?security=reality": "pbk",
		"wireguard://x":                    "not supported",
		"ss://YWVzOnB3@h:1?plugin=obfs":    "plugins",
		"vless://@h:443":                   "UUID",
		"nonsense":                         "scheme",
	} {
		_, err := Parse(link)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %v, expected to contain %q", link, err, want)
		}
	}
}

func TestName(t *testing.T) {
	if n := Name("vless://u@h:1#%D0%A4%D0%B8%D0%BD%D0%BB%D1%8F%D0%BD%D0%B4%D0%B8%D1%8F"); n != "\u0424\u0438\u043d\u043b\u044f\u043d\u0434\u0438\u044f" {
		t.Errorf("Name = %q", n)
	}
}
