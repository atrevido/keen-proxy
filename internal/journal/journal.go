// Package journal stores route decisions, audit actions, and system messages.
// Recent entries remain in memory; optional JSONL storage rotates into one
// backup file, keeping at most two files per journal.
package journal

import (
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kind selects the route, audit, or system journal.
type Kind string

const (
	Routes Kind = "routes"
	Audit  Kind = "audit"
	System Kind = "system"
)

var Kinds = []Kind{Routes, Audit, System}

// Entry is one journal record; populated fields depend on the journal kind.
type Entry struct {
	DestinationPort uint16    `json:"destination_port,omitempty"`
	ID              uint64    `json:"id"`
	Time            time.Time `json:"time"`

	// Routing.
	Host     string `json:"host,omitempty"`     // Domain from SNI/Host, or an IP address.
	IP       string `json:"ip,omitempty"`       // Destination IP address.
	Client   string `json:"client,omitempty"`   // Device address including its port.
	Network  string `json:"network,omitempty"`  // tcp or udp.
	Outbound string `json:"outbound,omitempty"` // Connection ID or direct.
	Rule     string `json:"rule,omitempty"`     // Human-readable matched rule.
	RuleRaw  string `json:"rule_raw,omitempty"` // Original sing-box rule string.

	// Audit.
	Actor   string `json:"actor,omitempty"`  // user, system, watchdog
	Action  string `json:"action,omitempty"` // Short action description.
	Target  string `json:"target,omitempty"` // Affected object: config, firewall, geoip, etc.
	Result  string `json:"result,omitempty"` // ok, info, warn, error
	Details string `json:"details,omitempty"`

	// System.
	Level   string `json:"level,omitempty"`  // INFO, WARN, ERROR
	Source  string `json:"source,omitempty"` // keenproxy or sing-box.
	Message string `json:"message,omitempty"`
	Count   int    `json:"count,omitempty"` // Consecutive repetition count when greater than one.
}

// Fields lists the supported exact-match journal filter fields.
var Fields = map[Kind][]string{
	Routes: {"outbound", "client", "network"},
	Audit:  {"actor", "result", "target"},
	System: {"level", "source"},
}

// field resolves exact-match filter values, removing client ports and normalizing bare IPs.
func (e *Entry) field(name string) string {
	switch name {
	case "outbound":
		return e.Outbound
	case "client":
		if host, _, err := net.SplitHostPort(e.Client); err == nil {
			return host
		}
		value := strings.Trim(e.Client, "[]")
		if addr, err := netip.ParseAddr(value); err == nil {
			return addr.String()
		}
		return e.Client
	case "network":
		return e.Network
	case "actor":
		return e.Actor
	case "result":
		return e.Result
	case "target":
		return e.Target
	case "level":
		return e.Level
	case "source":
		return e.Source
	}
	return ""
}

func (e *Entry) text() string {
	return strings.ToLower(strings.Join([]string{
		e.Host, e.IP, e.Client, e.Outbound, e.Rule, e.RuleRaw,
		e.Actor, e.Action, e.Target, e.Result, e.Details, e.Level, e.Source, e.Message,
	}, "\x00"))
}

// Options controls in-memory retention, optional disk rotation, and message deduplication.
type Options struct {
	Memory   int    // Maximum recent entries retained in memory.
	File     string // JSONL path; empty keeps entries in memory only.
	MaxBytes int64  // File size threshold for rotation.
	// Dedup increments the last entry's Count for consecutive matching
	// messages within DedupWindow instead of adding another entry.
	Dedup bool
}

const dedupWindow = 10 * time.Minute

// Journal serializes record IDs, bounded history, disk writes, and nonblocking subscribers.
type Journal struct {
	opt Options

	mu     sync.Mutex
	ring   []Entry // Ascending ID order.
	next   uint64
	file   *os.File
	size   int64
	subs   map[chan Entry]struct{}
	failed bool // Suppress repeated file-write errors after a failure.
}

// New creates a journal and restores persisted history when a file is configured.
func New(opt Options) *Journal {
	if opt.Memory <= 0 {
		opt.Memory = 2000
	}
	if opt.MaxBytes <= 0 {
		opt.MaxBytes = 1 << 20
	}
	j := &Journal{opt: opt, next: 1, subs: map[chan Entry]struct{}{}}
	if opt.File != "" {
		j.load()
	}
	return j
}

// load restores recent entries and sequence numbering from disk after restart.
func (j *Journal) load() {
	for _, path := range []string{j.opt.File + ".1", j.opt.File} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		_ = visitEntries(f, func(e Entry) error {
			j.pushLocked(e)
			j.next = max(j.next, e.ID+1)
			return nil
		})
		f.Close()
	}
}

func (j *Journal) pushLocked(e Entry) {
	if len(j.ring) >= j.opt.Memory {
		// Compact rather than wrap so returned slices stay ordered. Copying a few
		// thousand entries once per Memory/4 additions is inexpensive.
		n := copy(j.ring, j.ring[len(j.ring)-j.opt.Memory*3/4:])
		j.ring = j.ring[:n]
	}
	j.ring = append(j.ring, e)
}

// Add appends an entry and returns its assigned ID and timestamp.
func (j *Journal) Add(e Entry) Entry {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.opt.Dedup && len(j.ring) > 0 {
		last := &j.ring[len(j.ring)-1]
		if last.Level == e.Level && last.Source == e.Source && last.Message == e.Message && e.Time.Sub(last.Time) < dedupWindow {
			last.Count = max(last.Count, 1) + 1
			last.Time = e.Time
			j.publishLocked(*last)
			return *last
		}
	}
	e.ID = j.next
	j.next++
	j.pushLocked(e)
	j.writeLocked(e)
	j.publishLocked(e)
	return e
}

// writeLocked appends JSONL and rotates at the size limit; the caller holds j.mu.
// A write failure disables further persistence attempts for this journal.
func (j *Journal) writeLocked(e Entry) {
	if j.opt.File == "" || j.failed {
		return
	}
	if j.file == nil {
		if err := os.MkdirAll(filepath.Dir(j.opt.File), 0o755); err != nil {
			j.failed = true
			return
		}
		f, err := os.OpenFile(j.opt.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			j.failed = true
			return
		}
		st, _ := f.Stat()
		j.file, j.size = f, st.Size()
	}
	raw, _ := json.Marshal(e)
	raw = append(raw, '\n')
	n, err := j.file.Write(raw)
	if err != nil {
		j.failed = true
		return
	}
	j.size += int64(n)
	if j.size >= j.opt.MaxBytes {
		j.file.Close()
		j.file = nil
		os.Rename(j.opt.File, j.opt.File+".1")
	}
}

func (j *Journal) publishLocked(e Entry) {
	for ch := range j.subs {
		select {
		case ch <- e:
		default: // Slow subscribers may miss entries but never block the journal.
		}
	}
}

// Subscribe streams new entries and Count updates; callers must invoke cancel.
func (j *Journal) Subscribe() (<-chan Entry, func()) {
	ch := make(chan Entry, 256)
	j.mu.Lock()
	j.subs[ch] = struct{}{}
	j.mu.Unlock()
	return ch, func() {
		j.mu.Lock()
		delete(j.subs, ch)
		j.mu.Unlock()
	}
}

// Query combines free text, exact field filters, and older-page pagination.
type Query struct {
	Text    string            // Case-insensitive substring across all fields.
	Filters map[string]string // Field name to exact value.
	Before  uint64            // Only smaller IDs, for older-page pagination.
	Limit   int
}

func (q Query) match(e *Entry) bool {
	for k, v := range q.Filters {
		if v != "" && e.field(k) != v {
			return false
		}
	}
	return q.Text == "" || strings.Contains(e.text(), strings.ToLower(q.Text))
}

// Page contains newest-first matches, pagination state, and all-history filter facets.
type Page struct {
	Entries []Entry `json:"entries"`
	HasMore bool    `json:"has_more"`
	// Facets contains column-filter values across the whole in-memory journal.
	Facets map[string][]string `json:"facets"`
}

// Find returns matching entries from newest to oldest.
func (j *Journal) Find(q Query, fields []string) Page {
	if q.Limit <= 0 || q.Limit > 1000 {
		q.Limit = 50
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	p := Page{Entries: []Entry{}, Facets: map[string][]string{}}
	seen := map[string]map[string]bool{}
	for _, f := range fields {
		seen[f] = map[string]bool{}
	}
	for i := len(j.ring) - 1; i >= 0; i-- {
		e := &j.ring[i]
		for _, f := range fields {
			if v := e.field(f); v != "" && !seen[f][v] {
				seen[f][v] = true
				p.Facets[f] = append(p.Facets[f], v)
			}
		}
		if q.Before != 0 && e.ID >= q.Before || !q.match(e) {
			continue
		}
		if len(p.Entries) == q.Limit {
			p.HasMore = true
			continue
		}
		p.Entries = append(p.Entries, *e)
	}
	for _, v := range p.Facets {
		slices.Sort(v)
	}
	return p
}

// Export writes text entries since the requested time, reading disk when
// enabled and applying mask to redact secrets.
func (j *Journal) Export(w io.Writer, kind Kind, since time.Time, mask func(string) string) error {
	return j.Visit(func(e Entry) error {
		if e.Time.Before(since) {
			return nil
		}
		line := Format(kind, e)
		if mask != nil {
			line = mask(line)
		}
		_, err := io.WriteString(w, line+"\n")
		return err
	})
}

// Format renders one journal entry as a single export line.
func Format(kind Kind, e Entry) string {
	ts := e.Time.Format("2006-01-02 15:04:05")
	switch kind {
	case Routes:
		host := e.Host
		if e.IP != "" && e.IP != e.Host {
			host += " (" + e.IP + ")"
		}
		return ts + "  " + e.Client + " → " + host + "  " + e.Network + "  " + e.Outbound + "  " + e.Rule
	case Audit:
		return ts + "  " + e.Actor + "  " + e.Result + "  " + e.Action + " [" + e.Target + "]  " + e.Details
	default:
		s := ts + "  " + e.Level + "  " + e.Source + "  " + e.Message
		if e.Count > 1 {
			s += " (×" + strconv.Itoa(e.Count) + ")"
		}
		return s
	}
}

// Close releases the optional append file under the journal lock.
func (j *Journal) Close() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file != nil {
		j.file.Close()
		j.file = nil
	}
}

// Set contains the daemon's route, audit, and system journals.
type Set map[Kind]*Journal

// NewSet creates journals in dir, or memory-only journals when dir is empty.
func NewSet(dir string) Set {
	file := func(name string) string {
		if dir == "" {
			return ""
		}
		return filepath.Join(dir, name+".jsonl")
	}
	return Set{
		Routes: New(Options{Memory: 5000, File: file("routes"), MaxBytes: 4 << 20}),
		Audit:  New(Options{Memory: 2000, File: file("audit"), MaxBytes: 1 << 20}),
		System: New(Options{Memory: 3000, File: file("system"), MaxBytes: 2 << 20, Dedup: true}),
	}
}
