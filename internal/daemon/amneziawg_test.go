package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ordo-systems/keen-proxy/internal/model"
)

const testAWGProfile = `[Interface]
PrivateKey = IBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBA=
Address = 198.18.0.3/32
[Peer]
PublicKey = EBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBA=
AllowedIPs = 0.0.0.0/0
Endpoint = 192.0.2.10:51820
`

func TestFirstAmneziaWGApplyStagesWithoutTouchingLiveConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	active := &model.Config{}
	active.Defaults()
	raw, err := active.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	d := New(Options{ConfigPath: path, RuleSetDir: filepath.Join(dir, "rules")})
	d.cfg = active
	draft := active.Clone()
	draft.Connections = []model.Connection{{
		ID: "awg", Name: "AWG", Type: "amneziawg", AmneziaWG: &model.AmneziaWG{Profile: testAWGProfile},
	}}
	if err := d.Apply(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	if len(d.Config().Connections) != 0 || len(d.EditableConfig().Connections) != 1 {
		t.Fatal("AWG must appear in the saved draft only until restart")
	}
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != string(raw) {
		t.Fatal("the active startup file must be preserved for rollback")
	}
	pending, err := model.Load(path + ".pending-awg")
	if err != nil || len(pending.Connections) != 1 {
		t.Fatal("the pending AWG configuration must survive process restart")
	}
	panel := active.Clone()
	panel.Connections = []model.Connection{}
	if err := d.Apply(context.Background(), panel); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".pending-awg"); !os.IsNotExist(err) {
		t.Fatal("reapplying the active draft must cancel the staged draft")
	}
}

// A same-port profile replacement must leave the live sing-box instance intact.
func TestAmneziaWGReplacementKeepsEngine(t *testing.T) {
	dir := t.TempDir()
	cfg := &model.Config{Connections: []model.Connection{{
		ID: "awg", Name: "AWG", Type: model.ConnAmneziaWG, AmneziaWG: &model.AmneziaWG{Profile: testAWGProfile},
	}}}
	cfg.Defaults()
	path := filepath.Join(dir, "config.yaml")
	if raw, err := cfg.Marshal(); err != nil {
		t.Fatal(err)
	} else if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "fixture")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	d := New(Options{ConfigPath: path, RuleSetDir: filepath.Join(dir, "rules"), AWGBinary: binary})
	d.cfg = cfg
	t.Cleanup(func() { d.replaceAWG(nil, nil); d.eng.Close() })
	if _, err := d.eng.Apply([]byte(`{"outbounds":[{"type":"direct","tag":"baseline"}]}`)); err != nil {
		t.Fatal(err)
	}
	_, live, ok := d.eng.Outbound("baseline")
	if !ok {
		t.Fatal("baseline engine did not start")
	}
	draft := cfg.Clone()
	draft.Connections[0].AmneziaWG.Profile = strings.Replace(testAWGProfile, "192.0.2.10", "192.0.2.11", 1)
	if err := d.Apply(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	_, after, ok := d.eng.Outbound("baseline")
	if !ok || live != after || live.Err() != nil {
		t.Fatal("profile replacement restarted the engine")
	}
	if p := d.awg["awg"]; p == nil || p.client.SocksPort != cfg.Connections[0].AmneziaWG.SocksPort {
		t.Fatal("replacement did not retain the SOCKS endpoint")
	}
	if d.pendingAWG != nil {
		t.Fatal("same-port replacement must be immediate")
	}
}

func TestAmneziaWGProbeRefusesActiveWireGuardIdentity(t *testing.T) {
	d := New(Options{RuleSetDir: t.TempDir(), AWGBinary: "/must-not-execute"})
	d.cfg = &model.Config{Connections: []model.Connection{{
		ID: "live", Type: model.ConnWireGuard, WireGuard: &model.WireGuard{
			PrivateKey: "IBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBA=",
		},
	}}}
	if err := d.ProbeAmneziaWG(context.Background(), testAWGProfile); err == nil || !strings.Contains(err.Error(), "active WireGuard client identity") {
		t.Fatalf("expected identity conflict before subprocess start, got %v", err)
	}
}

func TestAmneziaWGProbeAllowsADifferentWireGuardIdentity(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "probe-fixture")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n[ \"$1\" = -config ] && [ -s \"$2\" ] && [ \"$3\" = -probe ]\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	d := New(Options{RuleSetDir: filepath.Join(dir, "rules"), AWGBinary: binary})
	d.cfg = &model.Config{Connections: []model.Connection{{
		ID: "live", Type: model.ConnWireGuard, WireGuard: &model.WireGuard{
			PrivateKey: "EBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBA=",
		},
	}}}
	if err := d.ProbeAmneziaWG(context.Background(), testAWGProfile); err != nil {
		t.Fatal(err)
	}
}
