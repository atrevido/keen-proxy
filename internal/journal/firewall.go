package journal

import "fmt"

// FirewallEvent records one watchdog repair attempt. After is populated only
// when verification fails; Outcome distinguishes success from an unrun check.
// Raw errors are local evidence and must be sanitized by diagnostic exporters.
type FirewallEvent struct {
	// Trigger identifies the scheduling path, not the cause of the rule change.
	Trigger string `json:"trigger,omitempty"`
	// DurationMS measures checking and repair, not the duration of an outage.
	// A nil value distinguishes historical entries from sub-millisecond work.
	DurationMS *int64         `json:"duration_ms,omitempty"`
	Before     *FirewallCheck `json:"before"`
	After      *FirewallCheck `json:"after,omitempty"`
	Outcome    string         `json:"outcome"`
	ApplyError string         `json:"apply_error,omitempty"`
}

// FirewallCheck is the persisted evidence from the first unsuccessful entry-jump probe.
// ExitCode is -1 when the command did not exit normally.
type FirewallCheck struct {
	Table    string `json:"table"`
	Chain    string `json:"chain"`
	Target   string `json:"target"`
	ExitCode int    `json:"exit_code"`
	Reason   string `json:"reason"`
	Error    string `json:"error,omitempty"`
}

// String formats the evidence for the human-readable audit details.
func (f *FirewallCheck) String() string {
	return fmt.Sprintf("%s/%s -> %s (reason=%s, exit=%d): %s", f.Table, f.Chain, f.Target, f.Reason, f.ExitCode, f.Error)
}
