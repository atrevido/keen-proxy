package devices

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/netip"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/miekg/dns"
)

// Name is an observed label, never a routing identity or configuration value.
type Name struct {
	Hostname string
	Source   string
}

// LeaseName ties a DHCP label to both a MAC and its leased address.
type LeaseName struct {
	MAC      string
	IP       netip.Addr
	Hostname string
}

// NameResolver injects local DHCP and PTR sources for bounded background discovery.
type NameResolver struct {
	Leases  func(context.Context) []LeaseName
	Reverse func(context.Context, netip.Addr) (string, error)
	Next    *int // optional cursor; the owning background worker calls Lookup serially
}

// cleanName rejects absent, oversized, or whitespace/control-bearing hostnames.
func cleanName(s string) string {
	s = strings.TrimSuffix(strings.TrimSpace(s), ".")
	if s == "" || s == "*" || len(s) > 253 || strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return ""
	}
	return s
}

// ParseDHCPNames reads Keenetic's documented show ip dhcp bindings text format.
func ParseDHCPNames(r io.Reader) []LeaseName {
	var out []LeaseName
	fields := map[string]string{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), ":")
		if !ok {
			continue
		}
		if key == "lease" {
			out = appendLeaseName(out, fields)
			fields = map[string]string{}
			continue
		}
		fields[key] = strings.TrimSpace(value)
	}
	return appendLeaseName(out, fields)
}

// appendLeaseName keeps incomplete or expired records out of discovery.
func appendLeaseName(out []LeaseName, fields map[string]string) []LeaseName {
	ip, ipErr := netip.ParseAddr(fields["ip"])
	mac, macErr := net.ParseMAC(fields["mac"])
	name := cleanName(fields["hostname"])
	if ipErr != nil || macErr != nil || name == "" || fields["expires"] == "0" {
		return out
	}
	return append(out, LeaseName{MAC: mac.String(), IP: ip, Hostname: name})
}

// Lookup prefers a matching lease and then local PTR; cancellation stops further work.
func (r NameResolver) Lookup(ctx context.Context, neighbors map[string]netip.Addr) map[string]Name {
	out := map[string]Name{}
	if ctx.Err() != nil {
		return out
	}
	addLeaseNames(out, r.Leases(ctx), neighbors)
	r.addReverseNames(ctx, out, neighbors)
	return out
}

// addLeaseNames requires both identities to match the current neighbor snapshot.
func addLeaseNames(out map[string]Name, leases []LeaseName, neighbors map[string]netip.Addr) {
	for _, lease := range leases {
		if ip, ok := neighbors[lease.MAC]; ok && ip == lease.IP {
			if name := cleanName(lease.Hostname); name != "" {
				out[lease.MAC] = Name{Hostname: name, Source: "dhcp"}
			}
		}
	}
}

// orderedMACs keeps the background cursor stable while neighbors are unchanged.
func orderedMACs(neighbors map[string]netip.Addr) []string {
	macs := make([]string, 0, len(neighbors))
	for mac := range neighbors {
		macs = append(macs, mac)
	}
	sort.Strings(macs)
	return macs
}

// reverseOrder resumes after the last attempted address, including timed-out ones.
func (r NameResolver) reverseOrder(neighbors map[string]netip.Addr) []string {
	macs := orderedMACs(neighbors)
	if len(macs) == 0 || r.Next == nil {
		return macs
	}
	offset := *r.Next % len(macs)
	return append(macs[offset:], macs[:offset]...)
}

// Reverse lookups are sequential and individually bounded by the local DNS client.
func (r NameResolver) addReverseNames(ctx context.Context, out map[string]Name, neighbors map[string]netip.Addr) {
	macs := r.reverseOrder(neighbors)
	for _, mac := range macs {
		if ctx.Err() != nil {
			return
		}
		if r.Next != nil {
			*r.Next = (*r.Next + 1) % len(macs)
		}
		if _, ok := out[mac]; ok {
			continue
		}
		name, err := r.Reverse(ctx, neighbors[mac])
		if err != nil {
			continue
		}
		if name = cleanName(name); name != "" {
			out[mac] = Name{Hostname: name, Source: "dns"}
		}
	}
}

// LocalNames uses only the local router CLI and its loopback DNS listener.
func LocalNames(ctx context.Context, neighbors map[string]netip.Addr) map[string]Name {
	return (NameResolver{Leases: keeneticLeases, Reverse: localPTR}).Lookup(ctx, neighbors)
}

// NewNameLookup owns a cursor for one serial background discovery worker.
func NewNameLookup() func(context.Context, map[string]netip.Addr) map[string]Name {
	next := 0
	resolver := NameResolver{Leases: keeneticLeases, Reverse: localPTR, Next: &next}
	return resolver.Lookup
}

// CLI absence or unsupported output leaves discovery available through local DNS.
func keeneticLeases(ctx context.Context) []LeaseName {
	return readKeeneticLeases(ctx, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	})
}

// Bound CLI work separately from parsing and allow testing without starting a process.
func readKeeneticLeases(ctx context.Context, output func(context.Context, string, ...string) ([]byte, error)) []LeaseName {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	data, err := output(ctx, "ndmc", "-c", "show ip dhcp bindings")
	if err != nil {
		return nil
	}
	return ParseDHCPNames(strings.NewReader(string(data)))
}

// localPTR does not use the host's potentially external resolv.conf servers.
func localPTR(ctx context.Context, ip netip.Addr) (string, error) {
	return reverseDNS(ctx, ip, "127.0.0.1:53")
}

// reverseDNS allows disposable local test servers while production always uses loopback.
func reverseDNS(ctx context.Context, ip netip.Addr, server string) (string, error) {
	q, err := dns.ReverseAddr(ip.String())
	if err != nil {
		return "", err
	}
	message := new(dns.Msg)
	message.SetQuestion(q, dns.TypePTR)
	// Request local authoritative/cache data without asking for upstream recursion.
	message.RecursionDesired = false
	client := dns.Client{Timeout: 300 * time.Millisecond}
	answer, _, err := client.ExchangeContext(ctx, message, server)
	if err != nil {
		return "", err
	}
	for _, record := range answer.Answer {
		if ptr, ok := record.(*dns.PTR); ok {
			return ptr.Ptr, nil
		}
	}
	return "", nil
}
