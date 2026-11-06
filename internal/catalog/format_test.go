package catalog

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

func TestPresetJSONUsesDomainsWithoutExportMetadata(t *testing.T) {
	raw, err := data.ReadFile("presets.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range [][]byte{raw, mustCatalogJSON(t)} {
		var presets []map[string]json.RawMessage
		if err := json.Unmarshal(input, &presets); err != nil {
			t.Fatal(err)
		}
		for _, p := range presets {
			for _, key := range []string{"version", "source", "entries"} {
				if _, ok := p[key]; ok {
					t.Fatalf("preset %s exposes %s", p["id"], key)
				}
			}
			var domains []string
			if err := json.Unmarshal(p["domains"], &domains); err != nil || len(domains) == 0 {
				t.Fatalf("missing domains for %s: %v", p["id"], err)
			}
		}
	}
}

func mustCatalogJSON(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(Presets)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAutoUpdateDetectsContentChangesWithoutExportVersion(t *testing.T) {
	original := Presets
	defer func() { Presets = original }()
	p := Preset{ID: "test", Domains: []string{"keep.test", "deleted.test"}}
	g, _ := Update(model.Group{Name: "custom", Target: "direct"}, p)
	g.Entries = []string{"keep.test", "mine.test"}
	g.Preset.AutoUpdate = true
	previous := g.Preset.Version
	p.Domains = []string{"keep.test", "deleted.test", "new.test"}
	Presets = []Preset{p}
	c := model.Config{Groups: []model.Group{g, {Preset: &model.Preset{ID: "missing", AutoUpdate: true}}, {Preset: &model.Preset{ID: "test"}}}}
	AutoUpdate(&c)
	got := c.Groups[0]
	if !slices.Equal(got.Entries, []string{"keep.test", "mine.test", "new.test"}) || got.Preset.Version == previous || !got.Preset.AutoUpdate {
		t.Fatalf("content update lost: %+v", got)
	}
	revision := got.Preset.Version
	AutoUpdate(&c)
	if c.Groups[0].Preset.Version != revision || !slices.Equal(c.Groups[0].Entries, got.Entries) {
		t.Fatal("update is not idempotent")
	}
	if c.Groups[1].Preset.ID != "missing" || c.Groups[2].Preset.Version != "" {
		t.Fatal("updated unknown or opted-out preset")
	}
}

func TestPresetRevisionTracksRoutingContentOnly(t *testing.T) {
	p := Preset{ID: "test", Domains: []string{"a.test"}}
	installed, _ := Update(model.Group{}, p)
	initial := installed.Preset.Version
	p.Name = "Updated title"
	p.Description = "Updated description"
	p.Category = "Development"
	unchanged, _ := Update(installed, p)
	if unchanged.Preset.Version != initial {
		t.Fatal("display metadata changed routing revision")
	}
	p.Prefixes = []string{"192.0.2.0/24"}
	changed, _ := Update(unchanged, p)
	if changed.Preset.Version == initial {
		t.Fatal("prefix change did not change revision")
	}
}
