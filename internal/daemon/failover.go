package daemon

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"sync"
	"time"

	M "github.com/sagernet/sing/common/metadata"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

// health tracks probe results and consecutive status duration for one connection.
type health struct {
	ok      bool
	known   bool
	since   time.Time // start of the current uninterrupted success or failure period
	checked time.Time
	latency time.Duration
	err     string
}

// record updates the latest result, restarting the duration only on status changes.
func (h *health) record(ok bool, latency time.Duration, err string, now time.Time) {
	if !h.known || h.ok != ok {
		h.since = now
	}
	h.known, h.ok, h.checked, h.latency, h.err = true, ok, now, latency, err
}

// down reports sustained failure for downAfter; untested connections remain usable.
func (h *health) down(now time.Time, downAfter time.Duration) bool {
	return h != nil && h.known && !h.ok && now.Sub(h.since) >= downAfter
}

// pick applies failover hysteresis. A usable active member yields to a higher
// priority only after upAfter of success. Once active has failed for downAfter,
// choose the first currently responsive member; keep active if none respond.
func pick(chain []string, cur string, hs map[string]*health, now time.Time, downAfter, upAfter time.Duration) string {
	ci := -1
	for i, m := range chain {
		if m == cur {
			ci = i
		}
	}
	if ci < 0 {
		ci, cur = 0, chain[0]
	}
	if cur == model.TargetDirect || !hs[cur].down(now, downAfter) {
		for _, m := range chain[:ci] {
			if h := hs[m]; m == model.TargetDirect || (h != nil && h.known && h.ok && now.Sub(h.since) >= upAfter) {
				return m
			}
		}
		return cur
	}
	for _, m := range chain {
		if h := hs[m]; m != cur && (m == model.TargetDirect || (h != nil && h.known && h.ok)) {
			return m
		}
	}
	return cur
}

// runFailover probes chain members and switches selectors until ctx is canceled.
func (d *Daemon) runFailover(ctx context.Context) {
	for {
		d.mu.Lock()
		f := d.cfg.Failover
		members := probeMembers(d.cfg)
		d.mu.Unlock()

		if len(members) > 0 {
			results := d.probeAll(ctx, members, f)
			d.mu.Lock()
			now := time.Now()
			for id, r := range results {
				h := d.health[id]
				if h == nil {
					h = &health{}
					d.health[id] = h
				}
				h.record(r.err == nil, r.latency, errText(r.err), now)
			}
			d.switchLocked(now)
			d.mu.Unlock()
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(f.Interval) * time.Second):
		}
	}
}

// probeMembers includes standalone profiles so UI health does not depend on failover.
func probeMembers(cfg *model.Config) []string {
	ids := make([]string, 0, len(cfg.Connections))
	for _, c := range cfg.Connections {
		ids = append(ids, c.ID)
	}
	return ids
}

// probeResult carries one HTTP probe outcome back to serialized health updates.
type probeResult struct {
	latency time.Duration
	err     error
}

// probeAll probes members concurrently with per-request deadlines derived from the interval.
func (d *Daemon) probeAll(ctx context.Context, ids []string, f model.Failover) map[string]probeResult {
	timeout := min(10*time.Second, max(3*time.Second, time.Duration(f.Interval)*time.Second))
	out := make(map[string]probeResult, len(ids))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			lat, err := d.probe(pctx, id, f.URL)
			mu.Lock()
			out[id] = probeResult{lat, err}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// probe sends one HTTP request through a connection; any response proves
// reachability, regardless of content. It resolves the probe host locally
// because a WireGuard outbound may lack a sing-box DNS resolver.
func (d *Daemon) probe(ctx context.Context, id, rawURL string) (time.Duration, error) {
	out, engCtx, ok := d.eng.Outbound(id)
	if !ok {
		return 0, fmt.Errorf("connection is not running")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0, err
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	p, _ := strconv.Atoi(port)
	addr, err := netip.ParseAddr(u.Hostname())
	if err != nil {
		ips, lerr := net.DefaultResolver.LookupNetIP(ctx, "ip4", u.Hostname())
		if lerr != nil || len(ips) == 0 {
			return 0, fmt.Errorf("probe address %s did not resolve: %v", u.Hostname(), lerr)
		}
		addr = ips[0]
	}
	// sing-box outbounds need the engine context; cancellation follows our ctx.
	dctx, cancel := context.WithCancel(engCtx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()

	start := time.Now()
	conn, err := out.DialContext(dctx, "tcp", M.SocksaddrFrom(addr.Unmap(), uint16(p)))
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	client := http.Client{
		Transport: &http.Transport{
			DialContext:       func(context.Context, string, string) (net.Conn, error) { return conn, nil },
			DisableKeepAlives: true,
			// Probe reachability only: root certificates may be absent,
			// and the request carries no secrets.
			TLSClientConfig: &tls.Config{ServerName: u.Hostname(), InsecureSkipVerify: true},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return time.Since(start), nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// switchLocked selects each chain's active member and updates selectors under d.mu.
func (d *Daemon) switchLocked(now time.Time) {
	f := d.cfg.Failover
	downAfter := time.Duration(f.DownAfter) * time.Second
	upAfter := time.Duration(f.UpAfter) * time.Second
	for _, fc := range d.chains {
		cur := d.active[fc.ID]
		if cur == "" {
			cur = fc.ID
		}
		next := pick(fc.Chain, cur, d.health, now, downAfter, upAfter)
		if next == cur {
			if cur != model.TargetDirect && d.health[cur].down(now, downAfter) && !d.allDown[fc.ID] {
				d.allDown[fc.ID] = true
				d.log.Error("no connection in the chain is available", "connection", fc.ID)
				d.audit("watchdog", "No working connection", fc.ID, "error",
					fmt.Sprintf("none of %s is available — traffic remains on «%s»", d.chainNames(fc.Chain), d.connName(cur)))
			}
			continue
		}
		if !d.eng.Select(fc.Tag, next) {
			d.log.Error("selector did not switch", "connection", fc.ID, "to", next)
			continue
		}
		d.active[fc.ID] = next
		d.allDown[fc.ID] = false
		if index(fc.Chain, next) > index(fc.Chain, cur) {
			h := d.health[cur]
			d.log.Warn("connection is unavailable, switching to fallback", "connection", fc.ID, "from", cur, "to", next, "err", h.err)
			d.audit("watchdog", "Switch to fallback", fc.ID, "warn",
				fmt.Sprintf("«%s» unavailable for %s (%s) — traffic via «%s»", d.connName(cur), roundDur(now.Sub(h.since)), h.err, d.connName(next)))
		} else {
			d.log.Info("connection is available again, switching back", "connection", fc.ID, "from", cur, "to", next)
			action := "Return to primary"
			if next != fc.ID {
				action = "Return to higher priority"
			}
			d.audit("watchdog", action, fc.ID, "ok",
				fmt.Sprintf("«%s» available again for %s — traffic via it instead of «%s»", d.connName(next), roundDur(now.Sub(d.health[next].since)), d.connName(cur)))
		}
	}
}

// reselectLocked restores selections after engine restarts, which reset to the
// primary member, and discards state for removed chains. Requires d.mu.
func (d *Daemon) reselectLocked() {
	keep := map[string]bool{}
	for _, fc := range d.chains {
		keep[fc.ID] = true
		cur, ok := d.active[fc.ID]
		if !ok || cur == fc.ID {
			continue
		}
		if index(fc.Chain, cur) < 0 || !d.eng.Select(fc.Tag, cur) {
			delete(d.active, fc.ID)
		}
	}
	for id := range d.active {
		if !keep[id] {
			delete(d.active, id)
			delete(d.allDown, id)
		}
	}
	members := map[string]bool{}
	for _, fc := range d.chains {
		for _, m := range fc.Chain {
			members[m] = true
		}
	}
	for id := range d.health {
		if !members[id] {
			delete(d.health, id)
		}
	}
}

func index(chain []string, id string) int {
	for i, m := range chain {
		if m == id {
			return i
		}
	}
	return -1
}

func roundDur(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d s", int(d.Seconds()))
	}
	return fmt.Sprintf("%d min %d s", int(d.Minutes()), int(d.Seconds())%60)
}

func (d *Daemon) connName(id string) string {
	if id == model.TargetDirect {
		return "Direct"
	}
	for _, cn := range d.cfg.Connections {
		if cn.ID == id && cn.Name != "" {
			return cn.Name
		}
	}
	return id
}

func (d *Daemon) chainNames(chain []string) string {
	s := ""
	for i, m := range chain {
		if i > 0 {
			s += " → "
		}
		s += "«" + d.connName(m) + "»"
	}
	return s
}

// FailoverInfo exposes fallback chain selection to the UI.
type FailoverInfo struct {
	Active string   `json:"active"` // currently selected chain member
	Chain  []string `json:"chain"`
}

// ConnHealth exposes the latest HTTP reachability probe through a connection.
type ConnHealth struct {
	OK      bool      `json:"ok"`
	Checked time.Time `json:"checked"`
	Since   time.Time `json:"since"` // start of the current health state
	Latency int64     `json:"latency_ms"`
	Error   string    `json:"error,omitempty"`
}

// failoverInfoLocked snapshots chain choices and connection health while d.mu is held.
func (d *Daemon) failoverInfoLocked() (map[string]FailoverInfo, map[string]ConnHealth) {
	fo := map[string]FailoverInfo{}
	for _, fc := range d.chains {
		active := d.active[fc.ID]
		if active == "" {
			active = fc.ID
		}
		fo[fc.ID] = FailoverInfo{Active: active, Chain: fc.Chain}
	}
	hs := map[string]ConnHealth{}
	for _, c := range d.cfg.Connections {
		id := c.ID
		h := d.health[id]
		if h != nil && h.known {
			hs[id] = ConnHealth{OK: h.ok, Checked: h.checked, Since: h.since, Latency: h.latency.Milliseconds(), Error: h.err}
		}
	}
	return fo, hs
}
