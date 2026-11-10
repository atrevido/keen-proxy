package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/journal"
)

func TestDemoFirewallAudit(t *testing.T) {
	s, fake := newServer()
	s.D = NewDemo(fake.cfg)
	w := do(t, s.Handler(), "GET", "/api/journal/audit?target=firewall", "", s.Token, lan)
	if w.Code != 200 {
		t.Fatalf("audit status: %d", w.Code)
	}
	var page struct {
		Entries []journal.Entry `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 4 {
		t.Fatalf("demo recovery entries: %+v", page.Entries)
	}
	seen := map[string]bool{}
	times := map[time.Time]bool{}
	for _, e := range page.Entries {
		if times[e.Time] {
			t.Fatalf("demo history collapsed to one timestamp: %+v", page.Entries)
		}
		times[e.Time] = true
		if age := time.Since(e.Time); age <= 0 || age >= 5*time.Minute {
			t.Fatalf("demo event outside recent history: %+v", e)
		}
		if e.Firewall == nil {
			if !strings.Contains(e.Details, "trigger: not recorded;") {
				t.Fatalf("legacy demo must explicitly identify missing source metadata: %s", e.Details)
			}
			seen["legacy"] = true
			continue
		}
		f := e.Firewall
		if f.DurationMS == nil || *f.DurationMS < 0 || f.Before == nil {
			t.Fatalf("missing evidence: %+v", f)
		}
		seen[f.Trigger+":"+f.Outcome] = true
		if f.Outcome == "restored" {
			if e.Action != "Interception rules restored" || e.Result != "warn" {
				t.Fatalf("incorrect recovered event: %+v", e)
			}
		} else if e.Action != "Interception rules not restored" || e.Result != "error" || f.After == nil {
			t.Fatalf("incorrect failure event: %+v", e)
		}
	}
	for _, key := range []string{"legacy", "netfilter_hook:restored", "watchdog:restored", "netfilter_hook:verify_failed"} {
		if !seen[key] {
			t.Fatalf("missing demo case: %s", key)
		}
	}
}
