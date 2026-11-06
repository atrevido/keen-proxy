// Package domaincache stores bounded, expiring DNS observations. It never
// decides which groups participate in routing; that remains the compiler's job.
package domaincache

import (
	"github.com/miekg/dns"
	"sort"
	"strings"
	"sync"
	"time"
)

// Address stores one IPv4 answer and its effective chain TTL expiration.
type Address struct {
	IP        string    `json:"ip"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Record keeps successful answers, latest attempt status, and device-observation time.
type Record struct {
	ObservedAt  time.Time `json:"observed_at"`
	Name        string    `json:"name"`
	Addresses   []Address `json:"addresses"`
	CheckedAt   time.Time `json:"checked_at"`
	AttemptedAt time.Time `json:"attempted_at"`
	Error       string    `json:"error,omitempty"`
	Source      string    `json:"source"`
	Limited     bool      `json:"limited,omitempty"`
}

// Cache synchronizes a bounded set of DNS observations using an injected clock.
type Cache struct {
	mu      sync.Mutex
	now     func() time.Time
	records map[string]Record
	limited bool
}

// New creates an empty cache whose expiration decisions use now.
func New(now func() time.Time) *Cache { return &Cache{now: now, records: map[string]Record{}} }

// Normalize trims whitespace and one trailing dot, then lowercases the DNS name.
func Normalize(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}

// put replaces a record and evicts the oldest attempt at the name limit; the caller holds mu.
func (c *Cache) put(r Record) {
	if _, ok := c.records[r.Name]; !ok && len(c.records) >= 4096 {
		var oldest string
		var when time.Time
		for name, v := range c.records {
			if oldest == "" || v.AttemptedAt.Before(when) {
				oldest = name
				when = v.AttemptedAt
			}
		}
		delete(c.records, oldest)
		c.limited = true
	}
	c.records[r.Name] = r
}

// Observe retains only the question's A/CNAME chain, bounded by 32 IPs and the shortest TTL.
// Only device observations advance ObservedAt; service refreshes preserve it.
func (c *Cache) Observe(name string, m *dns.Msg, source string) {
	if m == nil || m.Rcode != dns.RcodeSuccess {
		return
	}
	name = Normalize(name)
	if name == "" {
		return
	}
	now := c.now()
	r := Record{Name: name, Source: source, CheckedAt: now, AttemptedAt: now, Addresses: []Address{}}
	// Follow only the answer chain belonging to the question, regardless of RR order.
	current := name
	ttl := uint32(^uint32(0))
	seen := map[string]bool{}
	terminal := false
	for steps := 0; steps < 16 && !seen[current]; steps++ {
		seen[current] = true
		next := ""
		for _, rr := range m.Answer {
			if cn, ok := rr.(*dns.CNAME); ok && Normalize(cn.Hdr.Name) == current {
				next = Normalize(cn.Target)
				ttl = min(ttl, cn.Hdr.Ttl)
				break
			}
		}
		if next == "" {
			terminal = true
			break
		}
		current = next
	}
	if !terminal {
		c.SetError(name, "CNAME chain is cyclic or too long")
		return
	}
	unique := map[string]bool{}
	for _, rr := range m.Answer {
		a, ok := rr.(*dns.A)
		if !ok || Normalize(a.Hdr.Name) != current {
			continue
		}
		life := min(ttl, a.Hdr.Ttl)
		ip := a.A.To4()
		if life == 0 || ip == nil || unique[ip.String()] {
			continue
		}
		unique[ip.String()] = true
		if len(r.Addresses) >= 32 {
			r.Limited = true
			continue
		}
		r.Addresses = append(r.Addresses, Address{IP: ip.String(), ExpiresAt: now.Add(time.Duration(life) * time.Second)})
	}
	sort.Slice(r.Addresses, func(i, j int) bool { return r.Addresses[i].IP < r.Addresses[j].IP })
	c.mu.Lock()
	defer c.mu.Unlock()
	if source == "observed" {
		r.ObservedAt = now
	} else {
		r.ObservedAt = c.records[name].ObservedAt
	}
	c.put(r)
}

// SetError records a failed attempt while retaining still-valid answers.
func (c *Cache) SetError(name, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	name = Normalize(name)
	r := c.records[name]
	r.Name = name
	r.Error = message
	r.AttemptedAt = c.now()
	if r.Addresses == nil {
		r.Addresses = []Address{}
	}
	c.put(r)
}
func (c *Cache) pruneLocked() {
	now := c.now()
	for name, r := range c.records {
		live := make([]Address, 0, len(r.Addresses))
		for _, a := range r.Addresses {
			if a.ExpiresAt.After(now) {
				live = append(live, a)
			}
		}
		r.Addresses = live
		c.records[name] = r
	}
}

// Prune removes expired addresses while retaining name and attempt history.
func (c *Cache) Prune() { c.mu.Lock(); defer c.mu.Unlock(); c.pruneLocked() }

// Limited reports whether name-capacity eviction has occurred.
func (c *Cache) Limited() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.limited }

// Snapshot returns detached, sorted records with expired addresses removed; no names means all.
func (c *Cache) Snapshot(names []string) []Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked()
	out := []Record{}
	wanted := map[string]bool{}
	for _, n := range names {
		wanted[Normalize(n)] = true
	}
	for name, r := range c.records {
		if len(names) > 0 && !wanted[name] {
			continue
		}
		r.Addresses = append([]Address{}, r.Addresses...)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
