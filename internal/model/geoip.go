package model

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"

	"github.com/ordo-systems/keen-proxy/internal/geoip"
)

// MaxGeoIPDatabases bounds configured databases, including built-ins.
const MaxGeoIPDatabases = 12

// LegacyGeoIPID names the custom database migrated from geoip.url or geoip.file.
const LegacyGeoIPID = "custom"

var (
	geoIPID     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	geoCategory = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{0,39}$`)
)

// GeoIPKindCategory marks a custom database whose iso_code values are labels
// such as RU-BLOCKED; it serves Group.Categories, never country lookups.
const GeoIPKindCategory = geoip.KindCategory

// IsBuiltin reports whether the entry references a bundled source.
func (d GeoIPDatabase) IsBuiltin() bool {
	_, ok := geoip.FindBuiltin(d.ID)
	return ok
}

// IsCategory reports whether the database serves categories instead of countries.
func (d GeoIPDatabase) IsCategory() bool {
	if b, ok := geoip.FindBuiltin(d.ID); ok {
		return b.Kind == geoip.KindCategory
	}
	return d.Kind == GeoIPKindCategory
}

func (g GeoIP) hasCategories() bool {
	for _, d := range g.Databases {
		if d.Enabled && d.IsCategory() {
			return true
		}
	}
	return false
}

// DisplayName returns the catalog name of a built-in, or the custom name or ID.
func (d GeoIPDatabase) DisplayName() string {
	if b, ok := geoip.FindBuiltin(d.ID); ok {
		return b.Name
	}
	if d.Name != "" {
		return d.Name
	}
	return d.ID
}

// defaults migrates single-database settings and appends missing built-ins.
// A new configuration uses the built-in default states; built-ins introduced
// after a configuration was written start disabled so routing does not change.
func (g *GeoIP) defaults() {
	fresh := len(g.Databases) == 0
	if fresh && (g.URL != "" || g.File != "") {
		dbip, _ := geoip.FindBuiltin("dbip")
		if g.File != "" || g.URL != dbip.Source.(geoip.URLSource).URL {
			legacy := GeoIPDatabase{ID: LegacyGeoIPID, Name: "Custom", URL: g.URL, File: g.File, Enabled: true}
			if g.File != "" {
				// A local file used to disable downloads entirely.
				legacy.URL = ""
			}
			g.Databases = []GeoIPDatabase{legacy}
			fresh = false
		}
		g.URL, g.File = "", ""
	}
	for _, b := range geoip.Builtins {
		found := false
		for _, d := range g.Databases {
			found = found || d.ID == b.ID
		}
		if !found {
			g.Databases = append(g.Databases, GeoIPDatabase{ID: b.ID, Enabled: fresh && b.Enabled})
		}
	}
}

func (g GeoIP) validate(add func(string, ...any)) {
	if g.URL != "" || g.File != "" {
		add("geoip: url and file are replaced by geoip.databases")
	}
	if len(g.Databases) > MaxGeoIPDatabases {
		add("geoip.databases: at most %d databases", MaxGeoIPDatabases)
	}
	seen := map[string]bool{}
	for i, d := range g.Databases {
		where := fmt.Sprintf("geoip.databases[%d] %q", i, d.ID)
		switch {
		case !geoIPID.MatchString(d.ID):
			add("%s: id requires lowercase Latin letters, digits and hyphens, up to 40 characters", where)
		case seen[d.ID]:
			add("%s: duplicate id", where)
		}
		seen[d.ID] = true
		if len(d.Name) > 64 {
			add("%s: name is longer than 64 characters", where)
		}
		if d.IsBuiltin() {
			if d.URL != "" || d.File != "" || d.Name != "" || d.Kind != "" {
				add("%s: built-in databases cannot change name, kind, url or file", where)
			}
			continue
		}
		if d.Kind != "" && d.Kind != GeoIPKindCategory {
			add("%s: kind can only be %q", where, GeoIPKindCategory)
		}
		switch {
		case (d.URL == "") == (d.File == ""):
			add("%s: set either url or file", where)
		case d.URL != "":
			u, err := url.Parse(d.URL)
			if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
				add("%s: requires an HTTPS URL without user name or password", where)
			}
		case !filepath.IsAbs(d.File):
			add("%s: file must be an absolute path", where)
		}
	}
}
