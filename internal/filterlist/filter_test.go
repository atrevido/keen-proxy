package filterlist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

func TestFormatsAndLastGood(t *testing.T) {
	p, err := Parse(strings.NewReader("# test\n0.0.0.0 ads.test tracker.test\n||bad.test^\n@@||safe.bad.test^\nsite.test/path\n"))
	if err != nil || len(p.Domains) != 3 || len(p.Allow) != 1 || p.Skipped != 1 {
		t.Fatal(p, err)
	}
	bad := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bad {
			w.Write([]byte("<html>broken</html>"))
			return
		}
		w.Write([]byte("ads.test\nbad.test\n@@||safe.bad.test^\n"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	m := New(dir, srv.Client())
	cfg := model.Filters{Sources: []model.FilterSource{{ID: "test", URL: srv.URL, Enabled: true}}, Exceptions: []string{"except.bad.test"}}
	m.Configure(cfg)
	if err := m.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if m.Match("sub.ads.test.") == "" || m.Match("safe.bad.test") != "" || m.Match("except.bad.test") != "" {
		t.Fatal("match/exception")
	}
	bad = true
	if err := m.Refresh(context.Background(), false); err == nil {
		t.Fatal("bad update accepted")
	}
	if m.Match("ads.test") == "" {
		t.Fatal("last good lost")
	}
	restart := New(dir, srv.Client())
	restart.Configure(cfg)
	if restart.Match("ads.test") == "" {
		t.Fatal("snapshot lost at restart")
	}
	cfg.Sources[0].Enabled = false
	m.Configure(cfg)
	if m.Match("ads.test") != "" {
		t.Fatal("disabled list active")
	}
}

func TestCosmeticRuleDoesNotBlockWholeSite(t *testing.T) {
	p, err := Parse(strings.NewReader("ads.test\nexample.org##.sponsor\nexample.net#@#.ad\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Domains) != 1 || p.Skipped != 2 {
		t.Fatal(p)
	}
}
