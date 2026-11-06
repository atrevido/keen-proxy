package journal

import (
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/atrevido/keenetic-proxy/internal/compile"
	"github.com/atrevido/keenetic-proxy/internal/model"
)

// Rule-set tags emitted by internal/compile.
const (
	tagGroup      = "group-"
	tagGeoIP      = "geoip-"
	tagClientOff  = "clients-off"
	tagClientFull = "clients-full-"
	tagClientSplt = "clients-split"
)

var (
	reRuleSet = regexp.MustCompile(`(!\()?rule_set=(\[[^\]]*\]|[^\s)]+)`)
	reSource  = regexp.MustCompile(`source_ip_cidr=(\[[^\]]*\]|\S+)`)
)

func list(s string) []string {
	return strings.Fields(strings.Trim(s, "[]"))
}

// DescribeRule turns adapter.Rule.String() into a readable group, device,
// or network explanation. An empty rule means no rule matched.
func DescribeRule(cfg *model.Config, rule string) string {
	if rule == "" {
		return "ни одно правило не подошло — напрямую"
	}
	if strings.Contains(rule, "ip_is_private=true") {
		return "частный адрес — всегда напрямую"
	}
	var who, what string
	for _, m := range reRuleSet.FindAllStringSubmatch(rule, -1) {
		tags := list(m[2])
		invert := m[1] != ""
		switch t := tags[0]; {
		case t == tagClientOff:
			who = "устройство не участвует"
		case strings.HasPrefix(t, tagClientFull):
			who = "устройство в полном туннеле"
		case t == tagClientSplt:
			who = "устройство"
		case strings.HasPrefix(t, tagGroup):
			what = groupByIndex(cfg, t)
		case strings.HasPrefix(t, "dns-"):
			if cfg != nil {
				for _, g := range cfg.Groups {
					if compile.DomainTag(g) == t {
						what = "резерв по IP из DNS: группа «" + g.Name + "»"
						break
					}
				}
			}
		case strings.HasPrefix(t, tagGeoIP):
			what = countryGroup(cfg, tags, invert)
		}
	}
	if who == "" {
		if m := reSource.FindStringSubmatch(rule); m != nil {
			who = networkLabel(cfg, list(m[1]), what != "")
		}
	}
	switch {
	case who != "" && what != "":
		return who + ": " + what
	case what != "":
		return what
	case who == "устройство":
		return "устройство: ни одна группа не подошла — напрямую"
	case who != "":
		return who
	}
	return rule
}

func groupByIndex(cfg *model.Config, tag string) string {
	i, err := strconv.Atoi(strings.TrimPrefix(tag, tagGroup))
	if err != nil || cfg == nil || i < 0 || i >= len(cfg.Groups) {
		return "группа " + tag
	}
	return "группа «" + cfg.Groups[i].Name + "»"
}

// countryGroup labels country tags and inversion, using a configured group name only for an exact match.
func countryGroup(cfg *model.Config, tags []string, invert bool) string {
	var codes []string
	for _, t := range tags {
		codes = append(codes, strings.ToUpper(strings.TrimPrefix(t, tagGeoIP)))
	}
	desc := "страны " + strings.Join(codes, ", ")
	if invert {
		desc = "все, кроме " + strings.Join(codes, ", ")
	}
	if cfg != nil {
		for _, g := range cfg.Groups {
			if g.IsCountry() && g.Except == invert && slices.Equal(g.Countries, codes) {
				return "группа «" + g.Name + "» (" + desc + ")"
			}
		}
	}
	return desc
}

// networkLabel identifies the network from rule source addresses. withGroup
// denotes a split-mode group rule; otherwise the network mode determines the label.
func networkLabel(cfg *model.Config, cidrs []string, withGroup bool) string {
	if cfg == nil {
		return "сеть " + strings.Join(cidrs, ", ")
	}
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			continue
		}
		for _, n := range cfg.Networks {
			np, err := netip.ParsePrefix(n.CIDR)
			if err != nil || np.Masked() != p.Masked() {
				continue
			}
			name := "сеть «" + n.Name + "»"
			switch {
			case withGroup:
				return name
			case n.Mode == model.ModeOff:
				return name + " не участвует"
			case n.Mode == model.ModeFull:
				return name + " в полном туннеле"
			default:
				return name + ": ни одна группа не подошла — напрямую"
			}
		}
	}
	return "сеть " + strings.Join(cidrs, ", ")
}
