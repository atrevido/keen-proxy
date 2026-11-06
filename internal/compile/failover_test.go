package compile

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"

	"github.com/atrevido/keenetic-proxy/internal/engine"
	"github.com/atrevido/keenetic-proxy/internal/model"
)

// Groups and clients targeting a fallback-enabled connection route through its
// selector; the actual outbound retains the connection ID.
func TestFailoverSelector(t *testing.T) {
	cfg, err := model.Parse([]byte(`
lan: {interface: br0}
dns: {upstreams: ["https://1.1.1.1/dns-query"]}
connections:
  - {id: fi, type: link, link: "socks5://203.0.113.1:1080", fallback: nl}
  - {id: nl, type: link, link: "socks5://203.0.113.2:1080", fallback: de}
  - {id: de, type: link, link: "socks5://203.0.113.3:1080", fallback: direct}
  - {id: us, type: link, link: "socks5://203.0.113.4:1080"}
groups:
  - {name: Видео, target: fi, entries: ["youtube.com"]}
  - {name: Чат, target: us, entries: ["chat.example"]}
networks:
  - {name: Дом, cidr: 192.168.7.0/24, mode: split}
  - {name: Офис, cidr: 10.0.0.0/24, mode: full, target: nl}
clients:
  - {name: ТВ, mac: "aa:bb:cc:dd:ee:01", ip: 192.168.7.20, mode: full, target: fi}
`))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out, err := Compile(Input{Config: cfg, RuleSetDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Failover) != 3 || out.Failover[0].Tag != "failover-fi" || strings.Join(out.Failover[0].Chain, ",") != "fi,nl,de,direct" {
		t.Fatalf("цепочки: %+v", out.Failover)
	}
	var parsed struct {
		Outbounds []map[string]any `json:"outbounds"`
		Route     struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	json.Unmarshal(out.SingBox, &parsed)
	var outs, rules []string
	for _, o := range parsed.Outbounds {
		b, _ := json.Marshal(o)
		outs = append(outs, string(b))
	}
	for _, r := range parsed.Route.Rules {
		b, _ := json.Marshal(r)
		rules = append(rules, string(b))
	}
	all := strings.Join(outs, "\n") + "\n" + strings.Join(rules, "\n")
	for _, want := range []string{
		`{"default":"fi","interrupt_exist_connections":true,"outbounds":["fi","nl","de","direct"],"tag":"failover-fi","type":"selector"}`,
		`"tag":"fi"`, // the connection remains a regular outbound
		`{"outbound":"failover-fi","rule_set":["group-0"],"source_ip_cidr":["192.168.7.0/24"]}`,
		`{"outbound":"us","rule_set":["group-1"],"source_ip_cidr":["192.168.7.0/24"]}`, // without a fallback, route straight to the connection
		`{"outbound":"failover-nl","source_ip_cidr":["10.0.0.0/24"]}`,
		`{"outbound":"failover-fi","rule_set":["clients-full-fi"]}`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("нет %s в\n%s", want, all)
		}
	}
	if strings.Contains(all, `"failover-us"`) {
		t.Error("у соединения без резервного не должно быть селектора")
	}

	for name, raw := range out.RuleSets {
		os.WriteFile(filepath.Join(dir, name), raw, 0o600)
	}
	ctx := engine.Context(context.Background())
	opts, err := sjson.UnmarshalExtendedContext[option.Options](ctx, out.SingBox)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatalf("sing-box отверг конфиг: %v", err)
	}
	instance.Close()
}
