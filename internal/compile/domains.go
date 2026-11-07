package compile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"net/netip"
	"sort"
	"strings"
)

// DomainTag is stable when unrelated groups move and changes with group content.
func DomainTag(g model.Group) string {
	raw, _ := json.Marshal(g)
	sum := sha256.Sum256(raw)
	return "dns-" + hex.EncodeToString(sum[:8])
}

// DomainNames returns sorted, unique domain roots from enabled groups.
func DomainNames(c *model.Config) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, g := range c.Groups {
		if g.Disabled {
			continue
		}
		for _, raw := range g.Entries {
			e, err := model.ParseEntry(raw)
			if err == nil && e.Kind == model.EntryDomain && !seen[e.Domain] {
				seen[e.Domain] = true
				out = append(out, e.Domain)
			}
		}
	}
	sort.Strings(out)
	return out
}

// DomainMatcher normalizes a configuration once, then matches by label suffix.
type DomainMatcher struct{ names map[string]bool }

// NewDomainMatcher indexes normalized group domains for repeated suffix checks.
func NewDomainMatcher(c *model.Config) *DomainMatcher {
	m := &DomainMatcher{names: map[string]bool{}}
	for _, n := range DomainNames(c) {
		m.names[n] = true
	}
	return m
}

// Match accepts an exact domain or any label-delimited subdomain.
func (m *DomainMatcher) Match(name string) bool {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	for {
		if m.names[name] {
			return true
		}
		_, rest, ok := strings.Cut(name, ".")
		if !ok {
			return false
		}
		name = rest
	}
}

// MatchesDomain checks whether a name belongs to an enabled routing group.
func MatchesDomain(c *model.Config, name string) bool { return NewDomainMatcher(c).Match(name) }

// DNSRuleSets builds deterministic IPv4 fallback sets for enabled entry groups.
// The caller must supply only fresh cache addresses; empty sets match nothing.
func DNSRuleSets(c *model.Config, records map[string][]string) map[string][]byte {
	out := map[string][]byte{}
	for _, g := range c.Groups {
		if g.Disabled {
			continue
		}
		if g.IsCountry() {
			continue
		}
		seen := map[string]bool{}
		cidrs := []string{}
		for _, raw := range g.Entries {
			e, err := model.ParseEntry(raw)
			if err != nil || e.Kind != model.EntryDomain {
				continue
			}
			for name, ips := range records {
				if name != e.Domain && !strings.HasSuffix(name, "."+e.Domain) {
					continue
				}
				for _, ip := range ips {
					a, err := netip.ParseAddr(ip)
					if err != nil || !a.Is4() {
						continue
					}
					p := netip.PrefixFrom(a, 32).String()
					if !seen[p] {
						seen[p] = true
						cidrs = append(cidrs, p)
					}
				}
			}
		}
		sort.Strings(cidrs)
		if len(cidrs) == 0 {
			cidrs = []string{noSource}
		}
		out[DomainTag(g)+".json"], _ = json.Marshal(obj{"version": 3, "rules": []obj{{"ip_cidr": cidrs}}})
	}
	return out
}
