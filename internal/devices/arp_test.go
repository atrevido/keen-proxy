package devices

import (
	"strings"
	"testing"
)

func TestParseARP(t *testing.T) {
	const arp = `IP address       HW type     Flags       HW address            Mask     Device
192.168.7.104    0x1         0x2         02:00:00:00:00:10     *        br0
192.168.7.254    0x1         0x0         00:00:00:00:00:00     *        br0
10.1.30.5        0x1         0x2         aa:bb:cc:dd:ee:01     *        br1
`
	got := ParseARP(strings.NewReader(arp), "br0")
	if len(got) != 1 || got["02:00:00:00:00:10"].String() != "192.168.7.104" {
		t.Errorf("ParseARP(br0) = %v", got)
	}
	if all := ParseARP(strings.NewReader(arp), ""); len(all) != 2 {
		t.Errorf("without interface filter want 2 entries, got %v", all)
	}
}
