package dnsfront

import (
	"context"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/ordo-systems/keen-proxy/internal/model"
)

// fakeUpstream serves the specified A record over UDP and TCP.
func fakeUpstream(t *testing.T, ip string) model.Upstream {
	t.Helper()
	h := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP(ip)}}
		w.WriteMsg(m)
	})
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", pc.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	udp := &dns.Server{PacketConn: pc, Handler: h}
	tcp := &dns.Server{Listener: ln, Handler: h}
	go udp.ActivateAndServe()
	go tcp.ActivateAndServe()
	t.Cleanup(func() { udp.Shutdown(); tcp.Shutdown() })
	addr := pc.LocalAddr().(*net.UDPAddr)
	return model.Upstream{Kind: model.UpstreamUDP, Host: "127.0.0.1", Port: uint16(addr.Port)}
}

// deadUpstream returns an address with no listener.
func deadUpstream(t *testing.T) model.Upstream {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()
	return model.Upstream{Kind: model.UpstreamUDP, Host: "127.0.0.1", Port: uint16(port)}
}

func startServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	cfg.Listen = "127.0.0.1:0"
	if cfg.Timeout == 0 {
		cfg.Timeout = 300 * time.Millisecond
	}
	s := New(cfg)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func query(t *testing.T, addr, network, name string, qtype uint16) *dns.Msg {
	t.Helper()
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)
	c := &dns.Client{Net: network, Timeout: 3 * time.Second}
	resp, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("%s %s: %v", network, name, err)
	}
	return resp
}

func firstA(m *dns.Msg) string {
	for _, rr := range m.Answer {
		if a, ok := rr.(*dns.A); ok {
			return a.A.String()
		}
	}
	return ""
}

func TestFailoverUDPAndTCP(t *testing.T) {
	s := startServer(t, Config{Upstreams: []model.Upstream{deadUpstream(t), fakeUpstream(t, "1.2.3.4")}})
	for _, network := range []string{"udp", "tcp"} {
		if got := firstA(query(t, s.Addr(), network, "example.com", dns.TypeA)); got != "1.2.3.4" {
			t.Errorf("%s: response %q, want 1.2.3.4 from the second upstream", network, got)
		}
	}
	if s.ups[0].available(time.Now()) {
		t.Error("failed upstream must be quarantined")
	}
}

func TestFallbackWhenAllUpstreamsDown(t *testing.T) {
	fb := fakeUpstream(t, "5.6.7.8")
	s := startServer(t, Config{Upstreams: []model.Upstream{deadUpstream(t)}, Fallback: fb.Addr()})
	if got := firstA(query(t, s.Addr(), "udp", "example.com", dns.TypeA)); got != "5.6.7.8" {
		t.Errorf("response %q, want system resolver 5.6.7.8", got)
	}
}

func TestServfailWhenNothingAnswers(t *testing.T) {
	s := startServer(t, Config{Upstreams: []model.Upstream{deadUpstream(t)}})
	if rc := query(t, s.Addr(), "udp", "example.com", dns.TypeA).Rcode; rc != dns.RcodeServerFailure {
		t.Errorf("rcode = %s, want SERVFAIL", dns.RcodeToString[rc])
	}
	if !s.ups[0].available(time.Now()) {
		t.Error("quarantine resets after total failure to allow retries")
	}
}

func TestBlockedAndAAAA(t *testing.T) {
	s := startServer(t, Config{
		Upstreams: []model.Upstream{fakeUpstream(t, "1.2.3.4")},
		Blocked:   func(n string) bool { return strings.HasSuffix(n, "ads.example.com.") },
		DropAAAA:  true,
	})
	if got := firstA(query(t, s.Addr(), "udp", "x.ads.example.com", dns.TypeA)); got != "0.0.0.0" {
		t.Errorf("blocked domain: %q, want 0.0.0.0", got)
	}
	if m := query(t, s.Addr(), "udp", "example.com", dns.TypeAAAA); m.Rcode != dns.RcodeSuccess || len(m.Answer) != 0 {
		t.Errorf("AAAA must be an empty NOERROR, got %v", m)
	}
}

func TestDoH(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req := new(dns.Msg)
		if r.Method != http.MethodPost || req.Unpack(body) != nil {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		m := new(dns.Msg)
		m.SetReply(req)
		m.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: req.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.ParseIP("9.9.9.9")}}
		out, _ := m.Pack()
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(out)
	}))
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())

	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://"))
	var p uint16
	for _, c := range port {
		p = p*10 + uint16(c-'0')
	}
	s := New(Config{Upstreams: []model.Upstream{{Kind: model.UpstreamHTTPS, Host: host, Port: p, Path: "/dns-query"}}, TLSRoots: roots, Timeout: time.Second})
	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)
	if got := firstA(s.Resolve(context.Background(), req)); got != "9.9.9.9" {
		t.Errorf("DoH: %q, want 9.9.9.9", got)
	}
}
