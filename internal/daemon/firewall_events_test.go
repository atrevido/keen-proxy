package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/journal"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/ordo-systems/keen-proxy/internal/netfilter"
)

func eventDaemon(t *testing.T, interval time.Duration) (*Daemon, *watchdogRunner) {
	t.Helper()
	arp := filepath.Join(t.TempDir(), "arp")
	if err := os.WriteFile(arp, []byte("IP address HW type Flags HW address Mask Device\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &watchdogRunner{missing: true, applyDelay: 5 * time.Millisecond}
	d := New(Options{ARPPath: arp, WatchInterval: interval, Firewall: &netfilter.Firewall{Run: runner, IPTables: "iptables", Restore: "iptables-restore"}})
	d.cfg = &model.Config{}
	d.cfg.Defaults()
	d.plan = netfilter.Plan{Mode: model.InterceptTProxy}
	d.planOn = true
	return d, runner
}

func TestFirewallEventRecoversBeforePolling(t *testing.T) {
	d, _ := eventDaemon(t, time.Hour)
	var logs bytes.Buffer
	d.log = slog.New(slog.NewTextHandler(&logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); d.runWatchdog(ctx) }()
	defer func() { cancel(); <-done }()
	d.NotifyFirewallChange()
	deadline := time.After(time.Second)
	for {
		d.mu.Lock()
		restored := d.restored
		d.mu.Unlock()
		if restored == 1 {
			assertFirewallTrigger(t, d, "netfilter_hook")
			if logs.Len() != 0 {
				t.Fatalf("event recovery duplicated into system log: %s", logs.String())
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("event did not repair firewall before polling")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestWatchdogRecoversWithoutEventSource(t *testing.T) {
	d, _ := eventDaemon(t, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); d.runWatchdog(ctx) }()
	defer func() { cancel(); <-done }()
	deadline := time.After(time.Second)
	for {
		d.mu.Lock()
		restored := d.restored
		d.mu.Unlock()
		if restored == 1 {
			assertFirewallTrigger(t, d, "watchdog")
			return
		}
		select {
		case <-deadline:
			t.Fatal("polling fallback did not repair firewall")
		case <-time.After(time.Millisecond):
		}
	}
}

// Read the public journal representation, including zero/absent timing semantics.
func assertFirewallTrigger(t *testing.T, d *Daemon, want string) {
	t.Helper()
	entries := d.Journal(journal.Audit).Find(journal.Query{}, nil).Entries
	if len(entries) != 1 {
		t.Fatalf("expected one recovery event, got %+v", entries)
	}
	raw, err := json.Marshal(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	var event struct {
		Firewall struct {
			Trigger    string `json:"trigger"`
			DurationMS *int64 `json:"duration_ms"`
		}
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	if event.Firewall.Trigger != want || event.Firewall.DurationMS == nil || *event.Firewall.DurationMS < 5 {
		t.Fatalf("missing trigger or measured recovery duration: %s", raw)
	}
}

func TestFirewallNotificationsDoNotBlockWithoutConsumer(t *testing.T) {
	d, _ := eventDaemon(t, time.Hour)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			d.NotifyFirewallChange()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("event producer blocked")
	}
}

func TestFirewallEventDoesNotInstallAfterCancellation(t *testing.T) {
	d, _ := eventDaemon(t, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d.NotifyFirewallChange()
	d.runWatchdog(ctx)
	if d.restored != 0 {
		t.Fatal("canceled daemon restored firewall")
	}
}

func TestFirewallEventLeavesStoppedInterceptionAlone(t *testing.T) {
	d, runner := eventDaemon(t, time.Hour)
	d.planOn = false
	d.checkFirewallEvent(context.Background())
	if d.restored != 0 || !runner.missing {
		t.Fatal("event reinstalled stopped interception")
	}
}
