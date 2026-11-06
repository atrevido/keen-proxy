// Package supervise keeps an external process running with restart backoff
// and line-oriented output logging.
package supervise

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

// Process owns restart policy and diagnostics for one external command.
type Process struct {
	Name     string
	Path     string
	Args     []string
	Log      *slog.Logger
	MinDelay time.Duration // Delay before the first restart.
	MaxDelay time.Duration
	// Healthy resets backoff after a process has run for this duration.
	Healthy time.Duration

	starts atomic.Int64
}

// Starts returns the process start count for diagnostics and tests.
func (p *Process) Starts() int64 { return p.starts.Load() }

// Run supervises the process until ctx is canceled, then sends SIGTERM.
func (p *Process) Run(ctx context.Context) {
	if p.MinDelay == 0 {
		p.MinDelay = time.Second
	}
	if p.MaxDelay == 0 {
		p.MaxDelay = 30 * time.Second
	}
	if p.Healthy == 0 {
		p.Healthy = time.Minute
	}
	if p.Log == nil {
		p.Log = slog.Default()
	}
	delay := p.MinDelay
	for ctx.Err() == nil {
		started := time.Now()
		err := p.once(ctx)
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > p.Healthy {
			delay = p.MinDelay
		}
		p.Log.Warn("процесс завершился, перезапуск", "name", p.Name, "err", err, "after", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, p.MaxDelay)
	}
}

// once waits for exit or cancellation, escalating SIGTERM to kill after five seconds.
func (p *Process) once(ctx context.Context) error {
	cmd := exec.Command(p.Path, p.Args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	p.starts.Add(1)
	go p.logLines(out)

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		return ctx.Err()
	}
}

func (p *Process) logLines(r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		p.Log.Info(sc.Text(), "process", p.Name)
	}
}
