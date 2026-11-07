package geoip_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/ordo-systems/keen-proxy/internal/geoip"
	"github.com/ordo-systems/keen-proxy/internal/geoip/geoiptest"
)

var testNets = map[string]string{
	"77.88.0.0/18":     "RU",
	"5.255.255.0/24":   "RU",
	"8.8.8.0/24":       "US",
	"2a02:6b8::/32":    "RU",
	"185.199.108.0/22": "NL",
}

func writeDB(t *testing.T, nets map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.mmdb")
	if err := geoiptest.WriteTestDB(p, nets); err != nil {
		t.Fatal(err)
	}
	return p
}

func testDB(t *testing.T) string { return writeDB(t, testNets) }

func openDB(t *testing.T, path string) *DB {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func gzipped(raw []byte) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write(raw)
	w.Close()
	return b.Bytes()
}

func prefixes(p []netip.Prefix) string {
	var s []string
	for _, x := range p {
		s = append(s, x.String())
	}
	return strings.Join(s, ",")
}

func TestLookupAndPrefixes(t *testing.T) {
	db := openDB(t, testDB(t))
	defer db.Close()
	for ip, want := range map[string]string{"77.88.8.8": "RU", "8.8.8.8": "US", "1.1.1.1": "", "::ffff:5.255.255.242": "RU", "2a02:6b8::1": "RU"} {
		if got := db.Country(netip.MustParseAddr(ip)); got != want {
			t.Errorf("Country(%s) = %q, want %q", ip, got, want)
		}
	}
	p, err := db.Prefixes([]string{"ru", "NL"})
	if err != nil {
		t.Fatal(err)
	}
	if prefixes(p["RU"]) != "5.255.255.0/24,77.88.0.0/18,2a02:6b8::/32" || len(p["NL"]) != 1 || p["US"] != nil {
		t.Errorf("Prefixes: RU=%v NL=%v US=%v", p["RU"], p["NL"], p["US"])
	}
}

// The first database that knows an address decides its country, both for
// lookups and for the generated country prefixes.
func TestSetPriority(t *testing.T) {
	primary := openDB(t, writeDB(t, map[string]string{"10.0.0.0/24": "RU", "10.0.2.0/24": "DE"}))
	secondary := openDB(t, writeDB(t, map[string]string{"10.0.0.0/22": "FI", "10.0.4.0/24": "RU"}))
	set := NewSet(Member{ID: "a", DB: primary}, Member{ID: "b", DB: secondary})
	defer set.Close()
	for ip, want := range map[string]string{"10.0.0.1": "RU/a", "10.0.1.1": "FI/b", "10.0.2.1": "DE/a", "10.0.4.1": "RU/b", "10.0.9.1": "/"} {
		cc, id := set.Country(netip.MustParseAddr(ip))
		if cc+"/"+id != want {
			t.Errorf("Country(%s) = %s/%s, want %s", ip, cc, id, want)
		}
	}
	p, err := set.Prefixes([]string{"RU", "FI", "DE"})
	if err != nil {
		t.Fatal(err)
	}
	for cc, want := range map[string]string{"RU": "10.0.0.0/24,10.0.4.0/24", "FI": "10.0.1.0/24,10.0.3.0/24", "DE": "10.0.2.0/24"} {
		if got := prefixes(p[cc]); got != want {
			t.Errorf("Prefixes[%s] = %s, want %s", cc, got, want)
		}
	}
}

// Category labels are unioned across databases rather than prioritized.
func TestSetUnionAndLabels(t *testing.T) {
	a := openDB(t, writeDB(t, map[string]string{"10.0.0.0/24": "RU-BLOCKED", "10.0.2.0/24": "GOOGLE"}))
	b := openDB(t, writeDB(t, map[string]string{"10.0.0.0/23": "RU-BLOCKED", "10.0.2.0/24": "RE-FILTER"}))
	set := NewSet(Member{ID: "a", DB: a}, Member{ID: "b", DB: b})
	defer set.Close()
	p, err := set.Union([]string{"RU-BLOCKED", "RE-FILTER"})
	if err != nil || prefixes(p["RU-BLOCKED"]) != "10.0.0.0/23" || prefixes(p["RE-FILTER"]) != "10.0.2.0/24" {
		t.Fatalf("Union: %v %v", p, err)
	}
	if got := strings.Join(set.Labels(netip.MustParseAddr("10.0.2.1")), ","); got != "GOOGLE,RE-FILTER" {
		t.Errorf("Labels = %s", got)
	}
}

func TestSourceIntervals(t *testing.T) {
	if Interval(URLSource{URL: "https://x"}) != CheckInterval || Interval(GitHubRelease{Every: 6 * time.Hour}) != 6*time.Hour {
		t.Fatal("Interval")
	}
	now := time.Now()
	st := State{Checked: now, Failures: 9}
	if st.Next(6*time.Hour) != now.Add(6*time.Hour) {
		t.Error("failure backoff must not exceed the source interval")
	}
	b, _ := FindBuiltin("runetfreedom-ru")
	if b.Kind != KindCategory || len(b.Categories) == 0 || Interval(b.Source) != 6*time.Hour {
		t.Errorf("runetfreedom-ru: %+v", b)
	}
}

// server serves fixed paths over TLS and counts requests per path.
type server struct {
	*httptest.Server
	files map[string][]byte
	hits  map[string]int
	etag  string
}

func newServer(t *testing.T) *server {
	s := &server{files: map[string][]byte{}, hits: map[string]int{}}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits[r.URL.Path]++
		body, ok := s.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if s.etag != "" {
			if r.Header.Get("If-None-Match") == s.etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", s.etag)
		}
		w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

func newStore(t *testing.T, s *server, now time.Time) *Store {
	st := NewStore(filepath.Join(t.TempDir(), "geoip"))
	st.Client = s.Client()
	st.Now = func() time.Time { return now }
	return st
}

func TestMonthlyURLFallsBackAndSkipsKnownMonth(t *testing.T) {
	raw, _ := os.ReadFile(testDB(t))
	srv := newServer(t)
	// On March 31 the March file is missing; February must be computed
	// correctly even though February has no 31st day.
	srv.files["/db-2026-02.mmdb.gz"] = gzipped(raw)
	store := newStore(t, srv, time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC))
	src := URLSource{URL: srv.URL + "/db-{month}.mmdb.gz"}
	changed, err := store.Update(context.Background(), "dbip", src)
	if err != nil || !changed {
		t.Fatalf("Update: changed=%v err=%v", changed, err)
	}
	if st := store.State("dbip"); st.Version != "month:2026-02" || st.Failures != 0 {
		t.Errorf("state %+v", st)
	}
	changed, err = store.Update(context.Background(), "dbip", src)
	if err != nil || changed || srv.hits["/db-2026-02.mmdb.gz"] != 1 || srv.hits["/db-2026-03.mmdb.gz"] != 2 {
		t.Errorf("repeat check: changed=%v err=%v hits=%v", changed, err, srv.hits)
	}
	db := openDB(t, store.Path("dbip"))
	defer db.Close()
	if db.Country(netip.MustParseAddr("8.8.8.8")) != "US" {
		t.Error("database not installed")
	}
}

func releaseJSON(tag, asset, url string, digest string) []byte {
	return fmt.Appendf(nil, `{"tag_name":%q,"assets":[{"name":"other.mmdb","browser_download_url":"https://example.invalid/x"},{"name":%q,"browser_download_url":%q,"digest":%q,"updated_at":"2026-11-05T00:20:17Z"}]}`, tag, asset, url, digest)
}

func TestGitHubReleaseVerifiesDigest(t *testing.T) {
	raw, _ := os.ReadFile(testDB(t))
	sum := sha256.Sum256(raw)
	srv := newServer(t)
	srv.files["/dl/Country.mmdb"] = raw
	srv.files["/repos/o/r/releases/latest"] = releaseJSON("v1", "Country.mmdb", srv.URL+"/dl/Country.mmdb", "sha256:"+hex.EncodeToString(sum[:]))
	store := newStore(t, srv, time.Now())
	src := GitHubRelease{Repo: "o/r", Asset: "Country.mmdb", API: srv.URL}
	if changed, err := store.Update(context.Background(), "gh", src); err != nil || !changed {
		t.Fatalf("Update: changed=%v err=%v", changed, err)
	}
	// The same release digest is not downloaded again.
	if changed, err := store.Update(context.Background(), "gh", src); err != nil || changed || srv.hits["/dl/Country.mmdb"] != 1 {
		t.Fatalf("repeat: changed=%v err=%v hits=%v", changed, err, srv.hits)
	}
	// A tampered asset is rejected and the installed database stays.
	srv.files["/repos/o/r/releases/latest"] = releaseJSON("v2", "Country.mmdb", srv.URL+"/dl/Country.mmdb", "sha256:"+strings.Repeat("0", 64))
	if _, err := store.Update(context.Background(), "gh", src); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want checksum error, got %v", err)
	}
	if st := store.State("gh"); st.Version != "v1/sha256:"+hex.EncodeToString(sum[:]) || st.Failures != 1 || st.Error == "" {
		t.Errorf("state %+v", st)
	}
	db := openDB(t, store.Path("gh"))
	db.Close()
	// A release without the asset is an error, not an empty database.
	srv.files["/repos/o/r/releases/latest"] = []byte(`{"tag_name":"v3","assets":[]}`)
	if _, err := store.Update(context.Background(), "gh", src); err == nil {
		t.Error("want error: release has no asset")
	}
}

func TestStaticURLUsesConditionalRequests(t *testing.T) {
	raw, _ := os.ReadFile(testDB(t))
	srv := newServer(t)
	srv.etag = `"v1"`
	srv.files["/country.mmdb"] = raw
	store := newStore(t, srv, time.Now())
	src := URLSource{URL: srv.URL + "/country.mmdb"}
	if changed, err := store.Update(context.Background(), "c", src); err != nil || !changed {
		t.Fatalf("Update: changed=%v err=%v", changed, err)
	}
	if changed, err := store.Update(context.Background(), "c", src); err != nil || changed {
		t.Fatalf("304: changed=%v err=%v", changed, err)
	}
	if st := store.State("c"); st.ETag != `"v1"` {
		t.Errorf("state %+v", st)
	}
}

func TestUnpacksTarGz(t *testing.T) {
	raw, _ := os.ReadFile(testDB(t))
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	tw.WriteHeader(&tar.Header{Name: "GeoLite2-Country_20261105/README.txt", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	tw.Write([]byte("hi"))
	tw.WriteHeader(&tar.Header{Name: "GeoLite2-Country_20261105/GeoLite2-Country.mmdb", Mode: 0o644, Size: int64(len(raw)), Typeflag: tar.TypeReg})
	tw.Write(raw)
	tw.Close()
	srv := newServer(t)
	srv.files["/db.tar.gz"] = gzipped(b.Bytes())
	store := newStore(t, srv, time.Now())
	if _, err := store.Update(context.Background(), "tar", URLSource{URL: srv.URL + "/db.tar.gz"}); err != nil {
		t.Fatal(err)
	}
	db := openDB(t, store.Path("tar"))
	db.Close()
}

func TestRejectsGarbageAndPlainHTTP(t *testing.T) {
	srv := newServer(t)
	srv.files["/x.mmdb"] = []byte("<html>error</html>")
	srv.files["/asn.mmdb"], _ = os.ReadFile(writeDB(t, map[string]string{}))
	store := newStore(t, srv, time.Now())
	for _, u := range []string{srv.URL + "/x.mmdb", srv.URL + "/asn.mmdb", "http://example.com/country.mmdb"} {
		if _, err := store.Update(context.Background(), "bad", URLSource{URL: u}); err == nil {
			t.Errorf("%s: want error", u)
		}
	}
	if _, err := os.Stat(store.Path("bad")); err == nil {
		t.Error("database file must not be created")
	}
	// Each URL is a new source, so the failure count restarts.
	if st := store.State("bad"); st.Failures != 1 || !strings.Contains(st.Error, "HTTPS") {
		t.Errorf("state %+v", st)
	}
}

func TestDueAndBackoff(t *testing.T) {
	if Backoff(1) != 10*time.Minute || Backoff(2) != 20*time.Minute || Backoff(20) != 6*time.Hour {
		t.Errorf("Backoff: %v %v %v", Backoff(1), Backoff(2), Backoff(20))
	}
	srv := newServer(t)
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	store := newStore(t, srv, now)
	src := URLSource{URL: srv.URL + "/missing.mmdb"}
	if !store.Due("m", src) {
		t.Error("a new database is checked immediately")
	}
	store.Update(context.Background(), "m", src)
	if store.Due("m", src) {
		t.Error("after a failure, wait for backoff")
	}
	store.Now = func() time.Time { return now.Add(11 * time.Minute) }
	if !store.Due("m", src) {
		t.Error("retry after backoff")
	}
	if !store.Due("m", URLSource{URL: srv.URL + "/other.mmdb"}) {
		t.Error("a changed source is checked immediately")
	}
}

func TestPruneRemovesOnlyManagedFiles(t *testing.T) {
	raw, _ := os.ReadFile(testDB(t))
	srv := newServer(t)
	srv.files["/a.mmdb"] = raw
	store := newStore(t, srv, time.Now())
	for _, id := range []string{"keep", "gone"} {
		if _, err := store.Update(context.Background(), id, URLSource{URL: srv.URL + "/a.mmdb"}); err != nil {
			t.Fatal(err)
		}
	}
	foreign := filepath.Join(store.Dir, "user.mmdb")
	os.WriteFile(foreign, raw, 0o644)
	store.Prune(map[string]bool{"keep": true})
	for path, want := range map[string]bool{store.Path("keep"): true, store.Path("gone"): false, foreign: true} {
		if _, err := os.Stat(path); (err == nil) != want {
			t.Errorf("%s: exists=%v, want %v", path, err == nil, want)
		}
	}
}

func TestBuiltinsAreHTTPS(t *testing.T) {
	seen := map[string]bool{}
	for _, b := range Builtins {
		if b.ID == "" || seen[b.ID] || b.Source == nil || b.License == "" {
			t.Errorf("builtin %+v", b)
		}
		seen[b.ID] = true
		if u, ok := b.Source.(URLSource); ok && !strings.HasPrefix(u.URL, "https://") {
			t.Errorf("%s: %s", b.ID, u.URL)
		}
	}
}

func TestFlag(t *testing.T) {
	if Flag("ru") != "🇷🇺" || Flag("") != "" {
		t.Errorf("Flag: %q", Flag("ru"))
	}
}
