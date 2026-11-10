package netfilter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/model"
)

func TestCheckRetainsCommandFailure(t *testing.T) {
	for _, tc := range []struct {
		name, script, reason        string
		code                        int
		canceled, timedOut, missing bool
	}{
		{name: "missing rule", script: "echo 'rule unavailable' >&2; exit 1", reason: "check_failed", code: 1},
		{name: "command failure", script: "echo 'resource failure' >&2; exit 4", reason: "command_failed", code: 4},
		{name: "killed by signal", script: "kill -KILL $$", reason: "execution_failed", code: -1},
		{name: "executable absent", missing: true, reason: "execution_failed", code: -1},
		{name: "canceled", canceled: true, reason: "canceled", code: -1},
		{name: "deadline", timedOut: true, reason: "timeout", code: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "iptables")
			if !tc.missing {
				if err := os.WriteFile(path, []byte("#!/bin/sh\n"+tc.script+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			if tc.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if tc.timedOut {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			}
			f := &Firewall{Run: ExecRunner{}, IPTables: path}
			got := f.Check(ctx, testPlan(model.InterceptTProxy))
			if got == nil {
				t.Fatal("failed command reported healthy")
			}
			if got.Table != "nat" || got.Chain != "PREROUTING" || got.Target != "KEENPROXY_TCP" || got.ExitCode != tc.code || got.Reason != tc.reason {
				t.Fatalf("failure context = %+v", got)
			}
			if got.Error == "" {
				t.Fatalf("raw error lost: %+v", got)
			}
			if tc.code == 1 && !strings.Contains(got.Error, "rule unavailable") {
				t.Fatalf("stderr lost: %+v", got)
			}
			if f.Healthy(ctx, testPlan(model.InterceptTProxy)) {
				t.Fatal("Healthy disagrees with detailed check")
			}
		})
	}
}

func TestCheckInspectsEveryRequiredJump(t *testing.T) {
	for _, jump := range testPlan(model.InterceptTProxy).Jumps() {
		t.Run(jump.Table+"/"+jump.From+"/"+jump.To, func(t *testing.T) {
			r := &fakeRunner{missing: map[string]bool{jump.Table + "/" + jump.From + "/" + jump.To: true}}
			f := &Firewall{Run: r, IPTables: "iptables"}
			got := f.Check(context.Background(), testPlan(model.InterceptTProxy))
			if got == nil || got.Table != jump.Table || got.Chain != jump.From || got.Target != jump.To {
				t.Fatalf("failed jump not identified: %+v", got)
			}
		})
	}
	f := &Firewall{Run: &fakeRunner{}, IPTables: "iptables"}
	if got := f.Check(context.Background(), testPlan(model.InterceptTProxy)); got != nil {
		t.Fatalf("healthy plan = %+v", got)
	}
	if !f.Healthy(context.Background(), testPlan(model.InterceptRedirect)) {
		t.Fatal("healthy redirect plan rejected")
	}
}
