package daemon

import (
	"bufio"
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/ordo-systems/keen-proxy/internal/dnsfront"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/miekg/dns"
)

func TestEncryptedDNSThroughEngineOutbound(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		q := new(dns.Msg)
		if q.Unpack(raw) != nil {
			t.Error("bad DNS")
		}
		m := new(dns.Msg)
		m.SetReply(q)
		raw, _ = m.Pack()
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(raw)
	}))
	defer upstream.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var connects atomic.Int32
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r, err := http.ReadRequest(bufio.NewReader(conn))
				if err != nil {
					return
				}
				if r.Method != "CONNECT" {
					return
				}
				connects.Add(1)
				remote, err := net.Dial("tcp", upstream.Listener.Addr().String())
				if err != nil {
					return
				}
				defer remote.Close()
				io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n")
				go io.Copy(remote, conn)
				io.Copy(conn, remote)
			}()
		}
	}()
	d := New(Options{RuleSetDir: t.TempDir()})
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	p, _ := strconv.Atoi(port)
	cfg := &model.Config{Connections: []model.Connection{{ID: "vpn", Type: model.ConnSingBox, Outbound: map[string]any{"type": "http", "server": "127.0.0.1", "server_port": p}}}}
	cfg.Defaults()
	host := upstream.Certificate().DNSNames[0]
	_, tlsPort, _ := net.SplitHostPort(upstream.Listener.Addr().String())
	cfg.DNS.Route = "connection"
	cfg.DNS.Connection = "vpn"
	cfg.DNS.Failure = "strict"
	cfg.DNS.Upstreams = []string{"https://" + net.JoinHostPort(host, tlsPort) + "/dns-query"}
	cfg.DNS.Bootstrap = map[string]string{host: "127.0.0.1"}
	d.live.Store(cfg)
	raw, _ := json.Marshal(map[string]any{"outbounds": []map[string]any{{"type": "http", "tag": "vpn", "server": "127.0.0.1", "server_port": p}}})
	if _, err := d.eng.Apply(raw); err != nil {
		t.Fatal(err)
	}
	defer d.eng.Close()
	c := d.dnsConfig(cfg.DNS)
	c.TLSRoots = x509.NewCertPool()
	c.TLSRoots.AddCert(upstream.Certificate())
	resolver := dnsfront.New(c)
	defer resolver.CloseIdle()
	q := new(dns.Msg)
	q.SetQuestion("example.com.", dns.TypeA)
	if r := resolver.ResolveRaw(context.Background(), q); r.Rcode != 0 || connects.Load() != 1 {
		t.Fatal(r, connects.Load())
	}
	// With the same bootstrap destination reachable directly, a dead proxy still
	// must cause strict failure, proving it cannot silently dial the destination.
	ln.Close()
	resolver.CloseIdle()
	resolver = dnsfront.New(c)
	defer resolver.CloseIdle()
	if r := resolver.ResolveRaw(context.Background(), q); r.Rcode != dns.RcodeServerFailure {
		t.Fatal("strict bypassed dead outbound")
	}
}
