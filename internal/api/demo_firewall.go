package api

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/journal"
)

// Both demo runtimes consume this synthetic fixture, including its raw details.
//
//go:embed demo_firewall.json
var demoFirewallJSON []byte

// demoFirewallAudit timestamps fresh copies of the shared recovery examples.
func demoFirewallAudit(now time.Time, fixture []byte) ([]journal.Entry, error) {
	var entries []journal.Entry
	if err := json.Unmarshal(fixture, &entries); err != nil {
		return nil, fmt.Errorf("invalid embedded firewall demo: %w", err)
	}
	for i := range entries {
		entries[i].Time = now.Add(-time.Duration(len(entries)-i) * time.Minute)
	}
	return entries, nil
}
