package geoip

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Artifact is one downloadable database file resolved from a Source.
type Artifact struct {
	URL string
	// Version identifies the content when the source publishes one (month,
	// release digest). Equal versions skip the download; empty versions fall
	// back to HTTP conditional requests.
	Version string
	// SHA256 is the expected lowercase hex digest of the downloaded bytes, if published.
	SHA256 string
}

// Source resolves the current download candidates of a database, best first.
type Source interface {
	// Key identifies the source definition; saved state for another key is discarded.
	Key() string
	Resolve(ctx context.Context, c *http.Client, now time.Time) ([]Artifact, error)
}

// URLSource downloads a fixed HTTPS URL. A {month} placeholder expands to
// YYYY-MM: the current month is tried first, then the previous month.
type URLSource struct{ URL string }

func (s URLSource) Key() string { return "url:" + s.URL }

// Resolve needs no network access; the month doubles as the content version.
func (s URLSource) Resolve(_ context.Context, _ *http.Client, now time.Time) ([]Artifact, error) {
	if !strings.Contains(s.URL, "{month}") {
		return []Artifact{{URL: s.URL}}, nil
	}
	now = now.UTC()
	var out []Artifact
	for _, m := range []time.Time{now, time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)} {
		month := m.Format("2006-01")
		out = append(out, Artifact{URL: strings.ReplaceAll(s.URL, "{month}", month), Version: "month:" + month})
	}
	return out, nil
}

// GitHubRelease downloads a named asset of a GitHub release. An empty Tag
// selects the latest release. The release API supplies the asset digest,
// which is both the content version and an integrity check.
type GitHubRelease struct {
	Repo  string // owner/name
	Tag   string
	Asset string
	API   string        // API base URL; empty selects https://api.github.com
	Every time.Duration // check interval; zero selects CheckInterval
}

func (g GitHubRelease) Interval() time.Duration { return g.Every }

// Interval returns how often a source should be checked for updates.
func Interval(src Source) time.Duration {
	if s, ok := src.(interface{ Interval() time.Duration }); ok && s.Interval() > 0 {
		return s.Interval()
	}
	return CheckInterval
}

func (g GitHubRelease) Key() string { return "github:" + g.Repo + "@" + g.Tag + "/" + g.Asset }

func (g GitHubRelease) Resolve(ctx context.Context, c *http.Client, _ time.Time) ([]Artifact, error) {
	base := g.API
	if base == "" {
		base = "https://api.github.com"
	}
	path := "/repos/" + g.Repo + "/releases/latest"
	if g.Tag != "" {
		path = "/repos/" + g.Repo + "/releases/tags/" + url.PathEscape(g.Tag)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", userAgent)
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub %s: HTTP %d", g.Repo, res.StatusCode)
	}
	var rel struct {
		Tag    string `json:"tag_name"`
		Assets []struct {
			Name    string `json:"name"`
			URL     string `json:"browser_download_url"`
			Digest  string `json:"digest"`
			Updated string `json:"updated_at"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("GitHub %s: %w", g.Repo, err)
	}
	for _, a := range rel.Assets {
		if a.Name != g.Asset {
			continue
		}
		art := Artifact{URL: a.URL, Version: rel.Tag + "/" + a.Updated}
		if sum, ok := strings.CutPrefix(a.Digest, "sha256:"); ok {
			if b, err := hex.DecodeString(sum); err != nil || len(b) != 32 {
				return nil, fmt.Errorf("GitHub %s: invalid digest %q", g.Repo, a.Digest)
			}
			art.SHA256 = strings.ToLower(sum)
			art.Version = rel.Tag + "/sha256:" + art.SHA256
		}
		return []Artifact{art}, nil
	}
	return nil, fmt.Errorf("GitHub %s %s: no asset %s", g.Repo, rel.Tag, g.Asset)
}

// Database kinds: country databases answer country lookups in priority order;
// category databases hold labels such as RU-BLOCKED in country.iso_code and
// serve only category groups.
const (
	KindCountry  = "country"
	KindCategory = "category"
)

// Builtin describes a bundled database source. Users enable, disable, and
// reorder built-ins but cannot change where they come from.
type Builtin struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	// Categories lists the labels a category database is known to publish.
	Categories []string `json:"categories,omitempty"`
	Homepage   string   `json:"homepage"`
	License    string   `json:"license"`
	// Attribution must be displayed while the database is in use, if set.
	Attribution    string `json:"attribution,omitempty"`
	AttributionURL string `json:"attribution_url,omitempty"`
	Updates        string `json:"updates"` // publisher's update cadence: daily, weekly, monthly
	Enabled        bool   `json:"enabled"` // default state for new configurations
	Origin         string `json:"origin"`  // human-readable download origin
	Source         Source `json:"-"`
}

// Builtins lists bundled sources in default priority order. The data is
// downloaded separately and keeps its own license; see THIRD_PARTY_NOTICES.md.
var Builtins = []Builtin{
	{
		ID:             "dbip",
		Name:           "DB-IP IP to Country Lite",
		Kind:           KindCountry,
		Homepage:       "https://db-ip.com/db/download/ip-to-country-lite",
		License:        "CC BY 4.0",
		Attribution:    "IP Geolocation by DB-IP",
		AttributionURL: "https://db-ip.com/",
		Updates:        "monthly",
		Enabled:        true,
		Origin:         "download.db-ip.com",
		Source: URLSource{
			URL: "https://download.db-ip.com/free/dbip-country-lite-{month}.mmdb.gz",
		},
	},
	{
		ID:       "ip-location-db",
		Name:     "ip-location-db user-country",
		Kind:     KindCountry,
		Homepage: "https://github.com/sapics/ip-location-db",
		License:  "PDDL 1.0",
		Updates:  "daily",
		Origin:   "GitHub release sapics/ip-location-db · user-country.mmdb",
		Source: GitHubRelease{
			Repo:  "sapics/ip-location-db",
			Tag:   "latest",
			Asset: "user-country.mmdb",
		},
	},
	{
		ID:             "loyalsoldier",
		Name:           "Loyalsoldier GeoIP (GeoLite2-based)",
		Kind:           KindCountry,
		Homepage:       "https://github.com/Loyalsoldier/geoip",
		License:        "GeoLite2 data, MaxMind terms",
		Attribution:    "Includes GeoLite2 data created by MaxMind",
		AttributionURL: "https://www.maxmind.com",
		Updates:        "weekly",
		Origin:         "GitHub latest release Loyalsoldier/geoip · Country-without-asn.mmdb",
		Source: GitHubRelease{
			Repo:  "Loyalsoldier/geoip",
			Asset: "Country-without-asn.mmdb",
		},
	},
	// runetfreedom publishes each network under a single label, so a network
	// listed by several upstream lists appears in only one category.
	{
		ID: "runetfreedom-ru", Name: "runetfreedom: blocked in Russia",
		Kind:       KindCategory,
		Categories: []string{"RU-BLOCKED", "RU-BLOCKED-COMMUNITY", "RE-FILTER", "RU-WHITELIST", "PRIVATE"},
		Homepage:   "https://github.com/runetfreedom/russia-blocked-geoip",
		License:    "GPL-3.0 repository; data from antifilter.download and re:filter",
		Updates:    "6h",
		Origin:     "GitHub latest release runetfreedom/russia-blocked-geoip · Country-ru-only.mmdb",
		Source: GitHubRelease{
			Repo:  "runetfreedom/russia-blocked-geoip",
			Asset: "Country-ru-only.mmdb",
			Every: 6 * time.Hour,
		},
	},
	{
		ID:         "runetfreedom-services",
		Name:       "runetfreedom: service networks",
		Kind:       KindCategory,
		Categories: []string{"CLOUDFLARE", "CLOUDFRONT", "DDOS-GUARD", "FACEBOOK", "FASTLY", "GOOGLE", "NETFLIX", "TELEGRAM", "TWITTER", "YANDEX"},
		Homepage:   "https://github.com/runetfreedom/russia-blocked-geoip",
		License:    "GPL-3.0 repository",
		Updates:    "6h",
		Origin:     "GitHub latest release runetfreedom/russia-blocked-geoip · Country-asn.mmdb",
		Source: GitHubRelease{
			Repo:  "runetfreedom/russia-blocked-geoip",
			Asset: "Country-asn.mmdb",
			Every: 6 * time.Hour,
		},
	},
}

// FindBuiltin returns the built-in source with the given ID.
func FindBuiltin(id string) (Builtin, bool) {
	for _, b := range Builtins {
		if b.ID == id {
			return b, true
		}
	}
	return Builtin{}, false
}
