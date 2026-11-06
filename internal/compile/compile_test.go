package compile

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atrevido/keenetic-proxy/internal/engine"
	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

// Keys are valid base64 encodings of 32 zero bytes; no real keys are needed.
const testYAML = `
lan: {interface: br0, wan: eth3}
dns: {upstreams: ["https://1.1.1.1/dns-query"]}
intercept: {mode: tproxy}
connections:
  - id: fi
    type: wireguard
    wireguard:
      private_key: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
      address: ["10.13.13.2/32"]
      mtu: 1408
      peer:
        endpoint: 203.0.113.10:42820
        public_key: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
        keepalive: 25
groups:
  - {name: Зарубежное, target: fi, entries: ["youtube.com", "149.154.160.0/20"]}
  - {name: Реклама, target: block, entries: ["ads.example.com"]}
  - {name: Банк, target: direct, entries: ["bank.ru"]}
networks:
  - {name: Дом, cidr: 192.168.7.0/24, mode: split, target: fi}
clients:
  - {name: Телефон, mac: "02:00:00:00:00:10", mode: full, target: fi}
  - {name: Mac, mac: "aa:bb:cc:dd:ee:01", ip: 192.168.7.88, mode: "off"}
  - {name: Нет в сети, mac: "aa:bb:cc:dd:ee:02", mode: full, target: fi}
`

func compileWith(t *testing.T, phone string) Output {
	t.Helper()
	cfg, err := model.Parse([]byte(testYAML))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Compile(Input{Config: cfg, RuleSetDir: "/var/keenproxy", Neighbors: map[string]netip.Addr{
		"02:00:00:00:00:10": netip.MustParseAddr(phone),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func compileTest(t *testing.T) Output { return compileWith(t, "192.168.7.104") }

// sing-box must accept the schema, tag references, and WireGuard keys.
func TestSingBoxAcceptsConfig(t *testing.T) {
	out := compileTest(t)
	dir := t.TempDir()
	for name, raw := range out.RuleSets {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	config := []byte(strings.ReplaceAll(string(out.SingBox), "/var/keenproxy/", dir+"/"))
	ctx := engine.Context(context.Background())
	opts, err := sjson.UnmarshalExtendedContext[option.Options](ctx, config)
	if err != nil {
		t.Fatalf("sing-box не разобрал конфиг: %v\n%s", err, out.SingBox)
	}
	instance, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatalf("sing-box отверг конфиг: %v\n%s", err, out.SingBox)
	}
	instance.Close()
}

func TestRuleOrder(t *testing.T) {
	var cfg struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
			Final string           `json:"final"`
		} `json:"route"`
	}
	if err := json.Unmarshal(compileTest(t).SingBox, &cfg); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range cfg.Route.Rules {
		raw, _ := json.Marshal(r)
		if strings.Contains(string(raw), `"dns-`) {
			continue
		}
		dst := r["outbound"]
		if dst == nil {
			dst = r["action"]
		}
		sets := joinField(r["rule_set"])
		if sub, ok := r["rules"].([]any); ok { // logical AND
			var parts []string
			for _, x := range sub {
				parts = append(parts, joinField(x.(map[string]any)["rule_set"]))
			}
			sets = strings.Join(parts, "&")
		}
		got = append(got, joinField(r["source_ip_cidr"])+" "+sets+" → "+dst.(string))
	}
	want := []string{
		"  → sniff",
		"  → direct",                  // ip_is_private
		" group-1 → reject",           // blocking applies to every source
		" clients-off → direct",       // Mac is excluded from routing
		" clients-full-fi → fi",       // phone: full tunnel before network rules
		" clients-split&group-0 → fi", // clients using split routing
		" clients-split&group-2 → direct",
		" clients-split → direct",
		"192.168.7.0/24 group-0 → fi", // network: matching routing group
		"192.168.7.0/24 group-2 → direct",
		"192.168.7.0/24  → direct",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("порядок правил:\n%s\nждали:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if cfg.Route.Final != "direct" {
		t.Errorf("final = %q", cfg.Route.Final)
	}
}

func TestPlanFromModel(t *testing.T) {
	p := compileTest(t).Plan
	src := []string{}
	for _, s := range p.Sources {
		src = append(src, s.String())
	}
	if strings.Join(src, ",") != "192.168.7.104/32,192.168.7.0/24" {
		t.Errorf("Sources = %v", src)
	}
	if len(p.Excluded) != 1 || p.Excluded[0].String() != "192.168.7.88" {
		t.Errorf("Excluded = %v", p.Excluded)
	}
	if p.DNSPort != 5354 || p.LAN != "br0" {
		t.Errorf("план: %+v", p)
	}
	found := false
	for _, b := range p.Bypass {
		if b.String() == "203.0.113.10/32" {
			found = true
		}
	}
	if !found {
		t.Error("адрес WireGuard-сервера должен быть в Bypass")
	}
}

func joinField(v any) string {
	list, _ := v.([]any)
	var parts []string
	for _, x := range list {
		parts = append(parts, x.(string))
	}
	return strings.Join(parts, ",")
}

// Client address changes update rule-set files without changing sing-box configuration.
func TestAddressChangeKeepsConfig(t *testing.T) {
	a, b := compileWith(t, "192.168.7.104"), compileWith(t, "192.168.7.120")
	if string(a.SingBox) != string(b.SingBox) {
		t.Error("конфиг sing-box не должен зависеть от адреса устройства")
	}
	full := string(b.RuleSets["clients-full-fi.json"])
	if !strings.Contains(full, "192.168.7.120/32") || strings.Contains(full, "192.168.7.104") {
		t.Errorf("clients-full-fi.json:\n%s", full)
	}
	if split := string(b.RuleSets["clients-split.json"]); !strings.Contains(split, noSource) {
		t.Errorf("пустой класс должен содержать заглушку:\n%s", split)
	}
}
