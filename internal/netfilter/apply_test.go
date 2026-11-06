package netfilter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

// fakeRunner records calls and fails -C checks for jumps listed in missing.
type fakeRunner struct {
	calls   []string
	missing map[string]bool // "table/from/to"
	deleted map[string]int
}

func (r *fakeRunner) Run(_ context.Context, stdin, name string, args ...string) error {
	call := name + " " + strings.Join(args, " ")
	if stdin != "" {
		call += " <stdin>"
	}
	r.calls = append(r.calls, call)
	if len(args) >= 6 && args[0] == "-w" && args[1] == "-t" {
		key := args[2] + "/" + args[4]
		if len(args) >= 7 {
			key += "/" + args[len(args)-1]
		}
		switch args[3] {
		case "-C":
			if r.missing[key] {
				return errors.New("нет правила")
			}
		case "-D":
			if r.deleted[key] >= 2 { // the chain contained two duplicate jumps
				return errors.New("нет правила")
			}
			r.deleted[key]++
		}
	}
	return nil
}

func TestApplyInsertsOnlyMissingJumps(t *testing.T) {
	r := &fakeRunner{missing: map[string]bool{"nat/PREROUTING/KEENPROXY_DNS": true}}
	f := &Firewall{Run: r, IPTables: "iptables", Restore: "iptables-restore"}
	if err := f.Apply(context.Background(), testPlan(model.InterceptRedirect)); err != nil {
		t.Fatal(err)
	}
	if r.calls[0] != "iptables-restore -w --noflush <stdin>" {
		t.Errorf("первым должен быть restore, получили %q", r.calls[0])
	}
	var inserts []string
	for _, c := range r.calls {
		if strings.Contains(c, " -I ") {
			inserts = append(inserts, c)
		}
	}
	if len(inserts) != 1 || inserts[0] != "iptables -w -t nat -I PREROUTING 1 -j KEENPROXY_DNS" {
		t.Errorf("ожидалась вставка только DNS-перехода, получили %v", inserts)
	}
}

func TestRemoveDeletesJumpsBeforeChains(t *testing.T) {
	r := &fakeRunner{deleted: map[string]int{}}
	f := &Firewall{Run: r, IPTables: "iptables", Restore: "iptables-restore"}
	if err := f.Remove(context.Background(), testPlan(model.InterceptTProxy)); err != nil {
		t.Fatal(err)
	}
	firstFlush := -1
	lastDelete := -1
	for i, c := range r.calls {
		if strings.Contains(c, " -D ") {
			lastDelete = i
		}
		if strings.Contains(c, " -F ") && firstFlush < 0 {
			firstFlush = i
		}
	}
	if lastDelete < 0 || firstFlush < lastDelete {
		t.Errorf("переходы должны сниматься до очистки цепочек: %v", r.calls)
	}
	if r.deleted["nat/PREROUTING/KEENPROXY_DNS"] != 2 {
		t.Errorf("дубли перехода должны сниматься все: %v", r.deleted)
	}
}
