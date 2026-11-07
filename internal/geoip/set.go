package geoip

import (
	"net/netip"
	"slices"

	"go4.org/netipx"
)

// Member is one open database inside a Set.
type Member struct {
	ID string
	DB *DB
}

// Set answers country lookups from several databases in priority order: an
// address belongs to the country reported by the first database that knows it.
// The Set owns its databases and closes them in Close.
type Set struct {
	members []Member
}

// NewSet takes ownership of the given databases, highest priority first.
func NewSet(members ...Member) *Set { return &Set{members: members} }

// Members returns the databases in priority order; callers must not close them.
func (s *Set) Members() []Member {
	if s == nil {
		return nil
	}
	return s.members
}

// Len reports how many databases answer lookups.
func (s *Set) Len() int { return len(s.Members()) }

// Close releases every database in the set.
func (s *Set) Close() {
	if s == nil {
		return
	}
	for _, m := range s.members {
		m.DB.Close()
	}
	s.members = nil
}

// Country returns the first known country and the ID of the database that answered.
func (s *Set) Country(ip netip.Addr) (cc, id string) {
	for _, m := range s.Members() {
		if cc := m.DB.Country(ip); cc != "" {
			return cc, m.ID
		}
	}
	return "", ""
}

// Prefixes returns the requested countries' subnets with the same precedence
// as Country: addresses known to a higher-priority database are removed from
// the lower-priority databases' answers.
func (s *Set) Prefixes(codes []string) (map[string][]netip.Prefix, error) {
	members := s.Members()
	switch len(members) {
	case 0:
		return map[string][]netip.Prefix{}, nil
	case 1:
		return members[0].DB.Prefixes(codes)
	}
	want := wanted(codes)
	result := map[string]*netipx.IPSetBuilder{}
	covered := &netipx.IPSet{}
	for _, m := range members {
		var known ranges
		found := map[string]*ranges{}
		err := m.DB.networks(func(p netip.Prefix, cc string) {
			known.add(p)
			if want[cc] {
				if found[cc] == nil {
					found[cc] = &ranges{}
				}
				found[cc].add(p)
			}
		})
		if err != nil {
			return nil, err
		}
		for cc, r := range found {
			var b netipx.IPSetBuilder
			r.into(&b)
			b.RemoveSet(covered)
			if result[cc] == nil {
				result[cc] = &netipx.IPSetBuilder{}
			}
			result[cc].AddSet(mustSet(&b))
		}
		var next netipx.IPSetBuilder
		next.AddSet(covered)
		known.into(&next)
		covered = mustSet(&next)
	}
	out := map[string][]netip.Prefix{}
	for cc, b := range result {
		if p := mustSet(b).Prefixes(); len(p) > 0 {
			out[cc] = p
		}
	}
	return out, nil
}

// mustSet builds a set; netipx only reports errors for invalid input, which
// MMDB networks never produce.
func mustSet(b *netipx.IPSetBuilder) *netipx.IPSet {
	s, _ := b.IPSet()
	return s
}

// ranges coalesces adjacent networks that arrive in address order, keeping
// set construction small for databases with hundreds of thousands of networks.
type ranges struct {
	done []netipx.IPRange
	cur  netipx.IPRange
}

func (r *ranges) add(p netip.Prefix) {
	next := netipx.RangeOfPrefix(p.Masked())
	if r.cur.IsValid() && r.cur.To().Is4() == next.From().Is4() && r.cur.To().Next() == next.From() {
		r.cur = netipx.IPRangeFrom(r.cur.From(), next.To())
		return
	}
	if r.cur.IsValid() {
		r.done = append(r.done, r.cur)
	}
	r.cur = next
}

func (r *ranges) into(b *netipx.IPSetBuilder) {
	for _, x := range r.done {
		b.AddRange(x)
	}
	if r.cur.IsValid() {
		b.AddRange(r.cur)
	}
}

// Labels returns every distinct non-empty answer for ip, in member order.
// Category databases use it: labels are independent, not prioritized.
func (s *Set) Labels(ip netip.Addr) []string {
	var out []string
	for _, m := range s.Members() {
		if l := m.DB.Country(ip); l != "" && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

// Union returns the requested labels' subnets merged across all members,
// for category databases whose answers do not override each other.
func (s *Set) Union(codes []string) (map[string][]netip.Prefix, error) {
	members := s.Members()
	if len(members) == 1 {
		return members[0].DB.Prefixes(codes)
	}
	builders := map[string]*netipx.IPSetBuilder{}
	for _, m := range members {
		p, err := m.DB.Prefixes(codes)
		if err != nil {
			return nil, err
		}
		for cc, list := range p {
			if builders[cc] == nil {
				builders[cc] = &netipx.IPSetBuilder{}
			}
			for _, x := range list {
				builders[cc].AddPrefix(x)
			}
		}
	}
	out := map[string][]netip.Prefix{}
	for cc, b := range builders {
		out[cc] = mustSet(b).Prefixes()
	}
	return out, nil
}
