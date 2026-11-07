// Package geoip maps IP addresses to countries for routing rules and connection flags.
//
// Several MMDB country databases may be configured; Set answers lookups in
// priority order. Built-in sources are listed in Builtins, and Store keeps
// downloaded copies fresh. Any MMDB with country.iso_code is supported.
package geoip

import (
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
)

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

// networks calls fn for every network that has a country code, in address order.
func (db *DB) networks(fn func(p netip.Prefix, cc string)) error {
	for res := range db.r.Networks() {
		var rec record
		if err := res.Decode(&rec); err != nil {
			return err
		}
		if cc := strings.ToUpper(rec.Country.ISOCode); cc != "" {
			fn(res.Prefix(), cc)
		}
	}
	return nil
}

// Prefixes collects the requested countries' subnets in one database traversal.
func (db *DB) Prefixes(codes []string) (map[string][]netip.Prefix, error) {
	want := wanted(codes)
	out := map[string][]netip.Prefix{}
	err := db.networks(func(p netip.Prefix, cc string) {
		if want[cc] {
			out[cc] = append(out[cc], p)
		}
	})
	if err != nil {
		return nil, err
	}
	for cc := range out {
		sort.Slice(out[cc], func(i, j int) bool { return out[cc][i].Addr().Less(out[cc][j].Addr()) })
	}
	return out, nil
}

// hasCountries reports whether any of the first networks carries country.iso_code,
// rejecting MMDBs such as ASN databases that would match no country.
func (db *DB) hasCountries() bool {
	n := 0
	for res := range db.r.Networks() {
		var rec record
		if res.Decode(&rec) == nil && rec.Country.ISOCode != "" {
			return true
		}
		if n++; n > 100_000 {
			break
		}
	}
	return false
}

func wanted(codes []string) map[string]bool {
	want := map[string]bool{}
	for _, c := range codes {
		want[strings.ToUpper(c)] = true
	}
	return want
}

// Flag converts an ISO country code to a flag emoji (RU → 🇷🇺).
func Flag(cc string) string {
	if len(cc) != 2 {
		return ""
	}
	cc = strings.ToUpper(cc)
	return string(rune(0x1F1E6+int(cc[0]-'A'))) + string(rune(0x1F1E6+int(cc[1]-'A')))
}
