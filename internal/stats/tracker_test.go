package stats

import (
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
)

type fakeOut struct {
	adapter.Outbound
	tag string
}

func (f fakeOut) Tag() string { return f.tag }

func TestTrackerCountsAndSamples(t *testing.T) {
	tr := NewTracker()
	client, inbound := net.Pipe()
	defer client.Close()
	meta := adapter.InboundContext{Domain: "YouTube.com", Destination: M.SocksaddrFrom(netip.MustParseAddr("142.250.1.1"), 443)}
	c := tr.RoutedConnection(context.Background(), inbound, meta, nil, fakeOut{tag: "fi"})

	// The device sends 100 bytes (upload) and receives 300 bytes (download).
	go func() { client.Write(make([]byte, 100)); io.ReadFull(client, make([]byte, 300)) }()
	if _, err := io.ReadFull(c, make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(make([]byte, 300)); err != nil {
		t.Fatal(err)
	}

	tr.sample(time.Now())
	snap := tr.Snapshot(true)
	if len(snap.Outbounds) != 1 {
		t.Fatalf("outbounds: %+v", snap.Outbounds)
	}
	o := snap.Outbounds[0]
	if o.Tag != "fi" || o.UpTotal != 100 || o.DownTotal != 300 || o.Active != 1 || o.Unique != 1 {
		t.Errorf("summary: %+v", o)
	}
	if o.Down != 300*8/SampleInterval.Seconds() {
		t.Errorf("speed: %v", o.Down)
	}
	if len(o.Top) != 1 || o.Top[0].Host != "youtube.com" || o.Top[0].Requests != 1 {
		t.Errorf("top: %+v", o.Top)
	}

	c.Close()
	tr.sample(time.Now())
	o = tr.Snapshot(false).Outbounds[0]
	if o.Active != 0 || o.Unique != 0 || o.PeakUniq != 1 || o.Down != 0 {
		t.Errorf("after close: %+v", o)
	}
	hosts, total := tr.Hosts("fi", "tube", 10)
	if total != 1 || len(hosts) != 1 || hosts[0].Down != 300 {
		t.Errorf("hosts: %+v %d", hosts, total)
	}
	if h, _ := tr.Hosts("fi", "nomatch", 10); len(h) != 0 {
		t.Errorf("filter: %+v", h)
	}
}

func TestEvictsOldHosts(t *testing.T) {
	tr := NewTracker()
	for i := 0; i < maxHosts+10; i++ {
		meta := adapter.InboundContext{Destination: M.SocksaddrFrom(netip.AddrFrom4([4]byte{10, 0, byte(i >> 8), byte(i)}), 80)}
		a, b := net.Pipe()
		tr.RoutedConnection(context.Background(), a, meta, nil, fakeOut{tag: "direct"}).Close()
		b.Close()
	}
	if _, total := tr.Hosts("direct", "", 0); total != maxHosts {
		t.Errorf("hosts %d, want limit %d", total, maxHosts)
	}
}
