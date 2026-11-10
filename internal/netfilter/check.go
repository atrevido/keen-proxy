package netfilter

import (
	"context"
	"errors"
	"fmt"
)

// CheckFailure identifies the first entry jump whose command failed.
// ExitCode is -1 when the command did not exit normally. Exit code 1 alone
// does not prove that a rule was removed; Error retains the command's evidence.
type CheckFailure struct {
	Table    string `json:"table"`
	Chain    string `json:"chain"`
	Target   string `json:"target"`
	ExitCode int    `json:"exit_code"`
	Reason   string `json:"reason"`
	Error    string `json:"error,omitempty"`
}

// String formats the failed probe for local system and audit journals.
func (f *CheckFailure) String() string {
	return fmt.Sprintf("%s/%s -> %s (reason=%s, exit=%d): %s", f.Table, f.Chain, f.Target, f.Reason, f.ExitCode, f.Error)
}

// Check inspects required entry jumps and retains the first command failure.
// A nil result means every required jump passed at the time it was checked.
func (f *Firewall) Check(ctx context.Context, p Plan) *CheckFailure {
	for _, j := range p.Jumps() {
		if err := f.Run.Run(ctx, "", f.IPTables, "-w", "-t", j.Table, "-C", j.From, "-j", j.To); err != nil {
			return failedCheck(ctx, j, err)
		}
	}
	return nil
}

func failedCheck(ctx context.Context, j Jump, err error) *CheckFailure {
	failure := &CheckFailure{Table: j.Table, Chain: j.From, Target: j.To, ExitCode: -1, Reason: "execution_failed", Error: err.Error()}
	var exit interface{ ExitCode() int }
	if errors.As(err, &exit) && exit.ExitCode() >= 0 {
		failure.ExitCode = exit.ExitCode()
		failure.Reason = "command_failed"
		if failure.ExitCode == 1 {
			failure.Reason = "check_failed"
		}
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		failure.Reason = "timeout"
	case errors.Is(ctx.Err(), context.Canceled), errors.Is(err, context.Canceled):
		failure.Reason = "canceled"
	}
	return failure
}
