package netfilter

import (
	"strings"
	"testing"
)

func TestDNSGuard(t *testing.T) {
	p := Plan{LAN: "br0", BlockEncryptedDNS: true}
	raw := p.Render()
	if !strings.Contains(raw, "--dport 853") || !strings.Contains(raw, "-i br0") {
		t.Fatal(raw)
	}
	p.BlockEncryptedDNS = false
	if strings.Contains(p.Render(), "--dport 853") {
		t.Fatal("guard enabled by default")
	}
}
