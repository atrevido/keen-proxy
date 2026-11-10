package journal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFirewallProbeReloadsFromPersistedJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	record := `{"id":1,"actor":"watchdog","target":"firewall","firewall":{"outcome":"verify_failed","before":{"table":"nat","chain":"PREROUTING","target":"KEENPROXY_NAT","exit_code":1,"reason":"check_failed","error":"missing jump"},"after":{"table":"mangle","chain":"PREROUTING","target":"KEENPROXY_MANGLE","exit_code":-1,"reason":"execution_failed"}}}`
	if err := os.WriteFile(path, []byte(record+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	j := New(Options{File: path})
	defer j.Close()
	got := j.Find(Query{}, Fields[Audit]).Entries
	if len(got) != 1 || got[0].Firewall == nil {
		t.Fatalf("persisted firewall event missing: %+v", got)
	}
	f := got[0].Firewall
	before := FirewallCheck{Table: "nat", Chain: "PREROUTING", Target: "KEENPROXY_NAT", ExitCode: 1, Reason: "check_failed", Error: "missing jump"}
	after := FirewallCheck{Table: "mangle", Chain: "PREROUTING", Target: "KEENPROXY_MANGLE", ExitCode: -1, Reason: "execution_failed"}
	if f.Outcome != "verify_failed" || f.Before == nil || *f.Before != before || f.After == nil || *f.After != after {
		t.Fatalf("persisted probe data changed: %+v", f)
	}
}
