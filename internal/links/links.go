// Package links converts connection links into sing-box outbounds using
// conventions shared by v2rayN, Xray, and Shadowrocket.
package links

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Outbound is a raw sing-box outbound object whose tag is assigned during compilation.
type Outbound = map[string]any

// Parse decodes a connection link; the compiler assigns the outbound tag later.
func Parse(link string) (Outbound, error) {
	link = strings.TrimSpace(link)
	scheme, _, ok := strings.Cut(link, "://")
	if !ok {
		return nil, fmt.Errorf("не ссылка: нет схемы")
	}
	switch strings.ToLower(scheme) {
	case "vless":
		return parseVLESS(link)
	case "vmess":
		return parseVMess(link)
	case "trojan":
		return parseTrojan(link)
	case "ss":
		return parseShadowsocks(link)
	case "hysteria2", "hy2":
		return parseHysteria2(link)
	case "tuic":
		return parseTUIC(link)
	case "anytls":
		return parseAnyTLS(link)
	case "socks", "socks5":
		return parseSocks(link)
	default:
		return nil, fmt.Errorf("схема %q не поддерживается ссылками; используйте type: singbox с готовым outbound", scheme)
	}
}

// Name returns the link fragment as a suggested UI display name.
func Name(link string) string {
	if i := strings.LastIndexByte(link, '#'); i >= 0 {
		if n, err := url.PathUnescape(link[i+1:]); err == nil {
			return n
		}
	}
	return ""
}

func server(u *url.URL) (string, int, error) {
	host := u.Hostname()
	if host == "" {
		return "", 0, fmt.Errorf("нет адреса сервера")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("неверный порт %q", u.Port())
	}
	return host, port, nil
}

func truthy(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes":
		return true
	}
	return false
}

func splitList(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// tlsOptions maps Xray-style security, sni, fp, alpn, allowInsecure/insecure,
// and REALITY pbk/sid parameters to sing-box TLS options.
func tlsOptions(q url.Values, defaultSNI string, forceTLS bool) (map[string]any, error) {
	security := strings.ToLower(q.Get("security"))
	if security == "" && forceTLS {
		security = "tls"
	}
	switch security {
	case "", "none":
		return nil, nil
	case "tls", "reality", "xtls":
	default:
		return nil, fmt.Errorf("security=%q не поддерживается", security)
	}
	t := map[string]any{"enabled": true}
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("peer")
	}
	if sni == "" {
		sni = defaultSNI
	}
	if sni != "" {
		t["server_name"] = sni
	}
	if truthy(q.Get("allowInsecure")) || truthy(q.Get("insecure")) || truthy(q.Get("allow_insecure")) {
		t["insecure"] = true
	}
	if alpn := splitList(q.Get("alpn")); len(alpn) > 0 {
		t["alpn"] = alpn
	}
	fp := q.Get("fp")
	if security == "reality" {
		pbk := q.Get("pbk")
		if pbk == "" {
			return nil, fmt.Errorf("reality: нет pbk (публичного ключа)")
		}
		t["reality"] = map[string]any{"enabled": true, "public_key": pbk, "short_id": q.Get("sid")}
		if fp == "" {
			fp = "chrome" // REALITY requires uTLS.
		}
	}
	if fp != "" {
		t["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
	}
	return t, nil
}

// transport maps ws, grpc, http/h2, and httpupgrade; tcp/raw needs no transport.
func transport(q url.Values) (map[string]any, error) {
	kind := strings.ToLower(q.Get("type"))
	if kind == "" {
		kind = strings.ToLower(q.Get("net"))
	}
	host, path := q.Get("host"), q.Get("path")
	switch kind {
	case "", "tcp", "raw":
		if q.Get("headerType") == "http" {
			t := map[string]any{"type": "http"}
			if host != "" {
				t["host"] = splitList(host)
			}
			if path != "" {
				t["path"] = path
			}
			return t, nil
		}
		return nil, nil
	case "ws", "websocket":
		t := map[string]any{"type": "ws"}
		if path != "" {
			// A path query such as ?ed=2048 configures Xray early data.
			if p, ed, ok := strings.Cut(path, "?ed="); ok {
				path = p
				if n, err := strconv.Atoi(ed); err == nil {
					t["max_early_data"] = n
					t["early_data_header_name"] = "Sec-WebSocket-Protocol"
				}
			}
			t["path"] = path
		}
		if host != "" {
			t["headers"] = map[string]any{"Host": host}
		}
		return t, nil
	case "grpc":
		t := map[string]any{"type": "grpc"}
		if sn := q.Get("serviceName"); sn != "" {
			t["service_name"] = sn
		}
		return t, nil
	case "http", "h2":
		t := map[string]any{"type": "http"}
		if host != "" {
			t["host"] = splitList(host)
		}
		if path != "" {
			t["path"] = path
		}
		return t, nil
	case "httpupgrade":
		t := map[string]any{"type": "httpupgrade"}
		if host != "" {
			t["host"] = host
		}
		if path != "" {
			t["path"] = path
		}
		return t, nil
	case "xhttp", "splithttp":
		return nil, fmt.Errorf("транспорт %s есть только в Xray, sing-box его не поддерживает", kind)
	default:
		return nil, fmt.Errorf("транспорт type=%q не поддерживается", kind)
	}
}

// withTLSAndTransport mutates an outbound with supported TLS and transport options.
// Unsupported transport settings return an error rather than being dropped.
func withTLSAndTransport(o Outbound, q url.Values, host string, forceTLS bool) error {
	t, err := tlsOptions(q, host, forceTLS)
	if err != nil {
		return err
	}
	if t != nil {
		o["tls"] = t
	}
	tr, err := transport(q)
	if err != nil {
		return err
	}
	if tr != nil {
		o["transport"] = tr
	}
	return nil
}

// parseVLESS maps URL userinfo to UUID, defaults packet encoding to xudp, and preserves optional flow.
func parseVLESS(link string) (Outbound, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := server(u)
	if err != nil {
		return nil, err
	}
	id := u.User.Username()
	if id == "" {
		return nil, fmt.Errorf("vless: нет UUID")
	}
	q := u.Query()
	o := Outbound{"type": "vless", "server": host, "server_port": port, "uuid": id, "packet_encoding": "xudp"}
	if flow := q.Get("flow"); flow != "" {
		o["flow"] = flow
	}
	return o, withTLSAndTransport(o, q, host, false)
}

// parseTrojan requires a password in URL userinfo and enables TLS by default.
func parseTrojan(link string) (Outbound, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := server(u)
	if err != nil {
		return nil, err
	}
	password := u.User.Username()
	if password == "" {
		return nil, fmt.Errorf("trojan: нет пароля")
	}
	o := Outbound{"type": "trojan", "server": host, "server_port": port, "password": password}
	// Trojan enables TLS by default.
	return o, withTLSAndTransport(o, u.Query(), host, true)
}

// parseVMess accepts vmess://base64(JSON) in v2rayN format.
func parseVMess(link string) (Outbound, error) {
	body := link[strings.Index(link, "://")+3:]
	if i := strings.IndexByte(body, '#'); i >= 0 {
		body = body[:i]
	}
	raw, err := decodeBase64(body)
	if err != nil {
		return nil, fmt.Errorf("vmess: не base64: %w", err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("vmess: не JSON: %w", err)
	}
	str := func(k string) string {
		switch x := v[k].(type) {
		case string:
			return x
		case float64:
			return strconv.Itoa(int(x))
		}
		return ""
	}
	host := str("add")
	port, err := strconv.Atoi(str("port"))
	if host == "" || err != nil {
		return nil, fmt.Errorf("vmess: нет add/port")
	}
	if str("id") == "" {
		return nil, fmt.Errorf("vmess: нет id")
	}
	security := str("scy")
	if security == "" {
		security = "auto"
	}
	o := Outbound{"type": "vmess", "server": host, "server_port": port, "uuid": str("id"), "security": security, "packet_encoding": "xudp"}
	if aid, _ := strconv.Atoi(str("aid")); aid > 0 {
		o["alter_id"] = aid
	}
	q := url.Values{}
	for k, qk := range map[string]string{"net": "type", "host": "host", "path": "path", "sni": "sni", "alpn": "alpn", "fp": "fp"} {
		if s := str(k); s != "" {
			q.Set(qk, s)
		}
	}
	if str("type") == "http" {
		q.Set("headerType", "http")
	}
	if tls := str("tls"); tls == "tls" || tls == "reality" {
		q.Set("security", tls)
		if q.Get("sni") == "" && str("host") != "" {
			q.Set("sni", str("host"))
		}
	}
	return o, withTLSAndTransport(o, q, host, false)
}

// parseShadowsocks supports SIP002 (ss://base64(method:password)@host:port)
// and the legacy ss://base64(method:password@host:port) format.
func parseShadowsocks(link string) (Outbound, error) {
	body := link[len("ss://"):]
	if i := strings.IndexByte(body, '#'); i >= 0 {
		body = body[:i]
	}
	query := ""
	if i := strings.IndexByte(body, '?'); i >= 0 {
		body, query = body[:i], body[i+1:]
	}
	if q, _ := url.ParseQuery(query); q.Get("plugin") != "" {
		return nil, fmt.Errorf("ss: плагины (%s) не поддерживаются ссылкой, используйте type: singbox", q.Get("plugin"))
	}
	var userinfo, hostport string
	if at := strings.LastIndexByte(body, '@'); at >= 0 {
		userinfo, hostport = body[:at], body[at+1:]
		if dec, err := decodeBase64(userinfo); err == nil {
			userinfo = string(dec)
		} else if un, err := url.PathUnescape(userinfo); err == nil {
			userinfo = un // SIP002 permits plain method:password for 2022-* methods.
		}
	} else {
		dec, err := decodeBase64(body)
		if err != nil {
			return nil, fmt.Errorf("ss: не base64: %w", err)
		}
		at := strings.LastIndexByte(string(dec), '@')
		if at < 0 {
			return nil, fmt.Errorf("ss: нет адреса сервера")
		}
		userinfo, hostport = string(dec[:at]), string(dec[at+1:])
	}
	method, password, ok := strings.Cut(userinfo, ":")
	if !ok || method == "" || password == "" {
		return nil, fmt.Errorf("ss: нужно method:password")
	}
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return nil, fmt.Errorf("ss: адрес: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("ss: порт %q", portStr)
	}
	return Outbound{"type": "shadowsocks", "server": host, "server_port": port, "method": method, "password": password}, nil
}

// hopPorts replaces a port range such as 443,20000-30000 with one port
// so net/url can parse the link, returning the original range separately.
func hopPorts(link string) (clean, ports string) {
	start := strings.Index(link, "://") + 3
	end := len(link)
	if i := strings.IndexAny(link[start:], "/?#"); i >= 0 {
		end = start + i
	}
	authority := link[start:end]
	at := strings.LastIndexByte(authority, '@')
	hostport := authority[at+1:]
	colon := strings.LastIndexByte(hostport, ':')
	if colon < 0 || strings.HasSuffix(hostport, "]") {
		return link, ""
	}
	p := hostport[colon+1:]
	if !strings.ContainsAny(p, "-,") {
		return link, ""
	}
	cut := start + at + 1 + colon
	return link[:cut] + ":0" + link[end:], p
}

// parseHysteria2 supports port hopping, defaults to port 443, and enables TLS.
// Optional obfuscation and positive Mbps limits are carried into the outbound.
func parseHysteria2(link string) (Outbound, error) {
	link, hop := hopPorts(link)
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("hysteria2: нет адреса сервера")
	}
	q := u.Query()
	o := Outbound{"type": "hysteria2", "server": host}
	ports := u.Port()
	if hop != "" {
		ports = hop
	} else if ports == "" {
		ports = "443"
	}
	if p, err := strconv.Atoi(ports); err == nil {
		o["server_port"] = p
	} else {
		var list []string
		for _, r := range splitList(ports) {
			list = append(list, strings.ReplaceAll(r, "-", ":"))
		}
		o["server_ports"] = list
	}
	password := u.User.Username()
	if pw, ok := u.User.Password(); ok {
		password += ":" + pw
	}
	if password != "" {
		o["password"] = password
	}
	if obfs := q.Get("obfs"); obfs != "" {
		o["obfs"] = map[string]any{"type": obfs, "password": q.Get("obfs-password")}
	}
	if up, err := strconv.Atoi(strings.TrimSuffix(q.Get("up"), "mbps")); err == nil && up > 0 {
		o["up_mbps"] = up
	}
	if down, err := strconv.Atoi(strings.TrimSuffix(q.Get("down"), "mbps")); err == nil && down > 0 {
		o["down_mbps"] = down
	}
	t, err := tlsOptions(q, host, true)
	if err != nil {
		return nil, err
	}
	o["tls"] = t
	return o, nil
}

// parseTUIC reads UUID/password userinfo and defaults TLS ALPN to h3 when omitted.
func parseTUIC(link string) (Outbound, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := server(u)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	id := u.User.Username()
	pw, _ := u.User.Password()
	if id == "" {
		return nil, fmt.Errorf("tuic: нет UUID")
	}
	o := Outbound{"type": "tuic", "server": host, "server_port": port, "uuid": id, "password": pw}
	if cc := q.Get("congestion_control"); cc != "" {
		o["congestion_control"] = cc
	}
	if m := q.Get("udp_relay_mode"); m != "" {
		o["udp_relay_mode"] = m
	}
	t, err := tlsOptions(q, host, true)
	if err != nil {
		return nil, err
	}
	if _, ok := t["alpn"]; !ok {
		t["alpn"] = []string{"h3"}
	}
	o["tls"] = t
	return o, nil
}

// parseAnyTLS maps the userinfo name to the password and enables TLS for the server.
func parseAnyTLS(link string) (Outbound, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := server(u)
	if err != nil {
		return nil, err
	}
	o := Outbound{"type": "anytls", "server": host, "server_port": port, "password": u.User.Username()}
	t, err := tlsOptions(u.Query(), host, true)
	if err != nil {
		return nil, err
	}
	o["tls"] = t
	return o, nil
}

// parseSocks builds SOCKS5 options, attaching credentials only when a username is supplied.
func parseSocks(link string) (Outbound, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	host, port, err := server(u)
	if err != nil {
		return nil, err
	}
	o := Outbound{"type": "socks", "server": host, "server_port": port, "version": "5"}
	if name := u.User.Username(); name != "" {
		o["username"] = name
		if pw, ok := u.User.Password(); ok {
			o["password"] = pw
		}
	}
	return o, nil
}

func decodeBase64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("неверный base64")
}
