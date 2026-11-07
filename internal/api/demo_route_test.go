package api

import (
	"github.com/ordo-systems/keen-proxy/internal/journal"
	"testing"
	"time"
)

func TestDemoRouteIncludesDestinationPort(t *testing.T) {
	_, fake := newServer()
	d := &Demo{cfg: fake.cfg, journals: journal.NewSet("")}
	d.addDemoRoute(time.Unix(1, 0))
	entries := d.Journal(journal.Routes).Find(journal.Query{}, nil).Entries
	if len(entries) != 1 || entries[0].DestinationPort != 443 {
		t.Fatalf("missing demo destination port: %+v", entries)
	}
}

func TestDemoRouteIncludesRepeatedConnection(t *testing.T) {
	_, fake := newServer()
	d := &Demo{cfg: fake.cfg, journals: journal.NewSet("")}
	d.addDemoRoute(time.Unix(3, 0))
	entries := d.Journal(journal.Routes).Find(journal.Query{}, nil).Entries
	if len(entries) != 2 || entries[0].Host != entries[1].Host || entries[0].Outbound != entries[1].Outbound || entries[0].Client == entries[1].Client {
		t.Fatalf("missing repeated demo connection: %+v", entries)
	}
}
