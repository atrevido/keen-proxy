// Package filterlist implements bounded DNS-only lists, independent of routing
// groups and the small DNS-answer IP cache.
package filterlist

import (
	"bufio"
	"fmt"
	"io"
	"net/netip"
	"regexp"
	"slices"
	"strings"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

// MaxBytes bounds the downloaded text accepted by Parse.
const MaxBytes = 8 << 20

// MaxRules bounds combined block and allow entries in one source.
const MaxRules = 200000

// MaxTotalRules bounds loaded entries across all configured sources.
const MaxTotalRules = 300000

// Parsed holds sorted unique DNS block/allow suffixes and a skipped-entry count.
type Parsed struct {
	Domains []string `json:"domains"`
	Allow   []string `json:"allow"`
	Skipped int      `json:"skipped"`
}

var domainRE = regexp.MustCompile(`^[a-z0-9_][a-z0-9_.-]*[a-z0-9]$`)

func domain(s string) string {
	e, err := model.ParseEntry(s)
	if err != nil || e.Kind != model.EntryDomain || !domainRE.MatchString(e.Domain) || strings.Contains(e.Domain, "..") {
		return ""
	}
	return e.Domain
}

// Parse accepts plain domains, hosts files, and basic Adblock DNS rules under fixed limits.
func Parse(r io.Reader) (Parsed, error) {
	p := Parsed{}
	lr := &io.LimitedReader{R: r, N: MaxBytes + 1}
	sc := bufio.NewScanner(lr)
	sc.Buffer(make([]byte, 4096), 65536)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "[") {
			continue
		}
		if strings.Contains(line, "##") || strings.Contains(line, "#@#") || strings.Contains(line, "#?#") || strings.Contains(line, "#$#") {
			p.Skipped++
			continue
		}
		if i := strings.IndexByte(line, '#'); i >= 0 && i > 0 && (line[i-1] == ' ' || line[i-1] == '\t') {
			line = strings.TrimSpace(line[:i])
		}
		allow := strings.HasPrefix(line, "@@||")
		if allow {
			line = strings.TrimPrefix(line, "@@")
		}
		if strings.HasPrefix(line, "||") && strings.HasSuffix(line, "^") {
			line = strings.TrimSuffix(strings.TrimPrefix(line, "||"), "^")
		}
		fields := strings.Fields(line)
		if len(fields) > 1 {
			if a, err := netip.ParseAddr(fields[0]); err == nil && (a.IsUnspecified() || a.IsLoopback()) {
				fields = fields[1:]
			} else {
				p.Skipped++
				continue
			}
		}
		for _, s := range fields {
			d := domain(s)
			if d == "" {
				p.Skipped++
				continue
			}
			if allow {
				p.Allow = append(p.Allow, d)
			} else {
				p.Domains = append(p.Domains, d)
			}
		}
		if len(p.Domains)+len(p.Allow) > MaxRules {
			return Parsed{}, fmt.Errorf("at most %d rules", MaxRules)
		}
	}
	if err := sc.Err(); err != nil {
		return Parsed{}, err
	}
	if lr.N <= 0 {
		return Parsed{}, fmt.Errorf("list exceeds 8 MiB")
	}
	slices.Sort(p.Domains)
	p.Domains = slices.Compact(p.Domains)
	slices.Sort(p.Allow)
	p.Allow = slices.Compact(p.Allow)
	if len(p.Domains) == 0 {
		return Parsed{}, fmt.Errorf("empty or unsupported list (%d skipped)", p.Skipped)
	}
	return p, nil
}
func matches(domains []string, name string) bool {
	for {
		name = strings.TrimSuffix(strings.ToLower(name), ".")
		if _, ok := slices.BinarySearch(domains, name); ok {
			return true
		}
		i := strings.IndexByte(name, '.')
		if i < 0 {
			return false
		}
		name = name[i+1:]
	}
}
