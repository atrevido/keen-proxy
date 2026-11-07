package lists

import (
	"net/netip"
	"testing"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

func TestMatcher(t *testing.T) {
	m := New([]model.Group{
		{Name: "tunnel", Target: "fi", Entries: []string{"youtube.com", "*.googlevideo.com", "149.154.160.0/20"}},
		{Name: "block", Target: model.TargetBlock, Entries: []string{"ads.youtube.com", "youtube.com", "1.2.3.4"}},
	})

	domains := map[string]int{
		"youtube.com":               0,
		"WWW.YouTube.com.":          0,
		"r1.sn-abc.googlevideo.com": 0,
		"ads.youtube.com":           1, // Longer suffix from another group.
		"notyoutube.com":            NoMatch,
		"com":                       NoMatch,
		"":                          NoMatch,
	}
	for d, want := range domains {
		if got := m.Domain(d); got != want {
			t.Errorf("Domain(%q) = %d, want %d", d, got, want)
		}
	}

	addrs := map[string]int{
		"149.154.167.99": 0,
		"::ffff:1.2.3.4": 1,
		"8.8.8.8":        NoMatch,
	}
	for a, want := range addrs {
		if got := m.Addr(netip.MustParseAddr(a)); got != want {
			t.Errorf("Addr(%s) = %d, want %d", a, got, want)
		}
	}
}
