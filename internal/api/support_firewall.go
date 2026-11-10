package api

import (
	"github.com/ordo-systems/keen-proxy/internal/journal"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/ordo-systems/keen-proxy/internal/netfilter"
)

// supportFirewall retains only known probe metadata. Historical journal
// payloads are untrusted, even when they claim to originate from the watchdog.
func supportFirewall(e journal.Entry) *journal.FirewallEvent {
	if e.Actor != "watchdog" || e.Target != "firewall" || e.Firewall == nil {
		return nil
	}
	outcome := known(e.Firewall.Outcome, "restored", "apply_failed", "verify_failed")
	if outcome == "" {
		return nil
	}
	return &journal.FirewallEvent{
		Trigger:    known(e.Firewall.Trigger, "netfilter_hook", "watchdog"),
		DurationMS: supportFirewallDuration(e.Firewall.DurationMS),
		Outcome:    outcome,
		Before:     supportFirewallCheck(e.Firewall.Before),
		After:      supportFirewallCheck(e.Firewall.After),
		ApplyError: failureClass(e.Firewall.ApplyError),
	}
}

func supportFirewallDuration(ms *int64) *int64 {
	if ms == nil || *ms < 0 {
		return nil
	}
	return ms
}

func supportFirewallCheck(check *journal.FirewallCheck) *journal.FirewallCheck {
	if check == nil {
		return nil
	}
	// Match a whole owned jump, rather than allowing arbitrary chain names.
	for _, jump := range (netfilter.Plan{Mode: model.InterceptTProxy}).Jumps() {
		if check.Table == jump.Table && check.Chain == jump.From && check.Target == jump.To {
			exitCode := check.ExitCode
			if exitCode < 0 || exitCode > 255 {
				exitCode = -1
			}
			return &journal.FirewallCheck{
				Table: jump.Table, Chain: jump.From, Target: jump.To, ExitCode: exitCode,
				Reason: known(check.Reason, "execution_failed", "command_failed", "check_failed", "timeout", "canceled"),
				Error:  failureClass(check.Error),
			}
		}
	}
	return nil
}
