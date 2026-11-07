package api

import (
	"context"
	"net/netip"
	"testing"

	"github.com/ordo-systems/keen-proxy/internal/journal"
	"github.com/ordo-systems/keen-proxy/internal/model"
)

// A category-only group still yields a describable rule-set union.
func TestDemoRuleTagsIncludeCategories(t *testing.T) {
	cfg := &model.Config{}
	cfg.Defaults()
	g := model.Group{Name: "Blocked", Categories: []string{"RU-BLOCKED"}, Except: true}
	if got := tagsOf(g); got != "geocat-ru-blocked" {
		t.Fatalf("tags: %s", got)
	}
	g.Countries = []string{"RU"}
	if got := tagsOf(g); got != "[geoip-ru geocat-ru-blocked]" {
		t.Fatalf("tags: %s", got)
	}
	journal.DescribeRule(cfg, "source_ip_cidr=192.168.7.0/24 && !(rule_set="+tagsOf(g)+")")
}

// Without country databases the demo reports nothing loaded, and labels
// shared by category databases appear once.
func TestDemoGeoCategoriesOnly(t *testing.T) {
	cfg := &model.Config{}
	cfg.Defaults()
	for i, db := range cfg.GeoIP.Databases {
		cfg.GeoIP.Databases[i].Enabled = db.IsCategory()
	}
	d := NewDemo(cfg)
	if st := d.Status(context.Background()).GeoIP; st.Loaded || st.Active != 0 || st.Categories != 2 || st.Error == "" {
		t.Fatalf("status: %+v", st)
	}
	res, err := d.LookupGeo(netip.MustParseAddr("185.199.108.1"), "")
	if err != nil || len(res.Results) != 2 || len(res.Categories) != 1 || res.Categories[0] != "RU-BLOCKED" || res.Country != "" {
		t.Fatalf("lookup: %+v %v", res, err)
	}
}
