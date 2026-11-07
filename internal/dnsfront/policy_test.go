package dnsfront

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/miekg/dns"
)

func TestPolicyStrictAndExplicitDirectFallback(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ds := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) { m := new(dns.Msg); m.SetReply(r); w.WriteMsg(m) })}
	go ds.ActivateAndServe()
	defer ds.Shutdown()
	_, p, _ := net.SplitHostPort(pc.LocalAddr().String())
	u, _ := model.ParseUpstream("udp://127.0.0.1:" + p)
	q := new(dns.Msg)
	q.SetQuestion("example.com.", dns.TypeA)
	for _, fallback := range []bool{false, true} {
		route := ""
		s := New(Config{Upstreams: []model.Upstream{u}, DialContext: func(context.Context, string, string) (net.Conn, error) { return nil, fmt.Errorf("tunnel down") }, DirectFallback: fallback, OnRoute: func(r string) { route = r }})
		resp := s.ResolveRaw(context.Background(), q)
		if fallback {
			if resp.Rcode != 0 || route != "direct-fallback" {
				t.Fatal(resp, route)
			}
		} else if resp.Rcode != dns.RcodeServerFailure {
			t.Fatal("strict leaked", resp)
		}
	}
}

func TestRefusedResponseDoesNotBypassResolver(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ds := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeRefused)
		w.WriteMsg(m)
	})}
	go ds.ActivateAndServe()
	defer ds.Shutdown()
	u, _ := model.ParseUpstream("udp://" + pc.LocalAddr().String())
	s := New(Config{Upstreams: []model.Upstream{u}})
	q := new(dns.Msg)
	q.SetQuestion("example.com.", dns.TypeA)
	if r := s.ResolveRaw(context.Background(), q); r.Rcode != dns.RcodeRefused {
		t.Fatal("upstream policy bypassed", r)
	}
	if !s.ups[0].available(time.Now()) {
		t.Fatal("domain refusal quarantined server")
	}
}

func TestBootstrapUsesIP(t *testing.T) {
	u, _ := model.ParseUpstream("udp://resolver.example:53")
	got := ""
	s := New(Config{Upstreams: []model.Upstream{u}, Bootstrap: map[string]string{"resolver.example": "192.0.2.1"}, RequireBootstrap: true, DialContext: func(_ context.Context, _ string, addr string) (net.Conn, error) {
		got = addr
		return nil, fmt.Errorf("test dial")
	}})
	q := new(dns.Msg)
	q.SetQuestion("example.com.", dns.TypeA)
	s.ResolveRaw(context.Background(), q)
	if got != "192.0.2.1:53" {
		t.Fatal(got)
	}
}
