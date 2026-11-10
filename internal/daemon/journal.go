package daemon

import (
	"context"
	"fmt"
	"net/netip"
	"reflect"
	"sort"
	"strings"

	"github.com/sagernet/sing-box/adapter"

	"github.com/ordo-systems/keen-proxy/internal/compile"
	"github.com/ordo-systems/keen-proxy/internal/journal"
	"github.com/ordo-systems/keen-proxy/internal/model"
	"github.com/ordo-systems/keen-proxy/internal/stats"
)

// actorKey keeps audit attribution separate from other context values.
type actorKey struct{}

// Actor identifies the initiator of an audited action.
type Actor struct {
	Who  string // user, system, watchdog
	From string // source address of the dashboard request
}

// WithActor attaches audit attribution to an operation context.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

func actorOf(ctx context.Context) Actor {
	if a, ok := ctx.Value(actorKey{}).(Actor); ok {
		return a
	}
	return Actor{Who: "system"}
}

// Journal returns the requested journal stream.
func (d *Daemon) Journal(k journal.Kind) *journal.Journal { return d.journals[k] }

// Mask returns a function that redacts current configuration secrets from log exports.
func (d *Daemon) Mask() func(string) string { return journal.Masker(d.Config()) }

// audit records an operation result with the initiating actor and target.
func (d *Daemon) audit(who, action, target, result, details string) {
	d.auditEntry(journal.Entry{Actor: who, Action: action, Target: target, Result: result, Details: details})
}

// auditEntry records an audit event with optional structured evidence.
func (d *Daemon) auditEntry(entry journal.Entry) {
	d.journals[journal.Audit].Add(entry)
}

// onRoute journals each sing-box connection using the atomic configuration
// snapshot to avoid taking d.mu on the routing path.
func (d *Daemon) onRoute(m adapter.InboundContext, rule adapter.Rule, out adapter.Outbound) {
	e := journal.Entry{
		DestinationPort: m.Destination.Port,
		Host:            stats.HostOf(m),
		Client:          m.Source.String(),
		Network:         m.Network,
		Outbound:        "direct",
	}
	if m.Destination.Addr.IsValid() {
		e.IP = m.Destination.Addr.Unmap().String()
	} else if len(m.DestinationAddresses) > 0 {
		e.IP = m.DestinationAddresses[0].Unmap().String()
	}
	cfg := d.live.Load()
	var note string
	if out != nil {
		real := stats.Resolve(out)
		e.Outbound = real.Tag()
		// The fallback selector chose a non-primary connection.
		if head, ok := strings.CutPrefix(out.Tag(), compile.FailoverTagPrefix); ok && head != real.Tag() {
			note = " · fallback: «" + connLabel(cfg, head) + "» is unavailable"
		}
	}
	if rule != nil {
		e.RuleRaw = rule.String()
	}
	e.Rule = journal.DescribeRule(cfg, e.RuleRaw) + note
	if strings.Contains(e.RuleRaw, "dns-") {
		e.Rule += dnsRouteNote(cfg, d.domainRecords(), e.RuleRaw, e.IP)
	}
	d.journals[journal.Routes].Add(e)
}

func connLabel(cfg *model.Config, id string) string {
	if cfg != nil {
		for _, cn := range cfg.Connections {
			if cn.ID == id && cn.Name != "" {
				return cn.Name
			}
		}
	}
	return id
}

// configChanges summarizes configuration differences for the audit journal.
func configChanges(prev, cfg *model.Config) string {
	if prev == nil {
		return "first start"
	}
	var parts []string
	count := func(name string, a, b int, same bool) {
		switch {
		case a != b:
			parts = append(parts, fmt.Sprintf("%s: %d → %d", name, a, b))
		case !same:
			parts = append(parts, name+" changed")
		}
	}
	count("connections", len(prev.Connections), len(cfg.Connections), reflect.DeepEqual(prev.Connections, cfg.Connections))
	count("groups", len(prev.Groups), len(cfg.Groups), reflect.DeepEqual(prev.Groups, cfg.Groups))
	count("networks", len(prev.Networks), len(cfg.Networks), reflect.DeepEqual(prev.Networks, cfg.Networks))
	count("devices", len(prev.Clients), len(cfg.Clients), reflect.DeepEqual(prev.Clients, cfg.Clients))
	if !reflect.DeepEqual(prev.DNS, cfg.DNS) {
		parts = append(parts, "DNS changed")
	}
	if !reflect.DeepEqual(prev.Intercept, cfg.Intercept) {
		parts = append(parts, "interception changed")
	}
	if !reflect.DeepEqual(prev.API.NoTokenFrom, cfg.API.NoTokenFrom) {
		if len(cfg.API.NoTokenFrom) == 0 {
			parts = append(parts, "tokenless login disabled")
		} else {
			parts = append(parts, "tokenless login from "+strings.Join(cfg.API.NoTokenFrom, ", "))
		}
	}
	if !reflect.DeepEqual(prev.GeoIP, cfg.GeoIP) {
		parts = append(parts, "country database changed")
	}
	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, "; ")
}

// neighborsChange summarizes newly visible and departed devices.
func neighborsChange(old, cur map[string]netip.Addr) string {
	var added, gone []string
	for mac, ip := range cur {
		if o, ok := old[mac]; !ok || o != ip {
			added = append(added, ip.String()+" ("+mac+")")
		}
	}
	for mac, ip := range old {
		if _, ok := cur[mac]; !ok {
			gone = append(gone, ip.String()+" ("+mac+")")
		}
	}
	sort.Strings(added)
	sort.Strings(gone)
	var parts []string
	if len(added) > 0 {
		parts = append(parts, "added: "+strings.Join(added, ", "))
	}
	if len(gone) > 0 {
		parts = append(parts, "removed: "+strings.Join(gone, ", "))
	}
	parts = append(parts, fmt.Sprintf("online: %d", len(cur)))
	return strings.Join(parts, "; ")
}
