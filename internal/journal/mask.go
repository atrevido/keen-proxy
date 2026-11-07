package journal

import (
	"net/url"
	"sort"
	"strings"

	"github.com/ordo-systems/keen-proxy/internal/model"
)

const masked = "•••"

// Masker redacts configured credentials and extra secrets from exported log
// text, including WireGuard keys, connection links, passwords, and panel tokens.
func Masker(cfg *model.Config, extra ...string) func(string) string {
	secrets := append([]string{}, extra...)
	if cfg != nil {
		for _, raw := range cfg.DNS.Upstreams {
			secrets = append(secrets, linkSecrets(raw)...)
		}
		secrets = append(secrets, linkSecrets(cfg.GeoIP.URL)...)
		secrets = append(secrets, linkSecrets(cfg.Failover.URL)...)
		for _, source := range cfg.Filters.Sources {
			secrets = append(secrets, linkSecrets(source.URL)...)
		}
		for _, cn := range cfg.Connections {
			if wg := cn.WireGuard; wg != nil {
				secrets = append(secrets, wg.PrivateKey, wg.Peer.PresharedKey)
			}
			secrets = append(secrets, linkSecrets(cn.Link)...)
			if cn.TrustTunnel != nil {
				secrets = append(secrets, cn.TrustTunnel.Link)
			}
			for _, m := range []map[string]any{cn.Outbound, cn.Endpoint} {
				collect(m, &secrets)
			}
		}
	}
	// Skip short secrets such as "1" or "a" to avoid corrupting unrelated text.
	var list []string
	for _, s := range secrets {
		if len(s) >= 6 {
			list = append(list, s)
		}
	}
	// Replace longer secrets first: the whole link before its parts.
	sort.Slice(list, func(i, j int) bool { return len(list[i]) > len(list[j]) })
	return func(s string) string {
		for _, sec := range list {
			s = strings.ReplaceAll(s, sec, masked)
		}
		return s
	}
}

// linkSecrets includes the complete link and, when userinfo exists, credentials and known secret query values.
func linkSecrets(link string) []string {
	if link == "" {
		return nil
	}
	out := []string{link}
	if u, err := url.Parse(link); err == nil && u.User != nil {
		out = append(out, u.User.Username())
		if p, ok := u.User.Password(); ok {
			out = append(out, p)
		}
		for _, k := range []string{"pbk", "sid", "password", "psk", "key"} {
			if v := u.Query().Get(k); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

// collect walks raw JSON objects and arrays, gathering string values under credential-like keys.
func collect(v any, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if s, ok := val.(string); ok && secretKey(k) {
				*out = append(*out, s)
				continue
			}
			collect(val, out)
		}
	case []any:
		for _, val := range x {
			collect(val, out)
		}
	}
}

func secretKey(k string) bool {
	k = strings.ToLower(k)
	for _, s := range []string{"password", "uuid", "private_key", "pre_shared_key", "psk", "secret", "token", "public_key", "short_id"} {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}
