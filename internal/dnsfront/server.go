// Package dnsfront handles redirected LAN DNS requests with local blocking
// and ordered upstream failover. Its routing policy controls whether requests
// may fall back to the router's system resolver.
package dnsfront

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

const (
	blockTTL     = 60
	downDuration = 30 * time.Second // Cooldown before retrying a failed upstream.
)

// Config fixes resolver routing, bootstrap data, and observation callbacks for one server.
type Config struct {
	TLSRoots         *x509.CertPool
	DialContext      func(context.Context, string, string) (net.Conn, error)
	Bootstrap        map[string]string
	RequireBootstrap bool
	DirectFallback   bool
	Route            string
	OnRoute          func(string)
	BlockReason      func(string) string
	OnBlock          func(name, client, source string)
	Observe          func(name string, response *dns.Msg)
	Listen           string
	Upstreams        []model.Upstream
	Fallback         string // System resolver host:port over UDP.
	Timeout          time.Duration
	// Blocked reports whether a domain should receive a local block response.
	Blocked func(name string) bool
	// DropAAAA returns empty AAAA answers when IPv6 tunneling is unsupported.
	// This DNS policy alone is not a complete IPv6 traffic guard.
	DropAAAA bool
	Log      *slog.Logger
}

// Server owns UDP/TCP listeners, upstream cooldowns, and reusable resolver transports.
type Server struct {
	clientsMu sync.Mutex
	clients   map[string]*http.Client
	lifetime  context.Context
	cancel    context.CancelFunc
	cfg       Config
	ups       []*upstream
	udp       *dns.Server
	tcp       *dns.Server
}

// upstream adds a synchronized retry cooldown to one configured resolver.
type upstream struct {
	model.Upstream
	mu        sync.Mutex
	downUntil time.Time
}

func (u *upstream) available(now time.Time) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return now.After(u.downUntil)
}

func (u *upstream) markDown(now time.Time) {
	u.mu.Lock()
	u.downUntil = now.Add(downDuration)
	u.mu.Unlock()
}

// New prepares resolver state without opening listener sockets.
func New(cfg Config) *Server {
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Blocked == nil {
		cfg.Blocked = func(string) bool { return false }
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{cfg: cfg, lifetime: ctx, cancel: cancel, clients: map[string]*http.Client{}}
	for _, u := range cfg.Upstreams {
		s.ups = append(s.ups, &upstream{Upstream: u})
	}
	return s
}

// Start returns after both UDP and TCP listeners are open, so firewall DNS
// redirection can be installed safely afterward.
func (s *Server) Start() error {
	h := dns.HandlerFunc(s.serve)
	udpConn, err := net.ListenPacket("udp", s.cfg.Listen)
	if err != nil {
		return err
	}
	// Bind TCP to the actual UDP address, including an automatically assigned port.
	tcpLn, err := net.Listen("tcp", udpConn.LocalAddr().String())
	if err != nil {
		udpConn.Close()
		return err
	}
	s.udp = &dns.Server{PacketConn: udpConn, Handler: h}
	s.tcp = &dns.Server{Listener: tcpLn, Handler: h}
	go s.udp.ActivateAndServe()
	go s.tcp.ActivateAndServe()
	return nil
}

// Addr returns the bound address, including an automatically assigned port.
func (s *Server) Addr() string {
	return s.udp.PacketConn.LocalAddr().String()
}

// Close cancels resolver requests and shuts down both listener transports.
func (s *Server) Close() error {
	s.CloseIdle()
	if s.udp == nil {
		return nil
	}
	return errors.Join(s.udp.Shutdown(), s.tcp.Shutdown())
}

func (s *Server) serve(w dns.ResponseWriter, req *dns.Msg) {
	ctx := context.WithValue(context.Background(), clientKey{}, w.RemoteAddr().String())
	resp := s.Resolve(ctx, req)
	resp.Id = req.Id
	if _, isUDP := w.RemoteAddr().(*net.UDPAddr); isUDP {
		size := uint16(dns.MinMsgSize)
		if opt := req.IsEdns0(); opt != nil {
			size = opt.UDPSize()
		}
		resp.Truncate(int(size))
	}
	_ = w.WriteMsg(resp)
}

// Resolve applies local blocking and ordered upstream failover under the configured policy.
func (s *Server) Resolve(ctx context.Context, req *dns.Msg) *dns.Msg {
	if len(req.Question) != 1 {
		return reply(req, dns.RcodeFormatError)
	}
	q := req.Question[0]
	reason := ""
	if s.cfg.BlockReason != nil && s.cfg.BlockReason(q.Name) == "firefox-canary" {
		return reply(req, dns.RcodeNameError)
	}
	if s.cfg.Blocked(q.Name) {
		reason = "manual"
	}
	if reason == "" && s.cfg.BlockReason != nil {
		reason = s.cfg.BlockReason(q.Name)
	}
	if reason != "" {
		if s.cfg.OnBlock != nil {
			client, _ := ctx.Value(clientKey{}).(string)
			s.cfg.OnBlock(q.Name, client, reason)
		}
		return blocked(req, q)
	}
	if s.cfg.DropAAAA && q.Qtype == dns.TypeAAAA {
		return reply(req, dns.RcodeSuccess)
	}

	resp := s.ResolveRaw(ctx, req)
	if s.cfg.Observe != nil && q.Qtype == dns.TypeA && resp.Rcode == dns.RcodeSuccess && !resp.Truncated {
		s.cfg.Observe(q.Name, resp)
	}
	return resp
}

// ResolveRaw uses configured upstreams without local synthetic block responses.
func (s *Server) ResolveRaw(ctx context.Context, req *dns.Msg) *dns.Msg {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.lifetime, cancel)
	defer stop()
	ctx = cctx
	if len(req.Question) != 1 {
		return reply(req, dns.RcodeFormatError)
	}
	now := time.Now()
	for _, u := range s.ups {
		if !u.available(now) {
			continue
		}
		resp, err := s.exchange(ctx, u.Upstream, req)
		if err == nil {
			if s.cfg.OnRoute != nil {
				route := s.cfg.Route
				if route == "" {
					route = "direct"
				}
				s.cfg.OnRoute(route)
			}
			return resp
		}
		if ctx.Err() != nil {
			return reply(req, dns.RcodeServerFailure)
		}
		u.markDown(now)
		s.cfg.Log.Warn("upstream DNS did not respond, temporarily disabled", "upstream", u.Addr(), "kind", u.Kind, "err", err)
	}
	if s.cfg.DirectFallback {
		for _, u := range s.ups {
			resp, err := s.exchangeTransport(ctx, u.Upstream, req, true)
			if err == nil {
				if s.cfg.OnRoute != nil {
					s.cfg.OnRoute("direct-fallback")
				}
				return resp
			}
		}
	}
	if s.cfg.Fallback != "" {
		resp, err := s.exchange(ctx, model.Upstream{Kind: model.UpstreamUDP, Host: hostOf(s.cfg.Fallback), Port: portOf(s.cfg.Fallback)}, req)
		if err == nil {
			if s.cfg.OnRoute != nil {
				s.cfg.OnRoute("system-fallback")
			}
			return resp
		}
		s.cfg.Log.Warn("system resolver did not respond", "addr", s.cfg.Fallback, "err", err)
	}
	// All upstreams are in cooldown; let the next request retry them.
	for _, u := range s.ups {
		u.mu.Lock()
		u.downUntil = time.Time{}
		u.mu.Unlock()
	}
	return reply(req, dns.RcodeServerFailure)
}

func (s *Server) exchange(ctx context.Context, u model.Upstream, req *dns.Msg) (*dns.Msg, error) {
	return s.exchangeTransport(ctx, u, req, false)
}

// ExchangeContext applies the initial deadline but does not interrupt an
// established UDP/TCP read on early cancellation. Close that socket explicitly.
func exchangeDNS(ctx context.Context, c *dns.Client, req *dns.Msg, addr string) (*dns.Msg, error) {
	conn, err := c.DialContext(ctx, addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	resp, _, err := c.ExchangeWithConnContext(ctx, req, conn)
	return resp, err
}

// CloseIdle releases resolver connections and cancels outstanding requests.
func (s *Server) CloseIdle() {
	s.cancel()
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	for _, c := range s.clients {
		c.CloseIdleConnections()
	}
}

func reply(req *dns.Msg, rcode int) *dns.Msg {
	m := new(dns.Msg)
	m.SetRcode(req, rcode)
	m.RecursionAvailable = true
	return m
}

func blocked(req *dns.Msg, q dns.Question) *dns.Msg {
	m := reply(req, dns.RcodeSuccess)
	hdr := dns.RR_Header{Name: q.Name, Class: dns.ClassINET, Ttl: blockTTL}
	switch q.Qtype {
	case dns.TypeA:
		hdr.Rrtype = dns.TypeA
		m.Answer = []dns.RR{&dns.A{Hdr: hdr, A: net.IPv4zero}}
	case dns.TypeAAAA:
		hdr.Rrtype = dns.TypeAAAA
		m.Answer = []dns.RR{&dns.AAAA{Hdr: hdr, AAAA: net.IPv6zero}}
	}
	return m
}

func hostOf(addr string) string {
	h, _, _ := net.SplitHostPort(addr)
	return h
}

func portOf(addr string) uint16 {
	_, p, _ := net.SplitHostPort(addr)
	var n uint16
	fmt.Sscan(p, &n)
	return n
}
