// Package stats collects per-outbound traffic rates, unique destinations,
// host totals, and router resource usage directly through sing-box
// adapter.ConnectionTracker rather than the Clash API.
package stats

import (
	"context"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/bufio"
	N "github.com/sagernet/sing/common/network"
)

const (
	SampleInterval = 2 * time.Second
	seriesLen      = 900  // 30 minutes at two-second intervals.
	maxHosts       = 3000 // Per outbound; evict the oldest hosts first.
)

// Point is one timestamped traffic-history sample.
type Point struct {
	T      int64   `json:"t"` // unix ms
	Unique int     `json:"unique"`
	Down   float64 `json:"down"` // Bits per second.
	Up     float64 `json:"up"`
}

// Host aggregates device-visible traffic and request counts for one destination.
type Host struct {
	Host     string `json:"host"`
	Down     int64  `json:"down"`
	Up       int64  `json:"up"`
	Requests int64  `json:"requests"`
	LastSeen int64  `json:"last_seen"` // unix ms
}

// Outbound summarizes one exit, either a connection or direct routing.
type Outbound struct {
	Tag       string  `json:"tag"`
	Active    int     `json:"active"`
	Unique    int     `json:"unique"`
	Down      float64 `json:"down"` // Current bits per second.
	Up        float64 `json:"up"`
	DownTotal int64   `json:"down_total"`
	UpTotal   int64   `json:"up_total"`
	PeakDown  float64 `json:"peak_down"`
	PeakUniq  int     `json:"peak_unique"`
	Series    []Point `json:"series"`
	Top       []Host  `json:"top"`
}

// Snapshot combines per-outbound traffic with the latest router resource sample.
type Snapshot struct {
	Outbounds []Outbound `json:"outbounds"`
	System    System     `json:"system"`
}

// conn binds one active flow to its outbound and per-host counters.
type conn struct {
	out      *outbound
	host     *hostStat
	hostName string
}

// hostStat permits byte/request updates without holding the tracker lock.
type hostStat struct {
	down, up, requests atomic.Int64
	lastSeen           atomic.Int64
}

// outbound combines atomic byte totals with lock-protected history and host indexes.
type outbound struct {
	tag              string
	down, up         atomic.Int64 // Cumulative bytes.
	lastDown, lastUp int64
	series           []Point
	hosts            map[string]*hostStat
}

// Tracker implements adapter.ConnectionTracker and lives for the daemon
// lifetime so engine restarts preserve accumulated statistics.
type Tracker struct {
	// OnRoute receives each new connection and its matched rule (nil when no
	// rule matched). Set it before starting the engine.
	OnRoute func(m adapter.InboundContext, rule adapter.Rule, out adapter.Outbound)

	mu     sync.Mutex
	outs   map[string]*outbound
	active map[*conn]struct{}
	sys    systemSampler
}

// NewTracker creates daemon-lifetime counters without starting the sampler.
func NewTracker() *Tracker {
	return &Tracker{outs: map[string]*outbound{}, active: map[*conn]struct{}{}}
}

func (t *Tracker) outboundLocked(tag string) *outbound {
	o := t.outs[tag]
	if o == nil {
		o = &outbound{tag: tag, hosts: map[string]*hostStat{}}
		t.outs[tag] = o
	}
	return o
}

// HostOf returns the sniffed domain, falling back to the destination address.
func HostOf(m adapter.InboundContext) string {
	if m.Domain != "" {
		return strings.ToLower(m.Domain)
	}
	if m.Destination.Fqdn != "" {
		return strings.ToLower(m.Destination.Fqdn)
	}
	return m.Destination.Addr.Unmap().String()
}

// Resolve unwraps fallback selectors to the outbound currently selected for traffic.
func Resolve(out adapter.Outbound) adapter.Outbound {
	for i := 0; i < 4 && out != nil; i++ {
		g, ok := out.(adapter.OutboundGroup)
		if !ok {
			break
		}
		next := g.Selected("tcp")
		if next == nil {
			break
		}
		out = next
	}
	return out
}

// open resolves the active outbound, bounds retained hosts, and registers a new active connection.
// It owns t.mu while updating shared host and connection maps.
func (t *Tracker) open(m adapter.InboundContext, out adapter.Outbound) *conn {
	tag := "direct"
	if out = Resolve(out); out != nil {
		tag = out.Tag()
	}
	name := HostOf(m)
	t.mu.Lock()
	defer t.mu.Unlock()
	o := t.outboundLocked(tag)
	h := o.hosts[name]
	if h == nil {
		if len(o.hosts) >= maxHosts {
			evictOldest(o.hosts)
		}
		h = &hostStat{}
		o.hosts[name] = h
	}
	h.requests.Add(1)
	h.lastSeen.Store(time.Now().UnixMilli())
	c := &conn{out: o, host: h, hostName: name}
	t.active[c] = struct{}{}
	return c
}

func evictOldest(hosts map[string]*hostStat) {
	var oldest string
	var ts int64 = 1<<63 - 1
	for name, h := range hosts {
		if v := h.lastSeen.Load(); v < ts {
			oldest, ts = name, v
		}
	}
	delete(hosts, oldest)
}

func (t *Tracker) close(c *conn) {
	t.mu.Lock()
	delete(t.active, c)
	t.mu.Unlock()
}

// counters maps inbound reads to device upload and inbound writes to download.
func (c *conn) counters() (read, write []N.CountFunc) {
	read = []N.CountFunc{func(n int64) { c.out.up.Add(n); c.host.up.Add(n) }}
	write = []N.CountFunc{func(n int64) {
		c.out.down.Add(n)
		c.host.down.Add(n)
		c.host.lastSeen.Store(time.Now().UnixMilli())
	}}
	return read, write
}

// RoutedConnection wraps a routed TCP stream with upload/download and lifetime accounting.
func (t *Tracker) RoutedConnection(_ context.Context, nc net.Conn, m adapter.InboundContext, rule adapter.Rule, out adapter.Outbound) net.Conn {
	if t.OnRoute != nil {
		t.OnRoute(m, rule, out)
	}
	c := t.open(m, out)
	r, w := c.counters()
	return &trackedConn{CounterConn: bufio.NewCounterConn(nc, r, w), done: func() { t.close(c) }}
}

// RoutedPacketConnection wraps a routed packet stream with traffic and lifetime accounting.
func (t *Tracker) RoutedPacketConnection(_ context.Context, pc N.PacketConn, m adapter.InboundContext, rule adapter.Rule, out adapter.Outbound) N.PacketConn {
	if t.OnRoute != nil {
		t.OnRoute(m, rule, out)
	}
	c := t.open(m, out)
	r, w := c.counters()
	return &trackedPacketConn{CounterPacketConn: bufio.NewCounterPacketConn(pc, r, w), done: func() { t.close(c) }}
}

// RoutedFlow leaves native flow tracking unsupported; stream wrappers supply accounting.
func (t *Tracker) RoutedFlow(context.Context, adapter.InboundContext, adapter.Rule, adapter.Outbound) tun.FlowTracker {
	return nil
}

// trackedConn attaches exactly-once flow completion to a counted TCP connection.
type trackedConn struct {
	*bufio.CounterConn
	once sync.Once
	done func()
}

// Close accounts for TCP completion once before closing the underlying connection.
func (c *trackedConn) Close() error {
	c.once.Do(c.done)
	return c.CounterConn.Close()
}

// Upstream exposes the counting wrapper to engine connection introspection.
func (c *trackedConn) Upstream() any { return c.CounterConn }

// trackedPacketConn attaches exactly-once completion to a counted packet session.
type trackedPacketConn struct {
	*bufio.CounterPacketConn
	once sync.Once
	done func()
}

// Close accounts for packet-session completion once before closing the socket.
func (c *trackedPacketConn) Close() error {
	c.once.Do(c.done)
	return c.CounterPacketConn.Close()
}

// Upstream exposes the packet counter without bypassing its tracking wrapper.
func (c *trackedPacketConn) Upstream() any { return c.CounterPacketConn }

// Run samples traffic history every SampleInterval until ctx is canceled.
func (t *Tracker) Run(ctx context.Context) {
	tick := time.NewTicker(SampleInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			t.sample(now)
		}
	}
}

// sample holds t.mu while deriving interval bit rates, unique active hosts, and bounded history.
func (t *Tracker) sample(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sys.sample("/proc")
	unique := map[*outbound]map[string]struct{}{}
	for c := range t.active {
		u := unique[c.out]
		if u == nil {
			u = map[string]struct{}{}
			unique[c.out] = u
		}
		u[c.hostName] = struct{}{}
	}
	secs := SampleInterval.Seconds()
	for _, o := range t.outs {
		down, up := o.down.Load(), o.up.Load()
		p := Point{
			T:      now.UnixMilli(),
			Unique: len(unique[o]),
			Down:   float64(down-o.lastDown) * 8 / secs,
			Up:     float64(up-o.lastUp) * 8 / secs,
		}
		o.lastDown, o.lastUp = down, up
		o.series = append(o.series, p)
		if len(o.series) > seriesLen {
			o.series = o.series[len(o.series)-seriesLen:]
		}
	}
}

// Snapshot returns current panel statistics, optionally including history.
func (t *Tracker) Snapshot(withSeries bool) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	active := map[*outbound]int{}
	for c := range t.active {
		active[c.out]++
	}
	snap := Snapshot{System: t.sys.last}
	for _, o := range t.outs {
		out := Outbound{Tag: o.tag, Active: active[o], DownTotal: o.down.Load(), UpTotal: o.up.Load()}
		for _, p := range o.series {
			out.PeakDown = max(out.PeakDown, p.Down)
			out.PeakUniq = max(out.PeakUniq, p.Unique)
		}
		if n := len(o.series); n > 0 {
			last := o.series[n-1]
			out.Unique, out.Down, out.Up = last.Unique, last.Down, last.Up
		}
		if withSeries {
			out.Series = append([]Point(nil), o.series...)
		}
		out.Top = topHosts(o.hosts, 5, "", func(a, b Host) bool { return a.Requests > b.Requests })
		snap.Outbounds = append(snap.Outbounds, out)
	}
	sort.Slice(snap.Outbounds, func(i, j int) bool { return snap.Outbounds[i].Tag < snap.Outbounds[j].Tag })
	return snap
}

// Hosts returns filtered host traffic for one outbound and its total known-host count.
func (t *Tracker) Hosts(tag, query string, limit int) (list []Host, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	o := t.outs[tag]
	if o == nil {
		return []Host{}, 0
	}
	list = topHosts(o.hosts, limit, strings.ToLower(query), func(a, b Host) bool { return a.Down > b.Down })
	return list, len(o.hosts)
}

// topHosts snapshots atomic counters, filters host names, sorts, and applies a positive limit.
// The caller protects the host map from concurrent structural changes.
func topHosts(hosts map[string]*hostStat, limit int, query string, less func(a, b Host) bool) []Host {
	out := make([]Host, 0, len(hosts))
	for name, h := range hosts {
		if query != "" && !strings.Contains(name, query) {
			continue
		}
		out = append(out, Host{Host: name, Down: h.down.Load(), Up: h.up.Load(), Requests: h.requests.Load(), LastSeen: h.lastSeen.Load()})
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
