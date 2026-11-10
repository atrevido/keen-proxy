package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
	applyErr        error
	applyDelay      time.Duration
}

func (r *watchdogRunner) Run(_ context.Context, _ string, name string, args ...string) error {
	if name == "iptables-restore" {
		time.Sleep(r.applyDelay)
		return r.applyErr
	}
	if args[len(args)-1] != netfilter.ChainTCP {
		return nil
	}
	switch args[3] {
	case "-C":
		if r.missing {
			return errors.New("synthetic check failure")
		}
	case "-I":
		r.missing = r.sticky
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
			d.watch(context.Background())
			if d.restored != tc.wantCount {
				t.Fatalf("restorations = %d, want %d", d.restored, tc.wantCount)
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
