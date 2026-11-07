// Package geoip maps IP addresses to countries for routing rules and connection flags.
//
// The default database is DB-IP IP to Country Lite, updated monthly under
// CC BY 4.0 with "IP Geolocation by DB-IP" attribution in the UI. Other MMDBs
// with country.iso_code, such as MaxMind GeoLite2 Country, are supported.
package geoip

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

// DefaultURL is the DB-IP URL template; {month} expands to YYYY-MM.
const DefaultURL = "https://download.db-ip.com/free/dbip-country-lite-{month}.mmdb.gz"

// MaxAge determines when an existing database is eligible for refresh.
const MaxAge = 35 * 24 * time.Hour

// record decodes only the country ISO field needed from an MMDB result.
type record struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
}

// DB wraps an open country MMDB and exposes its build metadata.
type DB struct {
	r     *maxminddb.Reader
	Built time.Time
	Type  string
}

// Open maps a country database; callers must close it when no longer needed.
func Open(path string) (*DB, error) {
	r, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	return &DB{r: r, Built: time.Unix(int64(r.Metadata.BuildEpoch), 0), Type: r.Metadata.DatabaseType}, nil
}

// Close releases the underlying MMDB reader.
func (db *DB) Close() error { return db.r.Close() }

// Country returns the uppercase ISO country code, or an empty string.
func (db *DB) Country(ip netip.Addr) string {
	var rec record
	if err := db.r.Lookup(ip.Unmap()).Decode(&rec); err != nil {
		return ""
	}
	return strings.ToUpper(rec.Country.ISOCode)
}

// Prefixes collects the requested countries' subnets in one database traversal.
func (db *DB) Prefixes(codes []string) (map[string][]netip.Prefix, error) {
	want := map[string]bool{}
	for _, c := range codes {
		want[strings.ToUpper(c)] = true
	}
	out := map[string][]netip.Prefix{}
	for res := range db.r.Networks() {
		var rec record
		if err := res.Decode(&rec); err != nil {
			return nil, err
		}
		cc := strings.ToUpper(rec.Country.ISOCode)
		if want[cc] {
			out[cc] = append(out[cc], res.Prefix())
		}
	}
	for cc := range out {
		sort.Slice(out[cc], func(i, j int) bool { return out[cc][i].Addr().Less(out[cc][j].Addr()) })
	}
	return out, nil
}

// NeedsUpdate reports whether the database is missing or older than MaxAge.
func NeedsUpdate(path string) bool {
	db, err := Open(path)
	if err != nil {
		return true
	}
	defer db.Close()
	return time.Since(db.Built) > MaxAge
}

// Download fetches this month's database, falling back to last month, then
// validates it before atomically replacing path.
func Download(ctx context.Context, client *http.Client, urlTemplate, path string) error {
	if urlTemplate == "" {
		urlTemplate = DefaultURL
	}
	now := time.Now().UTC()
	var lastErr error
	for _, month := range []time.Time{now, now.AddDate(0, -1, 0)} {
		url := strings.ReplaceAll(urlTemplate, "{month}", month.Format("2006-01"))
		if err := downloadOne(ctx, client, url, path); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

// downloadOne validates a temporary MMDB before replacing the installed file.
// The 200 MiB read limit applies to the HTTP body before optional gzip decompression.
func downloadOne(ctx context.Context, client *http.Client, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", url, res.StatusCode)
	}
	var body io.Reader = io.LimitReader(res.Body, 200<<20)
	if strings.HasSuffix(url, ".gz") {
		gz, err := gzip.NewReader(body)
		if err != nil {
			return fmt.Errorf("%s: %w", url, err)
		}
		defer gz.Close()
		body = gz
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, body); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("%s: %w", url, err)
	}
	f.Close()
	// Reject an unreadable or incompatible database before replacing the working file.
	db, err := Open(tmp)
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%s: file is not MMDB: %w", url, err)
	}
	if db.Country(netip.MustParseAddr("77.88.8.8")) == "" && db.Country(netip.MustParseAddr("8.8.8.8")) == "" {
		db.Close()
		os.Remove(tmp)
		return fmt.Errorf("%s: database has no country.iso_code field", url)
	}
	db.Close()
	return os.Rename(tmp, path)
}

// Flag converts an ISO country code to a flag emoji (RU → 🇷🇺).
func Flag(cc string) string {
	if len(cc) != 2 {
		return ""
	}
	cc = strings.ToUpper(cc)
	return string(rune(0x1F1E6+int(cc[0]-'A'))) + string(rune(0x1F1E6+int(cc[1]-'A')))
}
