// Package lists matches domain names and IP addresses to routing groups.
package lists

import (
	"net/netip"
	"strings"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

// Matcher looks up domain suffixes or IP prefixes. Duplicate entries retain
// the first configured group; domain lookup prefers the longest suffix.
type Matcher struct {
	domains  map[string]int
	prefixes []prefixGroup
}

// prefixGroup retains configuration order alongside one address match.
type prefixGroup struct {
	prefix netip.Prefix
	group  int
}

// NoMatch indicates that no group matched.
const NoMatch = -1

// New indexes validated group entries in configuration order.
func New(groups []model.Group) *Matcher {
	m := &Matcher{domains: map[string]int{}}
	for gi, g := range groups {
		if g.Disabled {
			continue
		}
		for _, raw := range g.Entries {
			e, err := model.ParseEntry(raw)
			if err != nil {
				continue // The configuration has already passed Validate.
			}
			switch e.Kind {
			case model.EntryDomain:
				if _, dup := m.domains[e.Domain]; !dup {
					m.domains[e.Domain] = gi
				}
			case model.EntryPrefix:
				m.prefixes = append(m.prefixes, prefixGroup{e.Prefix, gi})
			}
		}
	}
	return m
}

// Domain returns a group index or NoMatch, preferring the longest suffix:
// www.youtube.com → youtube.com → com.
func (m *Matcher) Domain(name string) int {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	for {
		if gi, ok := m.domains[name]; ok {
			return gi
		}
		i := strings.IndexByte(name, '.')
		if i < 0 {
			return NoMatch
		}
		name = name[i+1:]
	}
}

// Addr returns the first group whose prefix contains the address, or NoMatch.
func (m *Matcher) Addr(a netip.Addr) int {
	a = a.Unmap()
	for _, p := range m.prefixes {
		if p.prefix.Contains(a) {
			return p.group
		}
	}
	return NoMatch
}
