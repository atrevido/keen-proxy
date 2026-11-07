package daemon

import (
	"github.com/atrevido/keenetic-proxy/internal/model"
	"reflect"
	"testing"
	"time"
)

func TestPick(t *testing.T) {
	const down, up = time.Minute, 2 * time.Minute
	t0 := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	chain := []string{"fi", "nl", "de"}
	hs := map[string]*health{}
	set := func(id string, ok bool, at time.Time) {
		if hs[id] == nil {
			hs[id] = &health{}
		}
		hs[id].record(ok, 0, map[bool]string{false: "timeout"}[ok], at)
	}
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }
	steps := []struct {
		name string
		sec  int
		fi   bool
		nl   bool
		de   bool
		cur  string
		want string
	}{
		{"all healthy — primary", 0, true, true, true, "fi", "fi"},
		{"primary became unavailable — keep waiting", 15, false, true, true, "fi", "fi"},
		{"unavailable for 45 s — keep waiting", 60, false, true, true, "fi", "fi"},
		{"unavailable for a minute — use fallback", 75, false, true, true, "fi", "nl"},
		{"primary recovered — wait before returning", 90, true, true, true, "nl", "nl"},
		{"brief failure — reset timer", 120, false, true, true, "nl", "nl"},
		{"healthy for 2 minutes — return", 300, true, true, true, "nl", "nl"},
	}
	for _, s := range steps {
		set("fi", s.fi, at(s.sec))
		set("nl", s.nl, at(s.sec))
		set("de", s.de, at(s.sec))
		if got := pick(chain, s.cur, hs, at(s.sec), down, up); got != s.want {
			t.Errorf("%s: %s, want %s", s.name, got, s.want)
		}
	}
	// fi has responded since 300 s; return after two minutes.
	set("fi", true, at(420))
	if got := pick(chain, "nl", hs, at(420), down, up); got != "fi" {
		t.Errorf("return to primary: %s", got)
	}

	// Nested fallback: fi and nl are down, so use de.
	hs = map[string]*health{}
	set("fi", false, at(0))
	set("nl", false, at(0))
	set("de", true, at(0))
	if got := pick(chain, "fi", hs, at(60), down, up); got != "de" {
		t.Errorf("fi and nl unavailable: %s, want de", got)
	}
	// While on de, nl failed later than fi, but fi recovered; return to fi.
	set("fi", true, at(60))
	if got := pick(chain, "de", hs, at(180), down, up); got != "fi" {
		t.Errorf("return past lower priority: %s", got)
	}
	// When active is down, a recently recovered higher-priority member is usable immediately.
	hs = map[string]*health{}
	set("fi", true, at(0))
	set("nl", false, at(0))
	set("de", false, at(0))
	if got := pick(chain, "nl", hs, at(60), down, up); got != "fi" {
		t.Errorf("active is down, fi healthy: %s", got)
	}
	// Keep the current selection when no member responds.
	set("fi", false, at(0))
	if got := pick(chain, "nl", hs, at(120), down, up); got != "nl" {
		t.Errorf("all down: %s", got)
	}
	// A terminal direct fallback is always considered available.
	if got := pick([]string{"fi", "nl", "direct"}, "nl", hs, at(120), down, up); got != "direct" {
		t.Errorf("last fallback direct: %s", got)
	}
	// An untested primary is considered available.
	if got := pick(chain, "fi", map[string]*health{}, at(0), down, up); got != "fi" {
		t.Errorf("before first probe: %s", got)
	}
}

func TestProbeMembersIncludesConnectionsWithoutFallback(t *testing.T) {
	cfg := &model.Config{Connections: []model.Connection{{ID: "wg"}, {ID: "tt"}, {ID: "backup"}}}
	got := probeMembers(cfg)
	if !reflect.DeepEqual(got, []string{"wg", "tt", "backup"}) {
		t.Fatalf("not all profiles are probed: %v", got)
	}
}

func TestHealthVisibleWithoutFailoverChain(t *testing.T) {
	d := New(Options{})
	d.cfg = &model.Config{Connections: []model.Connection{{ID: "tt"}}}
	d.health["tt"] = &health{}
	d.health["tt"].record(false, 0, "timeout", time.Now())
	_, hs := d.failoverInfoLocked()
	if h, ok := hs["tt"]; !ok || h.OK || h.Error != "timeout" {
		t.Fatalf("missing standalone profile health: %v", hs)
	}
}
