package daemon

import (
	"context"
	"net/netip"
	"testing"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/devices"
	"github.com/ordo-systems/keen-proxy/internal/model"
)

func TestDiscoveredNameDoesNotOverwriteClientOrBlockAPI(t *testing.T) {
	const mac = "aa:bb:cc:dd:ee:01"
	ip := netip.MustParseAddr("192.0.2.10")
	d := &Daemon{neighbors: map[string]netip.Addr{mac: ip}, cfg: &model.Config{Clients: []model.Client{{MAC: mac, Name: "My laptop"}}}}
	calls := 0
	d.opt.DeviceNames = func(ctx context.Context, neighbors map[string]netip.Addr) map[string]devices.Name {
		calls++
		// This would deadlock if discovery held the daemon lock.
		if len(d.Devices()) != 1 {
			t.Fatal("API snapshot unavailable during discovery")
		}
		return map[string]devices.Name{mac: {Hostname: "published-laptop", Source: "dhcp"}}
	}
	now := time.Now()
	d.refreshDeviceNames(context.Background(), now)
	got := d.Devices()[0]
	if got.Client != "My laptop" || got.Hostname != "published-laptop" || got.HostnameSource != "dhcp" || d.cfg.Clients[0].Name != "My laptop" {
		t.Fatalf("device=%+v", got)
	}
	if calls != 1 {
		t.Fatalf("API must read cache, calls=%d", calls)
	}
	d.neighbors[mac] = netip.MustParseAddr("192.0.2.20")
	if d.Devices()[0].Hostname != "" {
		t.Fatal("old IP's name reused")
	}
	d.neighbors[mac] = ip
	d.opt.DeviceNames = func(context.Context, map[string]netip.Addr) map[string]devices.Name { return nil }
	d.refreshDeviceNames(context.Background(), now.Add(time.Minute))
	if d.Devices()[0].Hostname == "" {
		t.Fatal("transient source failure erased unexpired cache")
	}
	d.refreshDeviceNames(context.Background(), now.Add(6*time.Minute))
	if d.Devices()[0].Hostname != "" {
		t.Fatal("expired name retained")
	}
}

func TestDiscoveryDiscardsResultsAfterNeighborChanges(t *testing.T) {
	const mac = "aa:bb:cc:dd:ee:01"
	d := &Daemon{neighbors: map[string]netip.Addr{mac: netip.MustParseAddr("192.0.2.10")}}
	d.opt.DeviceNames = func(context.Context, map[string]netip.Addr) map[string]devices.Name {
		d.mu.Lock()
		d.neighbors[mac] = netip.MustParseAddr("192.0.2.20")
		d.mu.Unlock()
		return map[string]devices.Name{mac: {Hostname: "old-address", Source: "dns"}}
	}
	d.refreshDeviceNames(context.Background(), time.Now())
	if d.Devices()[0].Hostname != "" {
		t.Fatal("late lookup result attached to another address")
	}
	d.neighbors[mac] = netip.MustParseAddr("192.0.2.10")
	if d.Devices()[0].Hostname != "" {
		t.Fatal("discarded late result resurfaced when the old address returned")
	}
}

func TestDeviceNameWorkerStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := &Daemon{}
	calls := 0
	d.opt.DeviceNames = func(ctx context.Context, _ map[string]netip.Addr) map[string]devices.Name {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second || time.Until(deadline) <= 0 {
			t.Error("discovery lacks bounded context")
		}
		calls++
		cancel()
		return nil
	}
	d.runDeviceNames(ctx)
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}
