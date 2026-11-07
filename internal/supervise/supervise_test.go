package supervise

import (
	"context"
	"testing"
	"time"
)

func TestRestartsCrashingProcess(t *testing.T) {
	p := &Process{Name: "crash", Path: "/bin/sh", Args: []string{"-c", "echo crashing; exit 1"},
		MinDelay: 10 * time.Millisecond, MaxDelay: 20 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	p.Run(ctx)
	if p.Starts() < 3 {
		t.Errorf("process must restart, starts: %d", p.Starts())
	}
}

func TestStopsOnCancel(t *testing.T) {
	p := &Process{Name: "sleep", Path: "/bin/sleep", Args: []string{"30"}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.Run(ctx); close(done) }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit after cancellation")
	}
	if p.Starts() != 1 {
		t.Errorf("starts: %d, want 1", p.Starts())
	}
}
