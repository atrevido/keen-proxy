package daemon

import (
	"encoding/json"
	"github.com/atrevido/keenetic-proxy/internal/journal"
	"github.com/atrevido/keenetic-proxy/internal/model"
	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
	"net/netip"
	"testing"
)

func TestRouteRecordsDestinationPort(t *testing.T) {
	j := journal.New(journal.Options{})
	d := &Daemon{journals: journal.Set{journal.Routes: j}}
	d.live.Store(&model.Config{})
	d.onRoute(adapter.InboundContext{Destination: M.SocksaddrFrom(netip.MustParseAddr("203.0.113.1"), 443)}, nil, nil)
	p := j.Find(journal.Query{}, nil)
	raw, _ := json.Marshal(p.Entries[0])
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if got["destination_port"] != float64(443) {
		t.Fatalf("destination port lost: %s", raw)
	}
}
