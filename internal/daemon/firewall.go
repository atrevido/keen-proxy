package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/journal"
	"github.com/ordo-systems/keen-proxy/internal/netfilter"
)

const firewallVerifyRetryDelay = 250 * time.Millisecond

// installFirewall installs the current plan and makes shutdown responsible for its cleanup.
// A failed update stays pending even when the previous entry jumps remain installed.
func (d *Daemon) installFirewall(ctx context.Context) error {
	if err := d.opt.Firewall.Apply(ctx, d.plan); err != nil {
		d.planPending = true
		return err
	}
	d.planOn, d.planPending = true, false
	return nil
}

// checkFirewall runs under d.mu. An installed or pending plan can be repaired;
// a pending plan must be applied even if residual entry jumps pass inspection.
func (d *Daemon) checkFirewall(ctx context.Context, trigger string) {
	if ctx.Err() != nil || (!d.planOn && !d.planPending) {
		return
	}
	started := time.Now()
	before := d.opt.Firewall.Check(ctx, d.plan)
	if ctx.Err() != nil || (before == nil && !d.planPending) {
		return
	}

	event := &journal.FirewallEvent{Trigger: trigger, Before: journalFirewallCheck(before)}
	d.repairFirewall(ctx, event)
	if ctx.Err() != nil {
		return
	}
	if event.Outcome == "restored" {
		d.restored++
	}
	duration := time.Since(started).Milliseconds()
	event.DurationMS = &duration
	d.recordFirewallRecovery(event)
}

// repairFirewall gives a firmware rebuild one bounded retry if rules disappear
// between installation and verification. The caller records the final outcome.
func (d *Daemon) repairFirewall(ctx context.Context, event *journal.FirewallEvent) {
	d.applyAndVerifyFirewall(ctx, event)
	if event.Outcome != "verify_failed" || ctx.Err() != nil {
		return
	}
	timer := time.NewTimer(firewallVerifyRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
		if ctx.Err() == nil {
			d.applyAndVerifyFirewall(ctx, event)
		}
	}
}

func (d *Daemon) applyAndVerifyFirewall(ctx context.Context, event *journal.FirewallEvent) {
	// Evidence describes the final attempt, which may fail before verification.
	event.After = nil
	if err := d.installFirewall(ctx); err != nil {
		event.Outcome, event.ApplyError = "apply_failed", err.Error()
		return
	}
	event.After = journalFirewallCheck(d.opt.Firewall.Check(ctx, d.plan))
	event.Outcome = "restored"
	if event.After != nil {
		event.Outcome = "verify_failed"
	}
}

func journalFirewallCheck(failure *netfilter.CheckFailure) *journal.FirewallCheck {
	if failure == nil {
		return nil
	}
	check := journal.FirewallCheck(*failure)
	return &check
}

func (d *Daemon) recordFirewallRecovery(event *journal.FirewallEvent) {
	details := fmt.Sprintf("trigger: %s; check and recovery: %d ms; outcome: %s; verified restorations since start: %d", event.Trigger, *event.DurationMS, event.Outcome, d.restored)
	if event.Before != nil {
		details += "; before: " + event.Before.String()
	}
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
	d.auditEntry(journal.Entry{
		Actor: "watchdog", Action: action, Target: "firewall", Result: result,
		Details: details, Firewall: event,
	})
}
