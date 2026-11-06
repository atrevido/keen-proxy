package compile

import (
	"bytes"

	"github.com/atrevido/keenetic-proxy/internal/model"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/route/rule"
	"testing"
)

func TestDNSFallbackOnlyWithoutDomain(t *testing.T) {
	item, err := rule.NewDomainRegexItem([]string{".+"})
	if err != nil {
		t.Fatal(err)
	}
	if item.Match(&adapter.InboundContext{}) {
		t.Fatal("empty metadata has domain")
	}
	if !item.Match(&adapter.InboundContext{Domain: "other.test"}) {
		t.Fatal("guard must see unrelated domain")
	}
	cfg, err := model.Parse([]byte(testYAML))
	if err != nil {
		t.Fatal(err)
	}
	out, err := Compile(Input{Config: cfg, RuleSetDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.SingBox, []byte(`"domain_regex"`)) || !bytes.Contains(out.SingBox, []byte(`dns-`)) {
		t.Fatal("DNS fallback absent")
	}
}

func TestDuplicateGroupsDoNotDuplicateDNSDeclarations(t *testing.T) {
	g := model.Group{Name: "same", Target: "direct", Entries: []string{"example.com"}}
	cfg := &model.Config{Groups: []model.Group{g, g}}
	sets, _, _, _ := groupRuleSets(cfg.Groups, t.TempDir())
	seen := map[string]bool{}
	for _, set := range sets {
		tag := set["tag"].(string)
		if seen[tag] {
			t.Fatal("duplicate tag", tag)
		}
		seen[tag] = true
	}

}

func TestFallbackGroupsReordered(t *testing.T) {
	g := model.Group{Name: "one", Target: "direct", Entries: []string{"example.com"}}
	cfg := &model.Config{Groups: []model.Group{g}}
	sets := DNSRuleSets(cfg, map[string][]string{"www.example.com": {"203.0.113.1"}, "notexample.com": {"203.0.113.2"}})
	raw := sets[DomainTag(g)+".json"]
	if !bytes.Contains(raw, []byte("203.0.113.1/32")) || bytes.Contains(raw, []byte("203.0.113.2")) {
		t.Fatal(string(raw))
	}
	tag := DomainTag(g)
	cfg.Groups = append([]model.Group{{Name: "new"}}, g)
	if !bytes.Equal(raw, DNSRuleSets(cfg, map[string][]string{"www.example.com": {"203.0.113.1"}})[tag+".json"]) {
		t.Fatal("identity depends on index")
	}
	g.Target = "block"
	if DomainTag(g) == tag {
		t.Fatal("changed group reused identity")
	}
}
