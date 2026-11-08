package catalog

import (
	"net/netip"
	"testing"

	"github.com/ordo-systems/keen-proxy/internal/model"
)

func TestBundledDNSProfiles(t *testing.T) {
	ids := make(map[string]bool)
	for _, profile := range DNS {
		t.Run(profile.ID, func(t *testing.T) {
			if profile.ID == "" || ids[profile.ID] || profile.Name == "" || profile.Source == "" {
				t.Fatalf("incomplete or duplicate profile: %+v", profile)
			}
			ids[profile.ID] = true
			for endpoint, kind := range map[string]model.UpstreamKind{
				profile.DoH: model.UpstreamHTTPS, profile.DoT: model.UpstreamTLS, profile.IP: model.UpstreamUDP,
			} {
				parsed, err := model.ParseUpstream(endpoint)
				if err != nil || parsed.Kind != kind {
					t.Errorf("%q: expected %s, got %v (%v)", endpoint, kind, parsed, err)
				}
			}
			if profile.BootstrapIP != "" {
				if _, err := netip.ParseAddr(profile.BootstrapIP); err != nil {
					t.Errorf("invalid bootstrap address: %v", err)
				}
			}
		})
	}
	for _, id := range []string{"cloudflare", "google", "quad9", "adguard", "yandex", "yandex-safe", "yandex-family", "opendns", "cleanbrowsing", "controld"} {
		if !ids[id] {
			t.Errorf("missing resolver %q", id)
		}
	}
}
