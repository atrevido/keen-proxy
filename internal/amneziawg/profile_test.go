package amneziawg

import (
	"strings"
	"testing"
)

const testProfile = `[Interface]
PrivateKey = AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=
Address = 10.40.0.2/32
DNS = 10.40.0.1
MTU = 1280
S1 = 16
S2 = 16
S3 = 16
S4 = 16
HeaderProtectionKey = AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=
ContentPaddingAddition = 4-12
RekeyAfterTime = 110-120
H1 = 1-10
H2 = 11-20
H3 = 21-30
H4 = 31-40
I1 = <b 0xaabb><r 12><t>

[Peer]
PublicKey = AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM=
AllowedIPs = 0.0.0.0/0
Endpoint = vpn.example.test:51820
PersistentKeepalive = 15-25
`

func TestParsePremiumProfileKeepsWireSettingsAndDropsSubscription(t *testing.T) {
	profile, err := Parse("# vpn://subscriber-comment\nvpn://subscriber-key\n" + testProfile + "SubscriptionKey = vpn://subscriber-field\n")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Endpoint != "vpn.example.test:51820" || profile.ClientID == "" {
		t.Fatal("missing endpoint or public client identity")
	}
	if strings.Contains(profile.Config, "subscriber") {
		t.Fatal("subscription key retained")
	}
	for _, field := range []string{"ContentPaddingAddition = 4-12", "HeaderProtectionKey = AgIC", "H1 = 1-10", "I1 = <b 0xaabb><r 12><t>", "PersistentKeepalive = 15-25"} {
		if !strings.Contains(profile.Config, field) {
			t.Errorf("lost protocol option %q", field)
		}
	}
	reparsed, err := Parse(profile.Config)
	if err != nil || reparsed.Config != profile.Config || reparsed.ClientID != profile.ClientID {
		t.Fatal("sanitized profile is not a stable, reusable configuration")
	}
}

func TestParseRejectsUnsafeProfilesWithoutLeakingValues(t *testing.T) {
	for _, tc := range []struct{ name, old, replacement string }{
		{"script", "MTU = 1280", "PostUp = highly-sensitive-command"},
		{"duplicate", "MTU = 1280", "MTU = 1280\nmtu = 1290"},
		{"empty-key", "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE=", "invalid-highly-sensitive-key"},
		{"missing-peer", "[Peer]", ""},
		{"multi-peer", "[Peer]", "[Peer]\n[Peer]"},
		{"invalid-address", "10.40.0.2/32", "0.0.0.0/0"},
		{"port-zero", "vpn.example.test:51820", "vpn.example.test:0"},
		{"overlap-lower", "H2 = 11-20", "H2 = 10-20"},
		{"overlap-upper", "H2 = 11-20", "H2 = 0-1"},
		{"reversed-range", "ContentPaddingAddition = 4-12", "ContentPaddingAddition = 12-4"},
		{"signature-allocation", "<r 12>", "<r 99999999>"},
		{"signature-negative", "<r 12>", "<r -12>"},
		{"bad-protection", "S4 = 16", "S4 = 11"},
		{"junk-range", "MTU = 1280", "MTU = 1280\nJc = 1\nJmin = 600\nJmax = 10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(strings.Replace(testProfile, tc.old, tc.replacement, 1))
			if err == nil || strings.Contains(err.Error(), "highly-sensitive") {
				t.Fatal("profile accepted or secret exposed")
			}
		})
	}
}

func TestParseProfileSizeBoundary(t *testing.T) {
	raw := testProfile + "#" + strings.Repeat("x", MaxProfileSize-len(testProfile)-1)
	if _, err := Parse(raw); err != nil {
		t.Fatal("profile at the size limit should be accepted", err)
	}
	if _, err := Parse(raw + "x"); err == nil {
		t.Fatal("profile over the size limit accepted")
	}
}

func TestParseIdentifiesMissingPeerSection(t *testing.T) {
	raw, _, _ := strings.Cut(testProfile, "[Peer]")
	_, err := Parse(raw)
	if err == nil || !strings.Contains(err.Error(), "one Interface and one Peer") {
		t.Fatalf("missing peer section should be identified, got %v", err)
	}
}
