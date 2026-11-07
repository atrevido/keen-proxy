package api

import (
	"slices"
	"strings"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/journal"
)

func known(value string, allowed ...string) string {
	if slices.Contains(allowed, value) {
		return value
	}
	return ""
}

// supportEntry writes only known event labels and aliased routing fields.
// Historical errors may include credentials no longer in the active config.
func supportEntry(kind journal.Kind, e journal.Entry, alias supportAlias) journal.Entry {
	safe := journal.Entry{ID: e.ID, Time: e.Time.UTC().Truncate(time.Second), Count: e.Count}
	switch kind {
	case journal.Routes:
		safe.Client = alias("ip", journalClientHost(e.Client))
		safe.Host = alias("host", e.Host)
		safe.IP = alias("ip", e.IP)
		safe.Outbound = alias("connection", e.Outbound)
		safe.Network = known(e.Network, "tcp", "udp", "dns")
		safe.DestinationPort = e.DestinationPort
		safe.Rule = supportRule(e, alias)
	case journal.Audit:
		safe.Actor = known(e.Actor, "user", "system", "watchdog", "dns")
		safe.Action = known(e.Action, "Start keenProxy", "Stop keenProxy", "Panel login",
			"Apply configuration", "Configuration rejected", "Configuration apply failed",
			"Configuration applied but not saved", "Device addresses updated", "Interception rules restored",
			"Interception rules not restored", "Direct DNS fallback", "Restore selected DNS route",
			"IP database updated", "IP database update failed", "DNS query blocked",
			"Switch to fallback", "No working connection", "Return to primary", "Return to higher priority")
		if safe.Action == "" {
			safe.Action = "Other audit event"
		}
		safe.Result = known(e.Result, "ok", "info", "warn", "error")
		safe.Target = supportAuditTarget(e.Target, alias)
		safe.Details = failureClass(e.Details)
	case journal.System:
		safe.Level = known(e.Level, "INFO", "WARN", "ERROR")
		safe.Source = known(e.Source, "keenproxy", "sing-box", "trusttunnel")
		safe.Message = systemEvent(e.Message)
	}
	return safe
}

func supportAuditTarget(target string, alias supportAlias) string {
	if section := known(target, "config", "core", "auth", "dns", "firewall", "geoip", "clients"); section != "" {
		return section
	}
	return alias("connection", target)
}

// supportRule keeps recognized rule families and uses an alias for everything else.
func supportRule(e journal.Entry, alias supportAlias) string {
	if e.Network == "dns" && e.Outbound == "block" {
		return "DNS blocked"
	}
	if group := supportGroupRule(e.Rule, alias); group != "" {
		return group
	}
	if label := known(e.Rule, "no rule matched — direct", "private address — always direct",
		"device excluded", "device in full tunnel", "device: no group matched — direct"); label != "" {
		return label
	}
	return alias("rule", e.Rule)
}

func supportGroupRule(rule string, alias supportAlias) string {
	prefix, tail, found := strings.Cut(rule, "group «")
	name, _, closed := strings.Cut(tail, "»")
	if !found || !closed {
		return ""
	}
	group := alias("group", name)
	if strings.HasSuffix(prefix, "DNS IP fallback: ") {
		return "DNS IP fallback: " + group
	}
	return group
}

func systemEvent(message string) string {
	for _, prefix := range []string{
		"keenProxy started", "stopping keenProxy", "configuration applied and saved",
		"new configuration failed to apply", "rollback also failed", "sing-box core restarted",
		"interception rules disappeared", "rule restoration", "rule update", "remove rules",
		"TrustTunnel started", "TrustTunnel configuration", "trusttunnel_client not found",
		"ARP unavailable", "IP database loaded", "IP database download failed",
		"upstream DNS did not respond", "system resolver did not respond",
		"scheduled update failed to start", "no connection in the chain is available",
		"connection is unavailable", "connection is available again", "selector did not switch",
	} {
		if strings.HasPrefix(message, prefix) {
			if failure := failureClass(message); failure != "" {
				return prefix + ": " + failure
			}
			return prefix
		}
	}
	if failure := failureClass(message); failure != "" {
		return failure
	}
	return "Other system event"
}

// failureClass derives a coarse reason without copying any portion of an error.
func failureClass(text string) string {
	text = strings.ToLower(text)
	for _, category := range []struct{ needle, label string }{
		{"timeout", "timeout"}, {"deadline exceeded", "timeout"},
		{"connection refused", "connection refused"}, {"connection reset", "connection reset"},
		{"certificate", "certificate error"}, {"permission denied", "permission denied"},
		{"no space left", "out of storage"}, {"network is unreachable", "network unreachable"},
		{"no such host", "DNS resolution failed"}, {"handshake", "handshake failed"},
	} {
		if strings.Contains(text, category.needle) {
			return category.label
		}
	}
	return ""
}
