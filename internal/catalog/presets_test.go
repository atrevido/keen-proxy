package catalog

import (
	"slices"
	"testing"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

func TestCatalogAndManagedUpdates(t *testing.T) {
	if len(Presets) != 60 {
		t.Fatal(len(Presets))
	}
	seen := map[string]bool{}
	for _, p := range Presets {
		if seen[p.ID] || len(p.Domains) == 0 || p.ID == "" {
			t.Fatal(p)
		}
		seen[p.ID] = true
		for _, s := range p.Domains {
			if _, e := model.ParseEntry(s); e != nil {
				t.Fatal(p.ID, s, e)
			}
		}
	}
	old := model.Group{Name: "custom title", Target: "direct", Entries: []string{"keep.test", "mine.test"}, Preset: &model.Preset{ID: "test", Version: "old", Base: []string{"keep.test", "deleted.test"}}}
	p := Preset{ID: "test", Domains: []string{"keep.test", "deleted.test", "new.test"}}
	g, diff := Update(old, p)
	if !slices.Equal(g.Entries, []string{"keep.test", "mine.test", "new.test"}) {
		t.Fatal(g.Entries)
	}
	if g.Name != old.Name || g.Target != old.Target || g.Preset.Version != p.revision() || !slices.Equal(diff.Added, []string{"new.test"}) {
		t.Fatal(g, diff)
	}
}

func TestUserAdditionSurvivesCatalogRoundTrip(t *testing.T) {
	g := model.Group{Entries: []string{"a.test", "mine.test"}, Preset: &model.Preset{ID: "t", Version: "1", Base: []string{"a.test"}}}
	g, _ = Update(g, Preset{ID: "t", Domains: []string{"a.test", "mine.test"}})
	g, _ = Update(g, Preset{ID: "t", Domains: []string{"a.test"}})
	if !slices.Contains(g.Entries, "mine.test") {
		t.Fatal("manual addition lost")
	}
}
