// Package devices discovers LAN addresses and optional published hostnames.
package devices

import (
	"bufio"
	"io"
	"net"
	"net/netip"
	"os"
	"strings"
)

const arpComplete = "0x2" // ATF_COM: completed ARP entry.

// ReadARP reads MAC-to-IPv4 mappings from a procfs ARP file, skipping incomplete
// entries. An empty iface accepts every interface.
func ReadARP(path, iface string) (map[string]netip.Addr, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseARP(f, iface), nil
}

// ParseARP extracts completed IPv4 neighbors, normalizing MAC addresses to lowercase.
func ParseARP(r io.Reader, iface string) map[string]netip.Addr {
	out := map[string]netip.Addr{}
	sc := bufio.NewScanner(r)
	sc.Scan() // Header.
	for sc.Scan() {
		// IP address  HW type  Flags  HW address  Mask  Device
		f := strings.Fields(sc.Text())
		if len(f) < 6 || f[2] != arpComplete || (iface != "" && f[5] != iface) {
			continue
		}
		ip, err := netip.ParseAddr(f[0])
		if err != nil {
			continue
		}
		mac, err := net.ParseMAC(f[3])
		if err != nil {
			continue
		}
		out[mac.String()] = ip
	}
	return out
}
