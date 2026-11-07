package dnsfront

import (
	"context"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/miekg/dns"
)

func TestDoHBootstrapPreservesTLSIdentity(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		q := new(dns.Msg)
		q.Unpack(raw)
		m := new(dns.Msg)
		m.SetReply(q)
		raw, _ = m.Pack()
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(raw)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	host := srv.Certificate().DNSNames[0]
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	target := ""
	cfg := Config{Upstreams: []model.Upstream{{Kind: model.UpstreamHTTPS, Host: host, Port: uint16(p), Path: "/dns-query"}}, Bootstrap: map[string]string{host: "127.0.0.1"}, RequireBootstrap: true, TLSRoots: roots, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		target = addr
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}
	q := new(dns.Msg)
	q.SetQuestion("example.com.", dns.TypeA)
	if r := New(cfg).ResolveRaw(context.Background(), q); r.Rcode != 0 {
		t.Fatal(r)
	}
	if target != net.JoinHostPort("127.0.0.1", port) {
		t.Fatal(target)
	}
	cfg.Upstreams[0].Host = "wrong.example"
	cfg.Bootstrap["wrong.example"] = "127.0.0.1"
	if r := New(cfg).ResolveRaw(context.Background(), q); r.Rcode != dns.RcodeServerFailure {
		t.Fatal("wrong TLS identity accepted")
	}
}
