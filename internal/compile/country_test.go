package compile

import (
	"context"
	"encoding/json"
	"net/netip"
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

const countryYAML = `
lan: {interface: br0}
dns: {upstreams: ["https://1.1.1.1/dns-query"]}
connections:
  - {id: fi, type: link, link: "socks5://203.0.113.9:1080"}
groups:
  - {name: Direct banks, target: direct, entries: ["sberbank.ru"]}
  - {name: Except Russia and Belarus, target: fi, countries: [ru, BY], except: true}
  - {name: Country block, target: block, countries: [KP]}
networks:
  - {name: Home, cidr: 192.168.7.0/24, mode: split}
`

func TestExceptCountry(t *testing.T) {
	cfg, err := model.Parse([]byte(countryYAML))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out, err := Compile(Input{Config: cfg, RuleSetDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(out.Countries, ",") != "BY,KP,RU" {
		t.Errorf("Countries = %v", out.Countries)
	}

	var parsed struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	json.Unmarshal(out.SingBox, &parsed)
	var got []string
	for _, r := range parsed.Route.Rules {
		b, _ := json.Marshal(r)
		got = append(got, string(b))
	}
	all := strings.Join(got, "\n")
	// Country blocking applies to all sources.
	if !strings.Contains(all, `{"action":"reject","rule_set":["geoip-kp"]}`) {
		t.Errorf("missing country block:\n%s", all)
	}
	// The explicit direct domain precedes the country-except rule.
	direct := strings.Index(all, `"rule_set":["group-0"]`)
	except := strings.Index(all, `{"invert":true,"rule_set":["geoip-ru","geoip-by"]}`)
	if direct < 0 || except < 0 || direct > except {
		t.Errorf("allowlist must precede country rule (%d, %d):\n%s", direct, except, all)
	}
	// Country-except uses logical AND so invert cannot negate the source condition.
	if !strings.Contains(all, `{"mode":"and","outbound":"fi","rules":[{"source_ip_cidr":["192.168.7.0/24"]},{"invert":true,"rule_set":["geoip-ru","geoip-by"]}],"type":"logical"}`) {
		t.Errorf("missing logical rule for network:\n%s", all)
	}

	// sing-box must accept configuration referencing country sets.
	for name, raw := range out.RuleSets {
		os.WriteFile(filepath.Join(dir, name), raw, 0o600)
	}
	os.WriteFile(filepath.Join(dir, "geoip-ru.json"), CountryRuleSet([]netip.Prefix{netip.MustParsePrefix("77.88.0.0/18")}), 0o600)
	os.WriteFile(filepath.Join(dir, "geoip-kp.json"), CountryRuleSet(nil), 0o600)
	os.WriteFile(filepath.Join(dir, "geoip-by.json"), CountryRuleSet(nil), 0o600)
	ctx := engine.Context(context.Background())
	opts, err := sjson.UnmarshalExtendedContext[option.Options](ctx, out.SingBox)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatalf("sing-box rejected configuration: %v", err)
	}
	instance.Close()
}

// Route non-Russian destinations through the tunnel but Steam directly. Country
// groups are evaluated after explicit domains and addresses, even when the
// country group appears earlier in configuration.
func TestCountryGroupsLast(t *testing.T) {
	cfg, err := model.Parse([]byte(`
lan: {interface: br0}
dns: {upstreams: ["https://1.1.1.1/dns-query"]}
connections:
  - {id: fi, type: link, link: "socks5://203.0.113.9:1080"}
groups:
  - {name: Except Russia, target: fi, countries: [RU], except: true}
  - {name: Steam, target: direct, entries: [steampowered.com, steamcommunity.com, steamcontent.com]}
networks:
  - {name: Home, cidr: 192.168.7.0/24, mode: split}
clients:
  - {name: PC, mac: "aa:bb:cc:dd:ee:01", mode: split}
`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Compile(Input{Config: cfg, RuleSetDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Route struct {
			Rules []json.RawMessage `json:"rules"`
		} `json:"route"`
	}
	json.Unmarshal(out.SingBox, &parsed)
	// For each source (network or client), the Steam rule (group-1) precedes
	// the country rule and routes directly.
	steam, country := 0, 0
	for _, r := range parsed.Route.Rules {
		var v any
		json.Unmarshal(r, &v)
		b, _ := json.Marshal(v)
		s := string(b)
		switch {
		case strings.Contains(s, `"group-1"`):
			if !strings.Contains(s, `"outbound":"direct"`) {
				t.Errorf("Steam is not routed directly: %s", s)
			}
			steam++
		case strings.Contains(s, `"geoip-ru"`):
			if steam <= country {
				t.Errorf("country rule precedes Steam: %s", s)
			}
			country++
		}
	}
	if steam == 0 || steam != country {
		t.Errorf("Steam rules %d, country rules %d", steam, country)
	}
}

// Category groups reference geocat-<label> sets and may combine labels with countries.
func TestCategoryGroups(t *testing.T) {
	cfg, err := model.Parse([]byte(`
lan: {interface: br0}
dns: {upstreams: ["https://1.1.1.1/dns-query"]}
geoip: {databases: [{id: runetfreedom-ru, enabled: true}]}
connections:
  - {id: fi, type: link, link: "socks5://203.0.113.9:1080"}
groups:
  - {name: Blocked, target: fi, categories: [ru-blocked, RE-FILTER]}
  - {name: All except Russia, target: fi, countries: [RU], categories: [RU-WHITELIST], except: true}
networks:
  - {name: Home, cidr: 192.168.7.0/24, mode: split}
`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Compile(Input{Config: cfg, RuleSetDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(out.Categories, ",") != "RE-FILTER,RU-BLOCKED,RU-WHITELIST" || strings.Join(out.Countries, ",") != "RU" {
		t.Fatalf("Categories=%v Countries=%v", out.Categories, out.Countries)
	}
	sb := string(out.SingBox)
	for _, want := range []string{`"geocat-ru-blocked"`, `"geocat-re-filter"`, `"geocat-ru-whitelist"`, `geocat-ru-whitelist.json`} {
		if !strings.Contains(sb, want) {
			t.Errorf("missing %s in sing-box config", want)
		}
	}
	// Categories need an enabled category database; a country database is not enough.
	cfg.GeoIP.Databases = []model.GeoIPDatabase{{ID: "dbip", Enabled: true}}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "category database") {
		t.Fatalf("want error without category database: %v", err)
	}
}
