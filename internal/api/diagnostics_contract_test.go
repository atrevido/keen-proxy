package api

import (
	"context"
	"testing"
	"time"

	"github.com/atrevido/keenetic-proxy/internal/daemon"
	"github.com/atrevido/keenetic-proxy/internal/model"
)

// diagnosticContextProbe observes the RPC boundary without opening network connections.
type diagnosticContextProbe struct {
	*fakeDaemon
	deadline  bool
	remaining time.Duration
}

func (p *diagnosticContextProbe) DiagnosticChecks(ctx context.Context, id string) (daemon.DiagnosticReport, error) {
	deadline, ok := ctx.Deadline()
	p.deadline, p.remaining = ok, time.Until(deadline)
	return daemon.DiagnosticReport{Connection: id}, nil
}

func TestDiagnosticRPCProvidesUsableBoundedContext(t *testing.T) {
	s, fake := newServer()
	fake.cfg.Connections = []model.Connection{{ID: "one", Type: model.ConnLink}}
	probe := &diagnosticContextProbe{fakeDaemon: fake}
	s.D = probe
	w := do(t, s.Handler(), "POST", "/api/diagnostics/check", `{"connection":"one"}`, "secret", lan)
	if w.Code != 200 || !probe.deadline || probe.remaining <= time.Second || probe.remaining > 25*time.Second {
		t.Fatalf("unusable diagnostic budget: status=%d deadline=%v remaining=%s", w.Code, probe.deadline, probe.remaining)
	}
}

func TestDemoDiagnosticResultsAreExplicitSyntheticSuccesses(t *testing.T) {
	d := &Demo{}
	report, err := d.DiagnosticChecks(context.Background(), "one")
	if err != nil || !report.Demo || report.Connection != "one" || len(report.Results) != 3 {
		t.Fatalf("invalid demo report: %+v, %v", report, err)
	}
	for i, kind := range []string{"http", "https", "transfer"} {
		r := report.Results[i]
		if r.Kind != kind || !r.OK || r.Error != "" || r.Checked.IsZero() {
			t.Fatalf("invalid synthetic %s result: %+v", kind, r)
		}
	}
	if report.Results[2].Bytes != 65536 {
		t.Fatal("demo transfer does not represent the advertised byte count")
	}
}
