package model

import (
	"fmt"
	"strings"
	"testing"
)

const validYAML = `
lan:
  interface: br0
  wan: eth3
dns:
  upstreams: ["https://1.1.1.1/dns-query", "77.88.8.8"]
intercept:
  mode: tproxy
connections:
  - id: fi
    name: Finland
    type: wireguard
    wireguard:
      private_key: cHJpdmF0ZQ==
      address: ["10.13.13.2/32"]
      peer:
        endpoint: 203.0.113.10:42820
        public_key: cHVibGlj
groups:
  - name: Foreign
    target: fi
    entries: ["youtube.com", "*.googlevideo.com", "149.154.160.0/20"]
  - name: Ads
    target: block
    entries: ["ads.example.com"]
networks:
  - name: Home
    cidr: 192.168.7.0/24
    mode: split
    target: fi
clients:
  - name: Phone
    mac: 02:00:00:00:00:10
    mode: full
    target: fi
`

func TestParseValid(t *testing.T) {
	c, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	if c.Intercept.TCPPort == 0 || c.DNS.Listen == "" {
		t.Fatalf("defaults not populated: %+v", c.Intercept)
	}
}

func TestValidateCollectsAllErrors(t *testing.T) {
	bad := strings.NewReplacer(
		"target: fi\n    entries", "target: nope\n    entries",
		"mac: 02:00:00:00:00:10", "mac: zz",
		"mode: tproxy", "mode: magic",
	).Replace(validYAML)
	_, err := Parse([]byte(bad))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{`unknown target "nope"`, "mac:", `unknown mode "magic"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q:\n%v", want, err)
		}
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	_, err := Parse([]byte(validYAML + "\nfoo: 1\n"))
	if err == nil || !strings.Contains(err.Error(), "foo") {
		t.Fatalf("unknown field must be rejected, got %v", err)
	}
}

func TestParseEntry(t *testing.T) {
	cases := []struct {
		in     string
		kind   EntryKind
		domain string
		prefix string
	}{
		{"Example.COM", EntryDomain, "example.com", ""},
		{"*.example.com", EntryDomain, "example.com", ""},
		{".example.com.", EntryDomain, "example.com", ""},
		{"\u043f\u0440\u0438\u043c\u0435\u0440.\u0440\u0444", EntryDomain, "xn--e1afmkfd.xn--p1ai", ""},
		{"1.2.3.4", EntryPrefix, "", "1.2.3.4/32"},
		{"10.1.2.3/8", EntryPrefix, "", "10.0.0.0/8"},
		{"2001:db8::1", EntryPrefix, "", "2001:db8::1/128"},
		{"example.com # comment", EntryDomain, "example.com", ""},
	}
	for _, c := range cases {
		e, err := ParseEntry(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if e.Kind != c.kind || e.Domain != c.domain || (c.prefix != "" && e.Prefix.String() != c.prefix) {
			t.Errorf("%q: got %+v", c.in, e)
		}
	}
	for _, bad := range []string{"", "localhost", "# only comment", "http://x"} {
		if _, err := ParseEntry(bad); err == nil {
			t.Errorf("%q must fail", bad)
		}
	}
}

func TestParseUpstream(t *testing.T) {
	cases := map[string]Upstream{
		"https://1.1.1.1/dns-query": {UpstreamHTTPS, "1.1.1.1", 443, "/dns-query"},
		"https://dns.google":        {UpstreamHTTPS, "dns.google", 443, "/dns-query"},
		"tls://9.9.9.9":             {UpstreamTLS, "9.9.9.9", 853, ""},
		"udp://77.88.8.8:5353":      {UpstreamUDP, "77.88.8.8", 5353, ""},
		"8.8.8.8":                   {UpstreamUDP, "8.8.8.8", 53, ""},
	}
	for in, want := range cases {
		got, err := ParseUpstream(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v, %v; expected %+v", in, got, err, want)
		}
	}
	if _, err := ParseUpstream("ftp://x"); err == nil {
		t.Error("ftp:// must fail")
	}
}

func TestCountryGroup(t *testing.T) {
	base := "lan: {interface: br0}\ndns: {upstreams: [\"1.1.1.1\"]}\ngroups:\n"
	if _, err := Parse([]byte(base + "  - {name: g, target: direct, countries: [RU, BY], except: true}\n")); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"  - {name: g, target: direct, entries: [a.com], countries: [RU]}\n",
		"  - {name: g, target: direct, countries: [rus]}\n",
		"  - {name: g, target: direct, entries: [\"!geoip:ru\"]}\n",
	} {
		if _, err := Parse([]byte(base + bad)); err == nil {
			t.Errorf("%q must fail", bad)
		}
	}
}

func TestFallbackChain(t *testing.T) {
	base := `
connections:
  - {id: fi, type: link, link: "socks5://203.0.113.1:1080", fallback: nl}
  - {id: nl, type: link, link: "socks5://203.0.113.2:1080", fallback: de}
  - {id: de, type: link, link: "socks5://203.0.113.3:1080"%s}
`
	cfg, err := Parse([]byte(fmt.Sprintf(base, "")))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.Chain("fi"), ","); got != "fi,nl,de" {
		t.Errorf("fi chain: %s", got)
	}
	if got := strings.Join(cfg.Chain("de"), ","); got != "de" {
		t.Errorf("de chain: %s", got)
	}
	if cfg.Failover.DownAfter != 60 || cfg.Failover.UpAfter != 120 || cfg.Failover.Interval != 15 {
		t.Errorf("defaults: %+v", cfg.Failover)
	}
	cfg, err = Parse([]byte(fmt.Sprintf(base, ", fallback: direct")))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.Chain("fi"), ","); got != "fi,nl,de,direct" {
		t.Errorf("with direct: %s", got)
	}
	for tail, want := range map[string]string{
		", fallback: fi":    "cycle",
		", fallback: de":    "connection itself",
		", fallback: block": "not block",
		", fallback: xx":    "unknown fallback",
	} {
		if _, err := Parse([]byte(fmt.Sprintf(base, tail))); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: expected %s, got %v", tail, want, err)
		}
	}
}

func TestNoTokenFrom(t *testing.T) {
	cfg, err := Parse([]byte("api: {no_token_from: [\"192.168.7.1/24\", \" 10.0.0.0/8 \"]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.API.NoTokenPrefixes(); len(got) != 2 || got[0].String() != "192.168.7.0/24" {
		t.Errorf("%v", got)
	}
	if _, err := Parse([]byte("api: {no_token_from: [\"192.168.7.1\"]}\n")); err == nil || !strings.Contains(err.Error(), "no_token_from") {
		t.Errorf("address without a prefix must fail: %v", err)
	}
}
