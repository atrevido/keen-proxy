package dnsfront

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/atrevido/keenetic-proxy/internal/model"
	"github.com/miekg/dns"
)

// clientKey carries the requesting client address into DNS block audit callbacks.
type clientKey struct{}

// exchangeTransport uses configured or explicit direct dialing with bootstrap IPs and TLS identity.
// It bounds response reads and retries truncated UDP answers over TCP.
func (s *Server) exchangeTransport(ctx context.Context, u model.Upstream, req *dns.Msg, direct bool) (*dns.Msg, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	address := u.Addr()
	if ip := s.cfg.Bootstrap[u.Host]; ip != "" {
		address = net.JoinHostPort(ip, strconv.Itoa(int(u.Port)))
	} else if s.cfg.RequireBootstrap {
		if _, err := netip.ParseAddr(u.Host); err != nil {
			return nil, fmt.Errorf("нет bootstrap IP для %s", u.Host)
		}
	}
	dial := s.cfg.DialContext
	if direct || dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	open := func(ctx context.Context, network, _ string) (net.Conn, error) { return dial(ctx, network, address) }
	if u.Kind == model.UpstreamHTTPS {
		q := req.Copy()
		q.Id = 0
		wire, err := q.Pack()
		if err != nil {
			return nil, err
		}
		key := u.Addr() + u.Path + strconv.FormatBool(direct)
		s.clientsMu.Lock()
		client := s.clients[key]
		if client == nil {
			tr := &http.Transport{DialContext: open, ForceAttemptHTTP2: true, MaxIdleConns: 4, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, TLSClientConfig: &tls.Config{ServerName: u.Host, MinVersion: tls.VersionTLS12, RootCAs: s.cfg.TLSRoots}}
			client = &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			s.clients[key] = client
		}
		s.clientsMu.Unlock()

		r, err := http.NewRequestWithContext(ctx, "POST", "https://"+u.Addr()+u.Path, bytes.NewReader(wire))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/dns-message")
		r.Header.Set("Accept", "application/dns-message")
		res, err := client.Do(r)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("DoH HTTP %d", res.StatusCode)
		}
		if ct := res.Header.Get("Content-Type"); ct != "application/dns-message" {
			return nil, fmt.Errorf("DoH content type %q", ct)
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, 65536))
		if err != nil {
			return nil, err
		}
		if len(raw) > 65535 {
			return nil, fmt.Errorf("DoH response too large")
		}
		resp := new(dns.Msg)
		if err := resp.Unpack(raw); err != nil {
			return nil, err
		}
		if !validResponse(q, resp) {
			return nil, fmt.Errorf("DoH question mismatch")
		}
		return resp, nil
	}
	exchange := func(network string) (*dns.Msg, error) {
		conn, err := open(ctx, network, address)
		if err != nil {
			return nil, err
		}
		defer conn.Close()
		rawConn := conn
		stop := context.AfterFunc(ctx, func() { rawConn.Close() })
		defer stop()
		if u.Kind == model.UpstreamTLS {
			tc := tls.Client(conn, &tls.Config{ServerName: u.Host, MinVersion: tls.VersionTLS12, RootCAs: s.cfg.TLSRoots})
			if err := tc.HandshakeContext(ctx); err != nil {
				return nil, err
			}
			conn = tc
		}
		c := &dns.Client{Net: network, UDPSize: dns.DefaultMsgSize}
		resp, _, err := c.ExchangeWithConnContext(ctx, req, &dns.Conn{Conn: conn, UDPSize: dns.DefaultMsgSize})
		return resp, err
	}
	if u.Kind == model.UpstreamTLS {
		return exchange("tcp")
	}
	resp, err := exchange("udp")
	if err == nil && resp.Truncated {
		return exchange("tcp")
	}
	return resp, err
}

// validResponse binds a DNS response to the requested ID and single question.
func validResponse(q, r *dns.Msg) bool {
	if !r.Response || r.Id != q.Id || len(r.Question) != 1 || len(q.Question) != 1 {
		return false
	}
	return r.Question[0] == q.Question[0]
}
