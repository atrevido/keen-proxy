package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ordo-systems/keen-proxy/internal/journal"
)

func TestSupportBundleFirewallDiagnostics(t *testing.T) {
	s, d := newServer()
	raw := `{"actor":"watchdog","action":"Interception rules not restored","target":"firewall","result":"error","details":"private-detail","firewall":{"trigger":"netfilter_hook","duration_ms":56,"outcome":"verify_failed","before":{"table":"nat","chain":"PREROUTING","target":"KEENPROXY_TCP","exit_code":1,"reason":"check_failed","error":"private-host: missing rule"},"after":{"table":"mangle","chain":"PREROUTING","target":"KEENPROXY_UDP","exit_code":4,"reason":"command_failed","error":"permission denied private-path"},"apply_error":"private-password","unknown":"private-value"}}`
	var entry journal.Entry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/audit.jsonl"
	d.journals[journal.Audit] = journal.New(journal.Options{File: path})
	d.journals[journal.Audit].Add(entry)
	d.journals[journal.Audit].Close()
	d.journals[journal.Audit] = journal.New(journal.Options{File: path})
	defer d.journals[journal.Audit].Close()
	w := do(t, s.Handler(), "GET", bundlePath, "", s.Token, lan)
	if w.Code != 200 {
		t.Fatalf("bundle status = %d", w.Code)
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name != "journals/audit.jsonl" {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "private") {
			t.Fatalf("private diagnostic data leaked: %s", data)
		}
		var got struct {
			Firewall *struct {
				Outcome    string
				Trigger    string
				DurationMS *int64 `json:"duration_ms"`
				Before     struct {
					Table, Chain, Target, Reason, Error string
					ExitCode                            int `json:"exit_code"`
				}
				After struct {
					Target, Error string
					ExitCode      int `json:"exit_code"`
				}
			}
		}
		if err := json.Unmarshal(bytes.TrimSpace(data), &got); err != nil {
			t.Fatal(err)
		}
		if got.Firewall == nil {
			t.Fatalf("diagnostics omitted from ZIP: %s", data)
		}
		fw := got.Firewall
		if fw.Trigger != "netfilter_hook" || fw.DurationMS == nil || *fw.DurationMS != 56 || fw.Outcome != "verify_failed" || fw.Before.Table != "nat" || fw.Before.Chain != "PREROUTING" || fw.Before.Target != "KEENPROXY_TCP" || fw.Before.ExitCode != 1 || fw.Before.Reason != "check_failed" || fw.After.Target != "KEENPROXY_UDP" || fw.After.ExitCode != 4 || fw.After.Error != "permission denied" {
			t.Fatalf("diagnostics changed during export: %s", data)
		}
		return
	}
	t.Fatal("audit journal missing")
}

// Old or tampered journal files must never bypass support-bundle redaction.
func TestSupportFirewallRejectsUntrustedMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, payload         string
		wantEvent, wantBefore bool
	}{
		{"legacy", `{}`, false, false},
		{"wrong actor", `{"actor":"private","target":"firewall","firewall":{"outcome":"restored"}}`, false, false},
		{"wrong target", `{"actor":"watchdog","target":"private","firewall":{"outcome":"restored"}}`, false, false},
		{"unknown outcome", `{"actor":"watchdog","target":"firewall","firewall":{"outcome":"private"}}`, false, false},
		{"unknown table", `{"actor":"watchdog","target":"firewall","firewall":{"outcome":"restored","before":{"table":"private","chain":"PREROUTING","target":"KEENPROXY_TCP"}}}`, true, false},
		{"unknown chain", `{"actor":"watchdog","target":"firewall","firewall":{"outcome":"restored","before":{"table":"nat","chain":"private","target":"KEENPROXY_TCP"}}}`, true, false},
		{"unknown target", `{"actor":"watchdog","target":"firewall","firewall":{"outcome":"restored","before":{"table":"nat","chain":"PREROUTING","target":"private"}}}`, true, false},
		{"wrong combination", `{"actor":"watchdog","target":"firewall","firewall":{"outcome":"restored","before":{"table":"filter","chain":"PREROUTING","target":"KEENPROXY_TCP"}}}`, true, false},
		{"untrusted reason", `{"actor":"watchdog","target":"firewall","firewall":{"outcome":"restored","before":{"table":"nat","chain":"PREROUTING","target":"KEENPROXY_TCP","reason":"private","error":"private","exit_code":999},"apply_error":"private"}}`, true, true},
		{"negative exit", `{"actor":"watchdog","target":"firewall","firewall":{"outcome":"restored","before":{"table":"nat","chain":"PREROUTING","target":"KEENPROXY_TCP","exit_code":-50}}}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var e journal.Entry
			if err := json.Unmarshal([]byte(tc.payload), &e); err != nil {
				t.Fatal(err)
			}
			got := supportEntry(journal.Audit, e, func(_, _ string) string { return "alias" })
			data, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "private") {
				t.Fatalf("untrusted data retained: %s", data)
			}
			if (got.Firewall != nil) != tc.wantEvent {
				t.Fatalf("unexpected diagnostic event: %s", data)
			}
			if got.Firewall != nil && (got.Firewall.Before != nil) != tc.wantBefore {
				t.Fatalf("unexpected check: %s", data)
			}
			if tc.wantBefore && (got.Firewall.Before.ExitCode != -1 || got.Firewall.Before.Reason != "" || got.Firewall.Before.Error != "") {
				t.Fatalf("metadata was not sanitized: %s", data)
			}
		})
	}
}

// Normalize out-of-range metadata while preserving every valid process exit.
func TestSupportFirewallExitCodeBounds(t *testing.T) {
	for _, tc := range []struct{ input, want int }{
		{-2, -1}, {-1, -1}, {0, 0}, {1, 1}, {255, 255}, {256, -1},
	} {
		t.Run(fmt.Sprint(tc.input), func(t *testing.T) {
			raw := fmt.Sprintf(`{"actor":"watchdog","target":"firewall","firewall":{"outcome":"restored","before":{"table":"nat","chain":"PREROUTING","target":"KEENPROXY_TCP","exit_code":%d}}}`, tc.input)
			var entry journal.Entry
			if err := json.Unmarshal([]byte(raw), &entry); err != nil {
				t.Fatal(err)
			}
			got := supportFirewall(entry)
			if got == nil || got.Before == nil || got.Before.ExitCode != tc.want {
				t.Fatalf("exit %d not normalized to %d: %+v", tc.input, tc.want, got)
			}
		})
	}
}

func TestSupportFirewallTriggerAndTiming(t *testing.T) {
	for _, tc := range []struct{ name, metadata, want string }{
		{"legacy", "", `"outcome":"restored"`},
		{"hook", `,"trigger":"netfilter_hook","duration_ms":0`, `"trigger":"netfilter_hook"`},
		{"poll", `,"trigger":"watchdog","duration_ms":42`, `"trigger":"watchdog"`},
		{"unknown", `,"trigger":"private-source","duration_ms":-1`, `"outcome":"restored"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var e journal.Entry
			raw := `{"actor":"watchdog","target":"firewall","firewall":{"outcome":"restored"` + tc.metadata + `}}`
			if err := json.Unmarshal([]byte(raw), &e); err != nil {
				t.Fatal(err)
			}
			got := supportEntry(journal.Audit, e, func(_, _ string) string { return "alias" })
			data, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			text := string(data)
			if !strings.Contains(text, tc.want) || strings.Contains(text, "private-source") || strings.Contains(text, `"duration_ms":-1`) {
				t.Fatalf("invalid recovery metadata: %s", data)
			}
			if tc.name == "hook" && !strings.Contains(text, `"duration_ms":0`) {
				t.Fatalf("zero duration lost: %s", data)
			}
			if tc.name == "poll" && !strings.Contains(text, `"duration_ms":42`) {
				t.Fatalf("duration lost: %s", data)
			}
			if (tc.name == "legacy" || tc.name == "unknown") && (strings.Contains(text, `"trigger"`) || strings.Contains(text, `"duration_ms"`)) {
				t.Fatalf("missing/invalid metadata invented: %s", data)
			}
		})
	}
}
