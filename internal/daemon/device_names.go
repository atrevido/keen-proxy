package daemon

import (
	"context"
	"maps"
	"net/netip"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/devices"
)

// cachedDeviceName expires observations and prevents reuse after an address change.
type cachedDeviceName struct {
	name    devices.Name
	ip      netip.Addr
	expires time.Time
}

// runDeviceNames refreshes labels independently of API calls and routing work.
func (d *Daemon) runDeviceNames(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		d.refreshDeviceNames(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Snapshot and publish under the lock; CLI/DNS work always runs outside it.
func (d *Daemon) refreshDeviceNames(ctx context.Context, now time.Time) {
	d.mu.Lock()
	neighbors := maps.Clone(d.neighbors)
	d.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	lookup := d.opt.DeviceNames
	if lookup == nil {
		lookup = devices.LocalNames
	}
	names := lookup(ctx, neighbors)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.deviceNames == nil {
		d.deviceNames = map[string]cachedDeviceName{}
	}
	for mac, cached := range d.deviceNames {
		if d.neighbors[mac] != cached.ip || !now.Before(cached.expires) {
			delete(d.deviceNames, mac)
		}
	}
	for mac, name := range names {
		ip, ok := neighbors[mac]
		if ok && d.neighbors[mac] == ip {
			d.deviceNames[mac] = cachedDeviceName{name: name, ip: ip, expires: now.Add(5 * time.Minute)}
		}
	}
}
