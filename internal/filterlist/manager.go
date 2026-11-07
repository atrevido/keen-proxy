package filterlist

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

// State exposes one source's loaded counts, version, and latest refresh error.
type State struct {
	ID      string    `json:"id"`
	Rules   int       `json:"rules"`
	Skipped int       `json:"skipped"`
	Bytes   int       `json:"bytes"`
	Updated time.Time `json:"updated"`
	Error   string    `json:"error,omitempty"`
	Version string    `json:"version,omitempty"`
	Enabled bool      `json:"enabled"`
}

// snapshot persists parsed entries with their source URL and refresh metadata.
type snapshot struct {
	URL    string `json:"url"`
	Parsed Parsed `json:"parsed"`
	State  State  `json:"state"`
}

// Manager owns immutable loaded snapshots and serializes downloads separately from lookups.
type Manager struct {
	mu     sync.RWMutex
	update sync.Mutex
	dir    string
	client *http.Client
	cfg    model.Filters
	loaded map[string]snapshot
}

// New creates an empty manager; a nil client selects bounded HTTPS-only redirect handling.
func New(dir string, client *http.Client) *Manager {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{ForceAttemptHTTP2: true, MaxIdleConns: 2, IdleConnTimeout: 30 * time.Second}, CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if r.URL.Scheme != "https" || len(via) > 5 {
				return fmt.Errorf("invalid redirect")
			}
			return nil
		}}
	}
	return &Manager{dir: dir, client: client, loaded: map[string]snapshot{}}
}
func (m *Manager) path(s model.FilterSource) string {
	h := sha256.Sum256([]byte(s.ID + "\x00" + s.URL))
	return filepath.Join(m.dir, hex.EncodeToString(h[:16])+".json")
}
func cloneConfig(c model.Filters) model.Filters {
	c.Sources = slices.Clone(c.Sources)
	c.Exceptions = slices.Clone(c.Exceptions)
	return c
}

// Configure copies settings and loads matching local snapshots without network requests.
func (m *Manager) Configure(cfg model.Filters) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cloneConfig(cfg)
	allowed := []string{}
	for _, raw := range m.cfg.Exceptions {
		if d := domain(raw); d != "" {
			allowed = append(allowed, d)
		}
	}
	slices.Sort(allowed)
	m.cfg.Exceptions = allowed
	keep := map[string]snapshot{}
	total := 0
	for _, source := range cfg.Sources {
		if !source.Enabled {
			continue
		}
		p, ok := m.loaded[source.ID]
		if !ok || p.URL != source.URL || len(p.Parsed.Domains) == 0 {
			var err error
			p, err = m.read(source)
			if err != nil {
				keep[source.ID] = snapshot{URL: source.URL, State: State{ID: source.ID, Error: err.Error()}}
				continue
			}
		}
		n := len(p.Parsed.Domains) + len(p.Parsed.Allow)
		if total+n > MaxTotalRules {
			keep[source.ID] = snapshot{URL: source.URL, State: State{ID: source.ID, Error: fmt.Sprintf("total limit of %d rules", MaxTotalRules)}}
			continue
		}
		total += n
		keep[source.ID] = p
	}
	m.loaded = keep
}

// read validates persisted size, source identity, ordering, and normalized domains before use.
func (m *Manager) read(s model.FilterSource) (snapshot, error) {
	f, err := os.Open(m.path(s))
	if err != nil {
		if os.IsNotExist(err) {
			return snapshot{}, nil
		}
		return snapshot{}, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxBytes*2+1))
	if err != nil {
		return snapshot{}, err
	}
	if len(raw) > MaxBytes*2 {
		return snapshot{}, fmt.Errorf("snapshot too large")
	}
	var p snapshot
	if err := json.Unmarshal(raw, &p); err != nil {
		return snapshot{}, err
	}
	if p.URL != s.URL || len(p.Parsed.Domains) == 0 || len(p.Parsed.Domains)+len(p.Parsed.Allow) > MaxRules {
		return snapshot{}, fmt.Errorf("invalid snapshot")
	}
	for _, list := range [][]string{p.Parsed.Domains, p.Parsed.Allow} {
		if !slices.IsSorted(list) {
			return snapshot{}, fmt.Errorf("unsorted snapshot")
		}
		for _, v := range list {
			if domain(v) != v {
				return snapshot{}, fmt.Errorf("invalid snapshot domain")
			}
		}
	}
	p.State.ID = s.ID
	p.State.Rules = len(p.Parsed.Domains)
	return p, nil
}

// Match returns the first blocking source ID, honoring global and source-local exceptions.
func (m *Manager) Match(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if matches(m.cfg.Exceptions, name) {
		return ""
	}
	for _, s := range m.cfg.Sources {
		p := m.loaded[s.ID]
		if s.Enabled && p.URL == s.URL && matches(p.Parsed.Domains, name) && !matches(p.Parsed.Allow, name) {
			return s.ID
		}
	}
	return ""
}

// Status returns detached source states in configured order, including disabled sources.
func (m *Manager) Status() []State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []State{}
	for _, s := range m.cfg.Sources {
		p := m.loaded[s.ID]
		st := p.State
		st.ID = s.ID
		st.Enabled = s.Enabled
		out = append(out, st)
	}
	return out
}

// Refresh serializes downloads, keeps prior data on failure, and discards obsolete-source results.
// Automatic refreshes honor per-source opt-in and a one-day minimum interval.
func (m *Manager) Refresh(ctx context.Context, automatic bool) error {
	if !m.update.TryLock() {
		return fmt.Errorf("update already in progress")
	}
	defer m.update.Unlock()
	m.mu.RLock()
	cfg := cloneConfig(m.cfg)
	m.mu.RUnlock()
	var errs []error
	for _, s := range cfg.Sources {
		if !s.Enabled || automatic && !s.AutoUpdate {
			continue
		}
		m.mu.RLock()
		old := m.loaded[s.ID]
		m.mu.RUnlock()
		if automatic && time.Since(old.State.Updated) < 24*time.Hour {
			continue
		}
		p, err := m.fetch(ctx, s)
		m.mu.Lock()
		current := false
		for _, c := range m.cfg.Sources {
			if c.ID == s.ID && c.URL == s.URL && c.Enabled {
				current = true
			}
		}
		total := len(p.Parsed.Domains) + len(p.Parsed.Allow)
		for id, other := range m.loaded {
			if id != s.ID {
				total += len(other.Parsed.Domains) + len(other.Parsed.Allow)
			}
		}
		if err == nil && total > MaxTotalRules {
			err = fmt.Errorf("total limit of %d rules", MaxTotalRules)
		}
		if !current {
			m.mu.Unlock()
			continue
		}
		if err == nil {
			err = m.save(s, p)
		}
		if err != nil {
			old := m.loaded[s.ID]
			old.URL = s.URL
			old.State.ID = s.ID
			old.State.Error = err.Error()
			m.loaded[s.ID] = old
			errs = append(errs, fmt.Errorf("%s: %w", s.ID, err))
		} else {
			m.loaded[s.ID] = p
		}
		m.mu.Unlock()
	}
	return errors.Join(errs...)
}

// fetch parses a bounded HTTP response into a content-versioned snapshot without publishing it.
// Bytes estimates parsed storage rather than reporting the downloaded payload size.
func (m *Manager) fetch(ctx context.Context, s model.FilterSource) (snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.URL, nil)
	if err != nil {
		return snapshot{}, err
	}
	res, err := m.client.Do(req)
	if err != nil {
		return snapshot{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return snapshot{}, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	p, err := Parse(res.Body)
	if err != nil {
		return snapshot{}, err
	}
	raw, _ := json.Marshal(p)
	h := sha256.Sum256(raw)
	size := 0
	for _, list := range [][]string{p.Domains, p.Allow} {
		for _, v := range list {
			size += len(v) + 16
		}
	}
	return snapshot{URL: s.URL, Parsed: p, State: State{ID: s.ID, Rules: len(p.Domains), Skipped: p.Skipped, Bytes: size, Updated: time.Now(), Version: hex.EncodeToString(h[:8])}}, nil
}

// save syncs a temporary snapshot before atomically replacing its source-specific file.
func (m *Manager) save(s model.FilterSource, p snapshot) error {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.dir, ".filter-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return os.Rename(name, m.path(s))
}

// Domain normalizes a user exception consistently with loaded filter entries.
func Domain(s string) string { return domain(strings.TrimSpace(s)) }
