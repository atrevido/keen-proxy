package journal

import "github.com/ordo-systems/keen-proxy/internal/netfilter"

// FirewallEvent records one watchdog repair attempt. After is populated only
// when verification fails; Outcome distinguishes success from an unrun check.
// Raw errors are local evidence and must be sanitized by diagnostic exporters.
type FirewallEvent struct {
	// Trigger identifies the scheduling path, not the cause of the rule change.
	Trigger string `json:"trigger,omitempty"`
	// DurationMS measures checking and repair, not the duration of an outage.
	// A nil value distinguishes historical entries from sub-millisecond work.
	DurationMS *int64                  `json:"duration_ms,omitempty"`
	Before     *netfilter.CheckFailure `json:"before"`
	After      *netfilter.CheckFailure `json:"after,omitempty"`
	Outcome    string                  `json:"outcome"`
	ApplyError string                  `json:"apply_error,omitempty"`
}
