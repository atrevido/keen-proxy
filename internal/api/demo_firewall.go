package api

import (
	"fmt"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/journal"
	"github.com/ordo-systems/keen-proxy/internal/netfilter"
)

// demoFirewallAudit mirrors browser-demo recovery cases without changing rules.
func demoFirewallAudit(now time.Time) []journal.Entry {
	before := &netfilter.CheckFailure{Table: "nat", Chain: "PREROUTING", Target: "KEENPROXY_TCP", ExitCode: 2, Reason: "command_failed", Error: "iptables: Couldn't load target 'KEENPROXY_TCP': No such file or directory"}
	after := &netfilter.CheckFailure{Table: "filter", Chain: "FORWARD", Target: "KEENPROXY_QUIC", ExitCode: 2, Reason: "command_failed", Error: "iptables: synthetic verification failure"}
	var entries []journal.Entry
	for i, sample := range []struct {
		trigger, outcome string
		duration         int64
		after            *netfilter.CheckFailure
	}{
		{"", "restored", 49, nil},
		{"netfilter_hook", "verify_failed", 84, after},
		{"watchdog", "restored", 47, nil},
		{"netfilter_hook", "restored", 56, nil},
	} {
		action, result := "Interception rules restored", "warn"
		if sample.outcome != "restored" {
			action, result = "Interception rules not restored", "error"
		}
		entries = append(entries, journal.Entry{
			Time: now.Add(-time.Duration(4-i) * time.Minute), Actor: "watchdog", Action: action, Target: "firewall", Result: result,
			Details:  fmt.Sprintf("Synthetic diagnostic record; trigger: %s; before: %s; outcome: %s", sample.trigger, before, sample.outcome),
			Firewall: &journal.FirewallEvent{Trigger: sample.trigger, DurationMS: &sample.duration, Outcome: sample.outcome, Before: before, After: sample.after},
		})
		if sample.trigger == "" {
			entries[len(entries)-1].Firewall = nil
		}
	}
	return entries
}
