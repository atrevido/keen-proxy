package model

import (
	"strings"
	"testing"

	"github.com/ordo-systems/keen-proxy/internal/geoip"
)

func geoIDs(g GeoIP) string {
	var s []string
	for _, d := range g.Databases {
		mark := "-"
		if d.Enabled {
			mark = "+"
		}
		s = append(s, mark+d.ID)
	}
	return strings.Join(s, " ")
}

func TestGeoIPDefaultsAndMigration(t *testing.T) {
	var all []string
	for _, b := range geoip.Builtins {
		all = append(all, b.ID)
	}
	for name, tc := range map[string]struct {
		in   GeoIP
		want string
	}{
		"fresh":        {GeoIP{}, "+dbip -ip-location-db -loyalsoldier -runetfreedom-ru -runetfreedom-services"},
		"legacy file":  {GeoIP{File: "/opt/geo.mmdb", URL: "https://x.example/{month}"}, "+custom -dbip -ip-location-db -loyalsoldier -runetfreedom-ru -runetfreedom-services"},
		"legacy url":   {GeoIP{URL: "https://mirror.example/db-{month}.mmdb.gz"}, "+custom -dbip -ip-location-db -loyalsoldier -runetfreedom-ru -runetfreedom-services"},
		"legacy dbip":  {GeoIP{URL: "https://download.db-ip.com/free/dbip-country-lite-{month}.mmdb.gz"}, "+dbip -ip-location-db -loyalsoldier -runetfreedom-ru -runetfreedom-services"},
		"new builtins": {GeoIP{Databases: []GeoIPDatabase{{ID: "loyalsoldier", Enabled: true}}}, "+loyalsoldier -dbip -ip-location-db -runetfreedom-ru -runetfreedom-services"},
	} {
		g := tc.in
		g.defaults()
		if got := geoIDs(g); got != tc.want {
			t.Errorf("%s: %s, want %s (built-ins %v)", name, got, tc.want, all)
		}
		if g.URL != "" || g.File != "" {
			t.Errorf("%s: legacy fields not cleared", name)
		}
		var errs []string
		g.validate(func(f string, a ...any) { errs = append(errs, f) })
		if len(errs) > 0 {
			t.Errorf("%s: %v", name, errs)
		}
	}
	g := GeoIP{File: "/opt/geo.mmdb", URL: "https://x.example/db"}
	g.defaults()
	if d := g.Databases[0]; d.File != "/opt/geo.mmdb" || d.URL != "" {
		t.Errorf("a local file used to disable downloads: %+v", d)
	}
}

func TestGeoIPValidation(t *testing.T) {
	for name, db := range map[string]GeoIPDatabase{
		"plain http":       {ID: "c1", URL: "http://geo.example/db.mmdb"},
		"credentials":      {ID: "c1", URL: "https://user:pass@geo.example/db.mmdb"},
		"url and file":     {ID: "c1", URL: "https://geo.example/db.mmdb", File: "/opt/db.mmdb"},
		"neither":          {ID: "c1", Name: "Empty"},
		"relative file":    {ID: "c1", File: "geo.mmdb"},
		"bad id":           {ID: "../etc", URL: "https://geo.example/db.mmdb"},
		"builtin with url": {ID: "dbip", URL: "https://geo.example/db.mmdb"},
	} {
		g := GeoIP{Databases: []GeoIPDatabase{db}}
		g.defaults()
		var errs []string
		g.validate(func(f string, a ...any) { errs = append(errs, f) })
		if len(errs) == 0 {
			t.Errorf("%s: want error", name)
		}
	}
	g := GeoIP{Databases: []GeoIPDatabase{{ID: "c1", URL: "https://geo.example/db-{month}.mmdb.gz"}, {ID: "c1", File: "/opt/a.mmdb"}}}
	var errs []string
	g.validate(func(f string, a ...any) { errs = append(errs, f) })
	if len(errs) != 1 || !strings.Contains(errs[0], "duplicate") {
		t.Errorf("duplicate id: %v", errs)
	}
	g = GeoIP{URL: "https://x.example/db", Databases: []GeoIPDatabase{{ID: "dbip"}}}
	errs = nil
	g.validate(func(f string, a ...any) { errs = append(errs, f) })
	if len(errs) != 1 {
		t.Errorf("legacy fields next to the list: %v", errs)
	}
}

func TestGeoIPRoundTripsThroughYAML(t *testing.T) {
	c, err := Parse([]byte(validYAML + "geoip:\n  databases:\n    - {id: loyalsoldier, enabled: true}\n    - {id: own, name: Own, url: \"https://geo.example/c.mmdb\", enabled: true}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := geoIDs(c.GeoIP); got != "+loyalsoldier +own -dbip -ip-location-db -runetfreedom-ru -runetfreedom-services" {
		t.Fatal(got)
	}
	if got := geoIDs(c.Clone().GeoIP); got != geoIDs(c.GeoIP) {
		t.Fatal(got)
	}
}

func TestGeoIPKinds(t *testing.T) {
	if !(GeoIPDatabase{ID: "runetfreedom-ru"}).IsCategory() || (GeoIPDatabase{ID: "dbip"}).IsCategory() {
		t.Fatal("built-in kinds come from the catalog")
	}
	if !(GeoIPDatabase{ID: "c1", Kind: "category"}).IsCategory() {
		t.Fatal("custom category database")
	}
	for name, db := range map[string]GeoIPDatabase{
		"builtin kind": {ID: "runetfreedom-ru", Kind: "category"},
		"unknown kind": {ID: "c1", URL: "https://geo.example/c.mmdb", Kind: "asn"},
	} {
		g := GeoIP{Databases: []GeoIPDatabase{db}}
		var errs []string
		g.validate(func(f string, a ...any) { errs = append(errs, f) })
		if len(errs) == 0 {
			t.Errorf("%s: want error", name)
		}
	}
	c, err := Parse([]byte(validYAML + "geoip: {databases: [{id: runetfreedom-services, enabled: true}]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	c.Groups = append(c.Groups, Group{Name: "Services", Target: "fi", Categories: []string{" telegram "}})
	c.Defaults()
	if err := c.Validate(); err != nil || c.Groups[len(c.Groups)-1].Categories[0] != "TELEGRAM" {
		t.Fatalf("categories: %v %v", err, c.Groups[len(c.Groups)-1].Categories)
	}
	c.Groups[len(c.Groups)-1].Categories = []string{"RU BLOCKED"}
	if err := c.Validate(); err == nil {
		t.Fatal("want label error")
	}
}
