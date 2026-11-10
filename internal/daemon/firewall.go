package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/journal"
)

// checkFirewall runs under d.mu and counts only repairs verified by a new
// entry-jump check. Inspection errors still trigger the existing repair path.
func (d *Daemon) checkFirewall(ctx context.Context, trigger string) {
	started := time.Now()
	before := d.opt.Firewall.Check(ctx, d.plan)
	if before == nil {
		return
	}
	event := &journal.FirewallEvent{Trigger: trigger, Before: before, Outcome: "apply_failed"}
	if err := d.opt.Firewall.Apply(ctx, d.plan); err != nil {
		event.ApplyError = err.Error()
	} else {
		event.After = d.opt.Firewall.Check(ctx, d.plan)
		event.Outcome = "verify_failed"
		if event.After == nil {
			event.Outcome = "restored"
			d.restored++
		}
	}
	duration := time.Since(started).Milliseconds()
	event.DurationMS = &duration
	d.recordFirewallRecovery(event)
}

func (d *Daemon) recordFirewallRecovery(event *journal.FirewallEvent) {
	details := fmt.Sprintf("trigger: %s; check and recovery: %d ms; before: %s; outcome: %s; verified restorations since start: %d", event.Trigger, *event.DurationMS, event.Before, event.Outcome, d.restored)
	if event.ApplyError != "" {
		details += "; apply: " + event.ApplyError
	}
	if event.After != nil {
		details += "; after: " + event.After.String()
	}
	action, result := "Interception rules not restored", "error"
	if event.Outcome == "restored" {
		action, result = "Interception rules restored", "warn"
	} else {
		d.log.Error("rule restoration failed", "trigger", event.Trigger, "details", details)
	}
	d.journals[journal.Audit].Add(journal.Entry{
		Actor: "watchdog", Action: action, Target: "firewall", Result: result,
		Details: details, Firewall: event,
	})
}
