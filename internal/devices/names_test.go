package devices

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestParseDHCPNames(t *testing.T) {
	leases := ParseDHCPNames(strings.NewReader(`lease:
 ip: 192.0.2.10
 mac: AA:BB:CC:DD:EE:01
 expires: 120
 hostname: laptop
lease:
 ip: 192.0.2.11
 mac: aa:bb:cc:dd:ee:02
 expires: 0
 hostname: expired
lease:
 ip: nonsense
 mac: aa:bb:cc:dd:ee:03
 hostname: invalid
lease:
 ip: 192.0.2.12
 mac: aa:bb:cc:dd:ee:04
 hostname: *
`))
	if len(leases) != 1 || leases[0].MAC != "aa:bb:cc:dd:ee:01" || leases[0].Hostname != "laptop" || leases[0].IP.String() != "192.0.2.10" {
		t.Fatalf("leases = %+v", leases)
	}
}

func TestNameValidationAndCancellation(t *testing.T) {
	for _, name := range []string{"", "*", "bad name", "bad\x00name", "\x00bad", strings.Repeat("a", 254)} {
		if cleanName(name) != "" {
			t.Errorf("accepted %q", name)
		}
	}
	if cleanName(" printer.local. ") != "printer.local" {
		t.Fatal("normalization")
	}
	maxName := strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
	if cleanName(maxName) != maxName {
		t.Fatal("valid maximum-length hostname rejected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := NameResolver{Leases: func(context.Context) []LeaseName { t.Fatal("canceled source called"); return nil }}
	if len(r.Lookup(ctx, nil)) != 0 {
		t.Fatal("unexpected canceled results")
	}
	if len(orderedMACs(nil)) != 0 {
		t.Fatal("empty neighbors")
	}
	if len(LocalNames(ctx, nil)) != 0 {
		t.Fatal("canceled local lookup")
	}
	if len(NewNameLookup()(ctx, nil)) != 0 {
		t.Fatal("canceled background lookup")
	}
	if _, err := localPTR(ctx, netip.MustParseAddr("192.0.2.10")); err == nil {
		t.Fatal("canceled PTR unexpectedly succeeded")
	}
	if got := ParseDHCPNames(strings.NewReader("prompt\nlease:\n ip: 192.0.2.10\n mac: invalid\n hostname: laptop\n")); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestNameResolverResumesAfterCanceledBatch(t *testing.T) {
	neighbors := map[string]netip.Addr{"a": netip.MustParseAddr("192.0.2.10"), "b": netip.MustParseAddr("192.0.2.11"), "c": netip.MustParseAddr("192.0.2.12")}
	next := 0
	source := NameResolver{Leases: func(context.Context) []LeaseName { return nil }, Next: &next}
	visited := map[netip.Addr]bool{}
	for range len(neighbors) {
		ctx, cancel := context.WithCancel(context.Background())
		source.Reverse = func(_ context.Context, ip netip.Addr) (string, error) {
			visited[ip] = true
			cancel()
			return "", context.Canceled
		}
		source.Lookup(ctx, neighbors)
		cancel()
	}
	if len(visited) != len(neighbors) {
		t.Fatalf("starved clients: visited=%v", visited)
	}
	if got := source.Lookup(context.Background(), nil); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestKeeneticCommandUsesFixedReadOnlyArguments(t *testing.T) {
	started := time.Now()
	output := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "ndmc" || len(args) != 2 || args[0] != "-c" || args[1] != "show ip dhcp bindings" {
			t.Fatalf("unexpected command: %s %v", name, args)
		}
		deadline, ok := ctx.Deadline()
		if !ok || deadline.Before(started.Add(2*time.Second)) || time.Until(deadline) > 2*time.Second {
			t.Fatal("missing CLI deadline")
		}
		return []byte("lease:\n ip: 192.0.2.10\n mac: aa:bb:cc:dd:ee:01\n hostname: laptop\n"), nil
	}
	if got := readKeeneticLeases(context.Background(), output); len(got) != 1 || got[0].Hostname != "laptop" {
		t.Fatalf("leases=%v", got)
	}
	unavailable := func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("CLI unavailable") }
	if got := readKeeneticLeases(context.Background(), unavailable); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestLocalReverseDNSDoesNotRequestRecursion(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &dns.Server{PacketConn: conn, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, req *dns.Msg) {
		if req.RecursionDesired {
			t.Error("local lookup requested upstream recursion")
		}
		if req.Question[0].Name == "12.2.0.192.in-addr.arpa." {
			return
		}
		reply := new(dns.Msg)
		reply.SetReply(req)
		if req.Question[0].Name == "10.2.0.192.in-addr.arpa." {
			reply.Answer = []dns.RR{&dns.PTR{Hdr: dns.RR_Header{Name: req.Question[0].Name, Rrtype: dns.TypePTR, Class: dns.ClassINET}, Ptr: "printer.local."}}
		}
		_ = w.WriteMsg(reply)
	})}
	go func() { _ = server.ActivateAndServe() }()
	t.Cleanup(func() { _ = server.Shutdown() })
	name, err := reverseDNS(context.Background(), netip.MustParseAddr("192.0.2.10"), conn.LocalAddr().String())
	if err != nil || name != "printer.local." {
		t.Fatalf("name=%q err=%v", name, err)
	}
	name, err = reverseDNS(context.Background(), netip.MustParseAddr("192.0.2.11"), conn.LocalAddr().String())
	if err != nil || name != "" {
		t.Fatalf("empty name=%q err=%v", name, err)
	}
	if _, err = reverseDNS(context.Background(), netip.Addr{}, conn.LocalAddr().String()); err == nil {
		t.Fatal("invalid address accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	if _, err := reverseDNS(ctx, netip.MustParseAddr("192.0.2.12"), conn.LocalAddr().String()); err == nil {
		t.Fatal("silent DNS server unexpectedly answered")
	}
	if time.Since(started) >= 800*time.Millisecond {
		t.Fatal("individual DNS timeout was not bounded")
	}
}

func TestNameLookupMatchesIdentityAndFallsBack(t *testing.T) {
	neighbors := map[string]netip.Addr{"aa:bb:cc:dd:ee:01": netip.MustParseAddr("192.0.2.10"), "aa:bb:cc:dd:ee:02": netip.MustParseAddr("192.0.2.11")}
	lookups := 0
	source := NameResolver{
		Leases: func(context.Context) []LeaseName {
			return []LeaseName{{MAC: "aa:bb:cc:dd:ee:01", IP: neighbors["aa:bb:cc:dd:ee:01"], Hostname: "laptop"}, {MAC: "aa:bb:cc:dd:ee:02", IP: netip.MustParseAddr("192.0.2.99"), Hostname: "old-ip"}}
		},
		Reverse: func(_ context.Context, ip netip.Addr) (string, error) { lookups++; return "printer.local.", nil },
	}
	got := source.Lookup(context.Background(), neighbors)
	if lookups != 1 || got["aa:bb:cc:dd:ee:01"].Hostname != "laptop" || got["aa:bb:cc:dd:ee:01"].Source != "dhcp" || got["aa:bb:cc:dd:ee:02"].Hostname != "printer.local" || got["aa:bb:cc:dd:ee:02"].Source != "dns" {
		t.Fatalf("names=%+v lookups=%d", got, lookups)
	}
	source.Reverse = func(context.Context, netip.Addr) (string, error) { return "", errors.New("unavailable") }
	if len(source.Lookup(context.Background(), neighbors)) != 1 {
		t.Fatal("DNS failure must not discard DHCP names")
	}
}
