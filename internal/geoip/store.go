package geoip

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	userAgent = "keenProxy-geoip"
	// CheckInterval is the time between successful freshness checks of one database.
	CheckInterval = 24 * time.Hour
	// maxDownload bounds the HTTP body.
	maxDownload = 200 << 20
)

// maxDatabase bounds the decompressed bytes of one download; tests lower it.
var maxDatabase int64 = 512 << 20

// ErrBusy reports that another update holds the database files.
var ErrBusy = errors.New("IP database update already in progress")

var errTooLarge = errors.New("unpacked database exceeds the size limit")

// State is persisted beside each managed database as <id>.json.
type State struct {
	Source       string    `json:"source"` // Source.Key of the installed file
	URL          string    `json:"url,omitempty"`
	Version      string    `json:"version,omitempty"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	Checked      time.Time `json:"checked,omitzero"` // last attempt, successful or not
	Updated      time.Time `json:"updated,omitzero"` // last installation of a new file
	Error        string    `json:"error,omitempty"`
	Failures     int       `json:"failures,omitempty"` // consecutive failed attempts
}

// Next returns when automatic updates should check the database again,
// given the source's check interval.
func (st State) Next(every time.Duration) time.Time {
	if st.Checked.IsZero() {
		return time.Time{}
	}
	if st.Failures > 0 {
		return st.Checked.Add(min(Backoff(st.Failures), every))
	}
	return st.Checked.Add(every)
}

// Backoff doubles the retry delay after each failure: 10 min, 20 min, … up to 6 h.
func Backoff(failures int) time.Duration {
	d := 10 * time.Minute
	for i := 1; i < failures && d < 6*time.Hour; i++ {
		d *= 2
	}
	return min(d, 6*time.Hour)
}

// NewClient returns an HTTP client that follows at most five HTTPS-only redirects.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if r.URL.Scheme != "https" || len(via) > 5 {
			return fmt.Errorf("redirect to %s is not allowed", r.URL.Redacted())
		}
		return nil
	}}
}

// Store keeps downloaded databases and their state in Dir. Callers serialize
// Update calls for one Store; see TryLock.
type Store struct {
	Dir    string
	Client *http.Client
	Now    func() time.Time // nil selects time.Now
	busy   chan struct{}
}

// NewStore creates a store with a bounded HTTPS-only client.
func NewStore(dir string) *Store {
	return &Store{Dir: dir, Client: NewClient(5 * time.Minute), busy: make(chan struct{}, 1)}
}

// TryLock reserves the store for one updater; Unlock releases it.
func (s *Store) TryLock() bool {
	select {
	case s.busy <- struct{}{}:
		return true
	default:
		return false
	}
}

// Unlock releases a reservation taken by TryLock.
func (s *Store) Unlock() { <-s.busy }

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Path returns the managed MMDB location for a database ID.
func (s *Store) Path(id string) string { return filepath.Join(s.Dir, id+".mmdb") }

func (s *Store) statePath(id string) string { return filepath.Join(s.Dir, id+".json") }

// State loads saved metadata; a missing or unreadable file yields the zero State.
func (s *Store) State(id string) State {
	var st State
	if raw, err := os.ReadFile(s.statePath(id)); err == nil {
		json.Unmarshal(raw, &st)
	}
	return st
}

func (s *Store) saveState(id string, st State) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	// State holds custom source URLs, which may embed access tokens.
	tmp := s.statePath(id) + ".tmp"
	os.Remove(tmp)
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.statePath(id))
}

// Due reports whether automatic updates should check the database now: it
// is missing, belongs to another source, or its check interval elapsed.
func (s *Store) Due(id string, src Source) bool {
	st := s.State(id)
	switch {
	case st.Source != src.Key():
		return true
	case st.Failures > 0:
		// Respect the retry backoff even while no usable file exists.
		return !s.now().Before(st.Next(Interval(src)))
	case !valid(s.Path(id)):
		return true
	}
	return !s.now().Before(st.Next(Interval(src)))
}

// Update checks a source and installs a newer database when one is available.
// It reports whether the installed file changed. The previous file stays in
// place when anything fails, and the failure is recorded for backoff.
func (s *Store) Update(ctx context.Context, id string, src Source) (bool, error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return false, err
	}
	st := s.State(id)
	if st.Source != src.Key() {
		st = State{Source: src.Key()}
	}
	have := valid(s.Path(id))
	changed, err := s.update(ctx, id, src, &st, have)
	st.Checked = s.now()
	if err != nil {
		st.Error = err.Error()
		st.Failures++
	} else {
		st.Error, st.Failures = "", 0
	}
	if serr := s.saveState(id, st); err == nil && serr != nil {
		err = serr
	}
	return changed, err
}

func (s *Store) update(ctx context.Context, id string, src Source, st *State, have bool) (bool, error) {
	arts, err := src.Resolve(ctx, s.Client, s.now())
	if err != nil {
		return false, err
	}
	var errs []error
	for _, a := range arts {
		if have && a.Version != "" && a.Version == st.Version {
			return false, nil
		}
		changed, err := s.fetch(ctx, id, a, st, have)
		if err == nil {
			return changed, nil
		}
		errs = append(errs, err)
	}
	return false, errors.Join(errs...)
}

// fetch downloads one artifact, verifies it, and atomically replaces the database.
func (s *Store) fetch(ctx context.Context, id string, a Artifact, st *State, have bool) (bool, error) {
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return false, fmt.Errorf("an HTTPS URL is required, not %q", a.URL)
	}
	shown := u.Redacted()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", userAgent)
	if have && a.URL == st.URL {
		if st.ETag != "" {
			req.Header.Set("If-None-Match", st.ETag)
		}
		if st.LastModified != "" {
			req.Header.Set("If-Modified-Since", st.LastModified)
		}
	}
	res, err := s.Client.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified && have {
		return false, nil
	}
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%s: HTTP %d", shown, res.StatusCode)
	}
	tmp := s.Path(id) + ".tmp"
	defer os.Remove(tmp)
	if err := s.receive(res.Body, a.SHA256, tmp); err != nil {
		return false, fmt.Errorf("%s: %w", shown, err)
	}
	if err := os.Rename(tmp, s.Path(id)); err != nil {
		return false, err
	}
	st.URL, st.Version = a.URL, a.Version
	st.ETag, st.LastModified = res.Header.Get("ETag"), res.Header.Get("Last-Modified")
	st.Updated = s.now()
	return true, nil
}

// receive stores the body as a validated MMDB at path, verifying the digest of
// the downloaded bytes and unpacking gzip or a tar.gz archive when present.
func (s *Store) receive(body io.Reader, digest, path string) error {
	raw := path + ".download"
	defer os.Remove(raw)
	f, err := os.Create(raw)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(body, maxDownload+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n > maxDownload {
		return fmt.Errorf("file exceeds %d MiB", maxDownload>>20)
	}
	if got := hex.EncodeToString(h.Sum(nil)); digest != "" && got != digest {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, digest)
	}
	if err := unpack(raw, path); err != nil {
		return err
	}
	db, err := Open(path)
	if err != nil {
		return fmt.Errorf("file is not MMDB: %w", err)
	}
	defer db.Close()
	if !db.hasCountries() {
		return errors.New("database has no country.iso_code field")
	}
	return nil
}

// unpack copies src to dst, decompressing gzip and extracting the first .mmdb
// file from a tar archive. Plain files are copied unchanged.
func unpack(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	var r io.Reader = bufio.NewReader(in)
	if head, _ := r.(*bufio.Reader).Peek(2); bytes.Equal(head, []byte{0x1f, 0x8b}) {
		gz, err := gzip.NewReader(r)
		if err != nil {
			return err
		}
		defer gz.Close()
		// Bound all decompressed bytes, including tar entries skipped below.
		r = bufio.NewReader(&capReader{r: gz, n: maxDatabase})
	}
	if head, _ := r.(*bufio.Reader).Peek(262); len(head) == 262 && string(head[257:262]) == "ustar" {
		tr := tar.NewReader(r)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				return errors.New("archive contains no .mmdb file")
			}
			if err != nil {
				return err
			}
			if h.Typeflag == tar.TypeReg && strings.HasSuffix(h.Name, ".mmdb") {
				r = tr
				break
			}
		}
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(r, maxDatabase+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxDatabase {
		err = errTooLarge
	}
	return err
}

// capReader fails with errTooLarge once more than n bytes are requested.
type capReader struct {
	r io.Reader
	n int64
}

func (c *capReader) Read(p []byte) (int, error) {
	if c.n <= 0 {
		return 0, errTooLarge
	}
	p = p[:min(int64(len(p)), c.n)]
	n, err := c.r.Read(p)
	c.n -= int64(n)
	return n, err
}

// valid reports whether path holds a readable MMDB.
func valid(path string) bool {
	db, err := Open(path)
	if err != nil {
		return false
	}
	db.Close()
	return true
}

// Prune removes managed files of databases that are no longer configured.
// Only IDs with a state file written by this store are touched.
func (s *Store) Prune(keep map[string]bool) {
	entries, _ := os.ReadDir(s.Dir)
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || keep[id] {
			continue
		}
		os.Remove(s.Path(id))
		os.Remove(s.statePath(id))
	}
}
