package daemon

import (
	"context"
	"testing"

	"github.com/atrevido/keenetic-proxy/internal/model"
	"github.com/miekg/dns"
)

func TestFirefoxCanaryNXDOMAIN(t *testing.T) {
	d := New(Options{RuleSetDir: t.TempDir()})
	d.cfg = &model.Config{}
	d.cfg.Defaults()
	d.cfg.DNS.Listen = "127.0.0.1:0"
	d.cfg.DNS.FirefoxCanary = true
	if err := d.startDNS(); err != nil {
		t.Fatal(err)
	}
	defer d.dns.Close()
	for _, typ := range []uint16{dns.TypeA, dns.TypeAAAA} {
		q := new(dns.Msg)
		q.SetQuestion("use-application-dns.net.", typ)
		r := d.dns.Resolve(context.Background(), q)
		if r.Rcode != dns.RcodeNameError || len(r.Answer) != 0 {
			t.Fatal(r)
		}
	}
}
