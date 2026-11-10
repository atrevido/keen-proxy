package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/journal"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/ordo-systems/keen-proxy/internal/netfilter"
)

// watchdogRunner models a disappearing TCP jump at the command boundary.
type watchdogRunner struct {
	missing, sticky bool
	dropAfterInsert bool
	dropNextCheck   bool
	applyCalls      int
	applyErr        error
	applyDelay      time.Duration
}

func (r *watchdogRunner) Run(_ context.Context, _ string, name string, args ...string) error {
	if name == "iptables-restore" {
		r.applyCalls++
		time.Sleep(r.applyDelay)
		return r.applyErr
	}
	if args[3] == "-D" {
		if args[len(args)-1] == netfilter.ChainTCP {
			r.missing = true
		}
		return errors.New("no more jumps")
	}
	if args[len(args)-1] != netfilter.ChainTCP {
		return nil
	}
	switch args[3] {
	case "-C":
		if r.dropNextCheck {
			r.missing = true
			r.dropNextCheck = false
		}
		if r.missing {
			return errors.New("synthetic check failure")
		}
	case "-I":
		r.missing = r.sticky
		r.dropNextCheck = r.dropAfterInsert
		r.dropAfterInsert = false
	}
	return nil
}

func TestWatchdogVerifiesAndRecordsRecovery(t *testing.T) {
	for _, tc := range []struct {
		name            string
		missing, sticky bool
		applyErr        error
		wantCount       int
		wantOutcome     string
	}{
		{name: "healthy"},
		{name: "restored", missing: true, wantCount: 1, wantOutcome: "restored"},
		{name: "verification failed", missing: true, sticky: true, wantOutcome: "verify_failed"},
		{name: "apply failed", missing: true, applyErr: errors.New("synthetic apply failure"), wantOutcome: "apply_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			arp := filepath.Join(t.TempDir(), "arp")
			if err := os.WriteFile(arp, []byte("IP address HW type Flags HW address Mask Device\n"), 0600); err != nil {
				t.Fatal(err)
			}
			runner := &watchdogRunner{missing: tc.missing, sticky: tc.sticky, applyErr: tc.applyErr}
			var logs bytes.Buffer
			d := New(Options{Log: slog.New(slog.NewTextHandler(&logs, nil)), ARPPath: arp, Firewall: &netfilter.Firewall{Run: runner, IPTables: "iptables", Restore: "iptables-restore"}})
			d.cfg = &model.Config{}
			d.cfg.Defaults()
			d.plan = netfilter.Plan{Mode: model.InterceptTProxy}
			d.planOn = true
			d.watch(context.Background())
			if d.restored != tc.wantCount {
				t.Fatalf("restorations = %d, want %d", d.restored, tc.wantCount)
			}
			if tc.sticky && runner.applyCalls != 2 {
				t.Fatalf("persistent verification failure should attempt exactly two repairs, got %d", runner.applyCalls)
			}
			if tc.wantOutcome == "apply_failed" || tc.wantOutcome == "verify_failed" {
				if !strings.Contains(logs.String(), "rule restoration failed") || !strings.Contains(logs.String(), "trigger=watchdog") {
					t.Fatalf("missing system error: %s", logs.String())
				}
			} else if logs.Len() != 0 {
				t.Fatalf("routine repair duplicated into system log: %s", logs.String())
			}
			entries := d.Journal(journal.Audit).Find(journal.Query{}, nil).Entries
			if tc.wantOutcome == "" {
				if len(entries) != 0 {
					t.Fatalf("healthy firewall produced audit entries: %+v", entries)
				}
				return
			}
			if len(entries) != 1 {
				t.Fatalf("audit entries = %+v", entries)
			}
			raw, err := json.Marshal(entries[0])
			if err != nil {
				t.Fatal(err)
			}
			var event struct {
				Firewall *struct {
					Outcome    string
					Trigger    string
					DurationMS *int64 `json:"duration_ms"`
					Before     *struct {
						Table, Chain, Target, Error string
						ExitCode                    int `json:"exit_code"`
					}
					After      *struct{ Target string }
					ApplyError string `json:"apply_error"`
				}
			}
			if err := json.Unmarshal(raw, &event); err != nil {
				t.Fatal(err)
			}
			if event.Firewall == nil {
				t.Fatalf("missing firewall evidence: %s", raw)
			}
			f := event.Firewall
			if f.Trigger != "watchdog" || f.DurationMS == nil || *f.DurationMS < 0 {
				t.Fatalf("recovery source or duration missing: %s", raw)
			}
			if f.Outcome != tc.wantOutcome || f.Before == nil {
				t.Fatalf("unexpected evidence: %s", raw)
			}
			b := f.Before
			if b.Table != "nat" || b.Chain != "PREROUTING" || b.Target != "KEENPROXY_TCP" || b.ExitCode != -1 || !strings.Contains(b.Error, "synthetic check failure") {
				t.Fatalf("check failure context lost: %s", raw)
			}
			if (f.After != nil) != tc.sticky || (f.ApplyError != "") != (tc.applyErr != nil) {
				t.Fatalf("incorrect recovery evidence: %s", raw)
			}
			if !strings.Contains(entries[0].Details, "KEENPROXY_TCP") || !strings.Contains(entries[0].Details, "synthetic check failure") {
				t.Fatalf("missing readable details: %+v", entries[0])
			}
			if tc.applyErr != nil && !strings.Contains(entries[0].Details, tc.applyErr.Error()) {
				t.Fatalf("Apply failure missing from readable audit details: %+v", entries[0])
			}
			if tc.wantCount == 1 && entries[0].Action != "Interception rules restored" {
				t.Fatalf("unexpected success action: %+v", entries[0])
			}
			if tc.wantCount == 0 && entries[0].Action != "Interception rules not restored" {
				t.Fatalf("unexpected failure action: %+v", entries[0])
			}
		})
	}
}

type interruptedFirewallRunner struct {
	cancel                    context.CancelFunc
	cancelAt, calls, restores int
}

func (r *interruptedFirewallRunner) Run(ctx context.Context, _ string, name string, _ ...string) error {
	r.calls++
	if name == "iptables-restore" {
		r.restores++
	}
	if r.calls == r.cancelAt {
		r.cancel()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if r.calls == 1 {
		return errors.New("synthetic missing jump")
	}
	return nil
}

func TestFirewallRecoveryIgnoresShutdown(t *testing.T) {
	for _, trigger := range []string{"watchdog", "netfilter_hook"} {
		for _, phase := range []string{"before check", "during check", "during apply", "during verification"} {
			t.Run(trigger+"/"+phase, func(t *testing.T) {
				d, _ := eventDaemon(t, time.Hour)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				runner := &interruptedFirewallRunner{cancel: cancel}
				wantRestores := 0
				switch phase {
				case "before check":
					cancel()
				case "during check":
					runner.cancelAt = 1
				case "during apply":
					runner.cancelAt, wantRestores = 2, 1
				case "during verification":
					runner.cancelAt, wantRestores = 3+len(d.plan.Jumps()), 1
				}
				d.opt.Firewall.Run = runner
				var logs bytes.Buffer
				d.log = slog.New(slog.NewTextHandler(&logs, nil))
				if trigger == "watchdog" {
					d.watch(ctx)
				} else {
					d.checkFirewallEvent(ctx)
				}
				entries := d.Journal(journal.Audit).Find(journal.Query{}, nil).Entries
				if d.restored != 0 || len(entries) != 0 || logs.Len() != 0 {
					t.Fatalf("shutdown recorded recovery: count=%d entries=%+v logs=%s", d.restored, entries, logs.String())
				}
				if runner.restores != wantRestores || (phase == "before check" && runner.calls != 0) {
					t.Fatalf("commands after shutdown: calls=%d restores=%d, want restores=%d", runner.calls, runner.restores, wantRestores)
				}
			})
		}
	}
}

func TestWatchdogLeavesStoppedInterceptionAlone(t *testing.T) {
	for _, changedNeighbors := range []bool{false, true} {
		t.Run(fmt.Sprint(changedNeighbors), func(t *testing.T) {
			d, runner := eventDaemon(t, time.Hour)
			d.opt.RuleSetDir = t.TempDir()
			d.planOn = false
			if changedNeighbors {
				d.neighbors = map[string]netip.Addr{"00:11:22:33:44:55": netip.MustParseAddr("192.0.2.10")}
			}
			d.watch(context.Background())
			if d.restored != 0 || !runner.missing || len(d.Journal(journal.Audit).Find(journal.Query{}, nil).Entries) != 0 {
				t.Fatal("watchdog modified intentionally stopped interception")
			}
		})
	}
}

func TestWatchdogRecoversAfterFailedApplyAndRollback(t *testing.T) {
	for _, trigger := range []string{"watchdog", "netfilter_hook"} {
		t.Run(trigger, func(t *testing.T) {
			ctx := context.Background()
			d, runner := eventDaemon(t, time.Hour)
			d.opt.RuleSetDir = t.TempDir()
			d.opt.ConfigPath = filepath.Join(t.TempDir(), "config.yaml")
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			cfg := &model.Config{}
			cfg.Defaults()
			cfg.DNS.Listen = "127.0.0.1:0"
			cfg.Intercept.TCPPort = uint16(listener.Addr().(*net.TCPAddr).Port)
			listener.Close()

			d.planOn = false
			if err := d.Apply(ctx, cfg); err != nil {
				t.Fatalf("initial apply: %v", err)
			}
			stopped := false
			t.Cleanup(func() {
				if !stopped {
					d.shutdown()
				}
			})

			next := d.Config()
			next.LAN.Interface = "lan-test"
			runner.applyErr = errors.New("synthetic xtables contention")
			if err := d.Apply(ctx, next); !errors.Is(err, runner.applyErr) {
				t.Fatalf("failed apply returned %v", err)
			}
			if runner.applyCalls != 3 || d.planOn || d.Config().LAN.Interface != cfg.LAN.Interface {
				t.Fatalf("expected failed rollback to leave the previous plan pending: calls=%d on=%v", runner.applyCalls, d.planOn)
			}
			runner.applyErr = nil
			if trigger == "watchdog" {
				// Neighbor discovery must continue even while the previous plan is pending.
				d.neighbors = map[string]netip.Addr{"00:11:22:33:44:55": netip.MustParseAddr("192.0.2.10")}
				d.watch(ctx)
				if len(d.neighbors) != 0 {
					t.Fatal("watchdog left stale neighbors")
				}
			} else {
				// Residual jumps do not prove the pending rules were installed.
				runner.missing = false
				d.checkFirewallEvent(ctx)
			}
			if !d.Status(ctx).Firewall || d.restored != 1 || runner.applyCalls != 4 {
				t.Fatalf("previous plan did not recover: on=%v restorations=%d applies=%d", d.planOn, d.restored, runner.applyCalls)
			}
			d.shutdown()
			stopped = true
			if !runner.missing {
				t.Fatal("shutdown left the recovered interception rules installed")
			}
		})
	}
}

func TestFirewallRecoveryRetriesWhenRulesDisappearDuringVerification(t *testing.T) {
	d, runner := eventDaemon(t, time.Hour)
	runner.dropAfterInsert = true
	var logs bytes.Buffer
	d.log = slog.New(slog.NewTextHandler(&logs, nil))
	d.checkFirewallEvent(context.Background())

	entries := d.Journal(journal.Audit).Find(journal.Query{}, nil).Entries
	if runner.applyCalls != 2 || len(entries) != 1 || entries[0].Action != "Interception rules restored" || logs.Len() != 0 {
		t.Fatalf("transient rebuild reported failure: applies=%d audit=%+v logs=%s", runner.applyCalls, entries, logs.String())
	}
}
