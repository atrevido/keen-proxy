package daemon

import (
	"context"
	"time"
)

// NotifyFirewallChange schedules a check without blocking firmware callbacks.
// Repeated events coalesce; a change during a repair leaves one pending check.
func (d *Daemon) NotifyFirewallChange() {
	select {
	case d.firewallWake <- struct{}{}:
	default:
	}
}

// runWatchdog serializes event recovery and periodic neighbor/firewall checks.
// The ticker is retained even when no platform event source is available.
func (d *Daemon) runWatchdog(ctx context.Context) {
	tick := time.NewTicker(d.opt.WatchInterval)
	defer tick.Stop()
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			d.watch(ctx)
		case <-d.firewallWake:
			d.checkFirewallEvent(ctx)
		}
	}
}

func (d *Daemon) checkFirewallEvent(ctx context.Context) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.planOn || ctx.Err() != nil {
		return
	}
	d.checkFirewall(ctx, "netfilter_hook")
}
