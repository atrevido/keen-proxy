package catalog

import "github.com/atrevido/keenetic-proxy/internal/model"

// DNSProfile describes provider endpoints and their bootstrap address.
type DNSProfile struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Filtering string `json:"filtering"`
	DoH       string `json:"doh"`
	DoT       string `json:"dot"`
	IP        string `json:"ip"`
	Source    string `json:"source"`
}

// DNS contains bundled resolver choices with provenance URLs.
var DNS = []DNSProfile{
	{"cloudflare", "Cloudflare", "none", "https://one.one.one.one/dns-query", "tls://one.one.one.one", "1.1.1.1", "https://developers.cloudflare.com/1.1.1.1/encryption/"},
	{"google", "Google Public DNS", "none", "https://dns.google/dns-query", "tls://dns.google", "8.8.8.8", "https://developers.google.com/speed/public-dns/docs/secure-transports"},
	{"quad9", "Quad9 Secure", "security", "https://dns.quad9.net/dns-query", "tls://dns.quad9.net", "9.9.9.9", "https://docs.quad9.net/services/"},
	{"quad9-unfiltered", "Quad9 Unfiltered", "none", "https://dns10.quad9.net/dns-query", "tls://dns10.quad9.net", "9.9.9.10", "https://docs.quad9.net/services/"},
	{"adguard", "AdGuard DNS", "ads", "https://dns.adguard-dns.com/dns-query", "tls://dns.adguard-dns.com", "94.140.14.14", "https://adguard-dns.io/en/public-dns.html"},
	{"adguard-unfiltered", "AdGuard Unfiltered", "none", "https://unfiltered.adguard-dns.com/dns-query", "tls://unfiltered.adguard-dns.com", "94.140.14.140", "https://adguard-dns.io/en/public-dns.html"},
	{"adguard-family", "AdGuard Family", "family", "https://family.adguard-dns.com/dns-query", "tls://family.adguard-dns.com", "94.140.14.15", "https://adguard-dns.io/en/public-dns.html"},
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
