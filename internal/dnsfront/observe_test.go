package dnsfront

import (
	"context"
	"github.com/atrevido/keenetic-proxy/internal/model"
	"github.com/miekg/dns"
	"testing"
)

func TestResolveObservationAndRaw(t *testing.T) {
	seen := 0
	s := New(Config{Blocked: func(string) bool { return true }, Observe: func(string, *dns.Msg) { seen++ }})
	req := new(dns.Msg)
	req.SetQuestion("blocked.test.", dns.TypeA)
	if r := s.Resolve(context.Background(), req); r.Rcode != dns.RcodeSuccess || seen != 0 {
		t.Fatal("local blocking response must not be observed")
	}
	if r := s.ResolveRaw(context.Background(), req); r.Rcode != dns.RcodeServerFailure {
		t.Fatal("raw query incorrectly used block response")
	}
}

func TestObserveSuccessfulUpstream(t *testing.T) {
	seen := 0
	s := startServer(t, Config{Upstreams: []model.Upstream{fakeUpstream(t, "203.0.113.1")}, Observe: func(name string, m *dns.Msg) {
		if name != "example.com." || firstA(m) != "203.0.113.1" {
			t.Error("wrong observation")
		}
		seen++
	}})
	query(t, s.Addr(), "tcp", "example.com", dns.TypeA)
	if seen != 1 {
		t.Fatal(seen)
	}
}
