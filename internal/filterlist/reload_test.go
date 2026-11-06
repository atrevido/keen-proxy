package filterlist

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

func TestReloadEnforcesAggregateLimit(t *testing.T) {
	m := New(t.TempDir(), nil)
	cfg := model.Filters{}
	for j := 0; j < 2; j++ {
		s := model.FilterSource{ID: fmt.Sprint(j), URL: "https://example.com/" + fmt.Sprint(j), Enabled: true}
		cfg.Sources = append(cfg.Sources, s)
		p := snapshot{URL: s.URL}
		for i := 0; i < MaxTotalRules/2+1; i++ {
			p.Parsed.Domains = append(p.Parsed.Domains, fmt.Sprintf("%06d.s%d.test", i, j))
		}
		raw, _ := json.Marshal(p)
		if err := os.WriteFile(m.path(s), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.Configure(cfg)
	total := 0
	for _, p := range m.loaded {
		total += len(p.Parsed.Domains) + len(p.Parsed.Allow)
	}
	if total > MaxTotalRules {
		t.Fatal("reload bypasses limit", total)
	}
}
