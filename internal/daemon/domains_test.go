package daemon

import (
	"context"
	"github.com/ordo-systems/keen-proxy/internal/compile"
	"github.com/ordo-systems/keen-proxy/internal/domaincache"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/miekg/dns"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDomainsObserveAndReconcile(t *testing.T) {
	d := New(Options{RuleSetDir: t.TempDir()})
	cfg := &model.Config{Groups: []model.Group{{Name: "test", Target: "direct", Entries: []string{"example.com"}}}}
	d.live.Store(cfg)
	d.cfg = cfg
	m := new(dns.Msg)
	rr, _ := dns.NewRR("www.example.com. 60 IN A 203.0.113.9")
	m.Answer = []dns.RR{rr}
	d.observeDomain("www.example.com.", m)
	d.flushDomains()
	if len(d.Domains().Records) != 1 {
		t.Fatal("observation lost")
	}
	raw, err := os.ReadFile(filepath.Join(d.opt.RuleSetDir, compile.DomainTag(cfg.Groups[0])+".json"))
	if err != nil || !strings.Contains(string(raw), "203.0.113.9") {
		t.Fatalf("%s %v", raw, err)
	}
	changed := &model.Config{Groups: []model.Group{{Name: "other", Target: "direct", Entries: []string{"other.test"}}}}
	d.live.Store(changed)
	d.cfg = changed
	d.flushDomains()
	if len(d.Domains().Records) != 0 {
		t.Fatal("old group observations exposed")
	}
	if _, err = d.RefreshDomains(context.Background(), []string{"example.com"}, false); err == nil {
		t.Fatal("refresh of removed domain accepted")
	}
}

func TestDomainPreviewDoesNotPopulateActiveCache(t *testing.T) {
	d := New(Options{RuleSetDir: t.TempDir()})
	cfg := &model.Config{}
	cfg.Defaults()
	cfg.DNS.Upstreams = []string{"udp://127.0.0.1:1"}
	cfg.DNS.Fallback = ""
	d.live.Store(cfg)
	d.cfg = cfg
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.RefreshDomains(ctx, []string{"draft.test"}, true); err == nil {
		t.Fatal("cancellation ignored")
	}
	if len(d.Domains().Records) != 0 {
		t.Fatal("preview escaped into active cache")
	}
}

func TestRefreshStopsUnusedObservedNames(t *testing.T) {
	now := time.Now()
	cfg := &model.Config{Groups: []model.Group{{Entries: []string{"example.com"}}}}
	records := []domaincache.Record{{Name: "old.example.com", ObservedAt: now.Add(-11 * time.Minute)}, {Name: "new.example.com", ObservedAt: now.Add(-time.Minute)}}
	names := refreshDomainNames(cfg, records, now)
	if containsName(names, "old.example.com") || !containsName(names, "new.example.com") || !containsName(names, "example.com") {
		t.Fatal(names)
	}
}
func TestSameDomainDifferentGroupsIsConflict(t *testing.T) {
	cfg := &model.Config{Groups: []model.Group{{Name: "one", Target: "direct", Entries: []string{"example.com"}}, {Name: "two", Target: "block", Entries: []string{"example.com"}}}}
	records := []domaincache.Record{{Name: "example.com", Addresses: []domaincache.Address{{IP: "203.0.113.1"}}}}
	if len(domainConflicts(cfg, records)["203.0.113.1"]) != 2 {
		t.Fatal("same name hides group ambiguity")
	}
	note := dnsRouteNote(cfg, records, compile.DomainTag(cfg.Groups[0]), "203.0.113.1")
	if !strings.Contains(note, "example.com") || !strings.Contains(note, "ambiguous") {
		t.Fatal(note)
	}
}

func TestDomainResolutionInFlightCancellation(t *testing.T) {
	socket, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	received := make(chan struct{}, 4)
	go func() {
		buf := make([]byte, 1024)
		for {
			if _, _, err := socket.ReadFrom(buf); err != nil {
				return
			}
			received <- struct{}{}
		}
	}()
	d := New(Options{RuleSetDir: t.TempDir()})
	cfg := &model.Config{}
	cfg.Defaults()
	cfg.DNS.Upstreams = []string{"udp://" + socket.LocalAddr().String()}
	cfg.DNS.Fallback = ""
	d.live.Store(cfg)
	d.cfg = cfg
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := d.RefreshDomains(ctx, []string{"one.test", "two.test", "three.test", "four.test"}, true)
		done <- err
	}()
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("no DNS request")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled resolution succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("in-flight DNS did not cancel")
	}
	if len(d.domainSlots) != 0 {
		t.Fatal("resolution slots leaked")
	}
}
