package netfilter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes external commands and permits recording substitutes in tests.
type Runner interface {
	Run(ctx context.Context, stdin string, name string, args ...string) error
}

// ExecRunner executes host commands and preserves stderr in command errors.
type ExecRunner struct{}

// Run executes one firewall command, forwarding optional input and preserving stderr on failure.
func (ExecRunner) Run(ctx context.Context, stdin, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// Firewall installs and removes rules through Entware iptables.
type Firewall struct {
	Run      Runner
	IPTables string // iptables executable name or path
	Restore  string // iptables-restore executable name or path
}

// NewFirewall resolves iptables commands through PATH and executes them on the host.
func NewFirewall() *Firewall {
	return &Firewall{Run: ExecRunner{}, IPTables: "iptables", Restore: "iptables-restore"}
}

// Apply loads owned chains and installs jumps idempotently, restoring rules
// after an NDM firewall rebuild when needed.
func (f *Firewall) Apply(ctx context.Context, p Plan) error {
	if err := f.Run.Run(ctx, p.Render(), f.Restore, "-w", "--noflush"); err != nil {
		return err
	}
	for _, j := range p.Jumps() {
		if f.Run.Run(ctx, "", f.IPTables, "-w", "-t", j.Table, "-C", j.From, "-j", j.To) == nil {
			continue
		}
		if err := f.Run.Run(ctx, "", f.IPTables, "-w", "-t", j.Table, "-I", j.From, "1", "-j", j.To); err != nil {
			return err
		}
	}
	return nil
}

// Remove detaches jumps before flushing and deleting owned chains so client
// DNS immediately returns to the router resolver (fail-open).
func (f *Firewall) Remove(ctx context.Context, p Plan) error {
	var errs []error
	jumps := p.Jumps()
	for i := len(jumps) - 1; i >= 0; i-- {
		j := jumps[i]
		// Remove every duplicate jump.
		for f.Run.Run(ctx, "", f.IPTables, "-w", "-t", j.Table, "-D", j.From, "-j", j.To) == nil {
		}
	}
	seen := map[string]bool{}
	for _, j := range jumps {
		key := j.Table + "/" + j.To
		if seen[key] {
			continue
		}
		seen[key] = true
		_ = f.Run.Run(ctx, "", f.IPTables, "-w", "-t", j.Table, "-F", j.To)
		if err := f.Run.Run(ctx, "", f.IPTables, "-w", "-t", j.Table, "-X", j.To); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Healthy checks that all required jumps exist; owned chains without their
// entry jumps do not count as healthy.
func (f *Firewall) Healthy(ctx context.Context, p Plan) bool {
	for _, j := range p.Jumps() {
		if f.Run.Run(ctx, "", f.IPTables, "-w", "-t", j.Table, "-C", j.From, "-j", j.To) != nil {
			return false
		}
	}
	return true
}
