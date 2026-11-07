package daemon

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ordo-systems/keen-proxy/internal/model"
)

func TestApplyGenerationCancelsDomainResolution(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	received := make(chan struct{})
	go func() { buf := make([]byte, 1024); pc.ReadFrom(buf); close(received) }()
	d := New(Options{RuleSetDir: t.TempDir()})
	cfg := &model.Config{}
	cfg.Defaults()
	cfg.DNS.Upstreams = []string{"udp://" + pc.LocalAddr().String()}
	cfg.DNS.Fallback = ""
	cfg.Groups = []model.Group{{Target: "direct", Entries: []string{"one.test"}}}
	d.live.Store(cfg)
	done := make(chan error, 1)
	go func() { _, err := d.RefreshDomains(context.Background(), []string{"one.test"}, false); done <- err }()
	<-received
	d.resetDomainGeneration()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stale resolution succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("old DNS policy kept running")
	}
}
