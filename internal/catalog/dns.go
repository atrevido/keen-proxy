package catalog

import (
	_ "embed"
	"encoding/json"

	"github.com/ordo-systems/keen-proxy/internal/model"
)

// DNSProfile describes provider endpoints and their bootstrap address.
type DNSProfile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Filtering   string `json:"filtering"`
	DoH         string `json:"doh"`
	DoT         string `json:"dot"`
	IP          string `json:"ip"`
	BootstrapIP string `json:"bootstrap_ip,omitempty"`
	Source      string `json:"source"`
}

// DNS contains bundled resolver choices with provenance URLs.
var DNS = loadDNS()

//go:embed dns.json
var dnsData []byte

func loadDNS() []DNSProfile {
	var profiles []DNSProfile
	if err := json.Unmarshal(dnsData, &profiles); err != nil {
		panic(err)
	}
	return profiles
}

// Filter adds catalog category, provenance, and license metadata to a blocklist source.
type Filter struct {
	model.FilterSource
	Category string `json:"category"`
	Source   string `json:"source"`
	License  string `json:"license"`
}

// Filters contains available blocklists; installation remains an explicit configuration edit.
var Filters = []Filter{
	{model.FilterSource{ID: "hagezi-light", Name: "HaGeZi Light", URL: "https://raw.githubusercontent.com/hagezi/dns-blocklists/main/wildcard/light-onlydomains.txt"}, "ads", "https://github.com/hagezi/dns-blocklists", "GPL-3.0"},
	{model.FilterSource{ID: "hagezi-normal", Name: "HaGeZi Normal", URL: "https://raw.githubusercontent.com/hagezi/dns-blocklists/main/wildcard/multi-onlydomains.txt"}, "ads", "https://github.com/hagezi/dns-blocklists", "GPL-3.0"},
	{model.FilterSource{ID: "hagezi-tif-mini", Name: "HaGeZi Threat Intelligence Mini", URL: "https://raw.githubusercontent.com/hagezi/dns-blocklists/main/wildcard/tif.mini-onlydomains.txt"}, "security", "https://github.com/hagezi/dns-blocklists", "GPL-3.0"},
	{model.FilterSource{ID: "hagezi-nsfw", Name: "HaGeZi NSFW", URL: "https://raw.githubusercontent.com/hagezi/dns-blocklists/main/wildcard/nsfw-onlydomains.txt"}, "adult", "https://github.com/hagezi/dns-blocklists", "GPL-3.0"},
}
