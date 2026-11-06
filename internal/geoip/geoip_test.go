package geoip_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/atrevido/keenetic-proxy/internal/geoip"
	"github.com/atrevido/keenetic-proxy/internal/geoip/geoiptest"
)

var testNets = map[string]string{
	"77.88.0.0/18":     "RU",
	"5.255.255.0/24":   "RU",
	"8.8.8.0/24":       "US",
	"2a02:6b8::/32":    "RU",
	"185.199.108.0/22": "NL",
}

func testDB(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.mmdb")
	if err := geoiptest.WriteTestDB(p, testNets); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLookupAndPrefixes(t *testing.T) {
	db, err := Open(testDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for ip, want := range map[string]string{"77.88.8.8": "RU", "8.8.8.8": "US", "1.1.1.1": "", "::ffff:5.255.255.242": "RU", "2a02:6b8::1": "RU"} {
		if got := db.Country(netip.MustParseAddr(ip)); got != want {
			t.Errorf("Country(%s) = %q, ждали %q", ip, got, want)
		}
	}
	p, err := db.Prefixes([]string{"ru", "NL"})
	if err != nil {
		t.Fatal(err)
	}
	var ru []string
	for _, x := range p["RU"] {
		ru = append(ru, x.String())
	}
	if strings.Join(ru, ",") != "5.255.255.0/24,77.88.0.0/18,2a02:6b8::/32" || len(p["NL"]) != 1 || p["US"] != nil {
		t.Errorf("Prefixes: RU=%v NL=%v US=%v", ru, p["NL"], p["US"])
	}
}

func TestDownloadFallsBackToPreviousMonth(t *testing.T) {
	raw, _ := os.ReadFile(testDB(t))
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write(raw)
	w.Close()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 { // This month's database is not yet available.
			http.NotFound(rw, r)
			return
		}
		rw.Write(gz.Bytes())
	}))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "sub", "country.mmdb")
	if err := Download(context.Background(), srv.Client(), srv.URL+"/db-{month}.mmdb.gz", dst); err != nil {
		t.Fatal(err)
	}
	if hits != 2 || NeedsUpdate(dst) {
		t.Errorf("hits=%d, NeedsUpdate=%v", hits, NeedsUpdate(dst))
	}
}

func TestDownloadRejectsGarbage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.Write([]byte("<html>ошибка</html>")) }))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "country.mmdb")
	if err := Download(context.Background(), srv.Client(), srv.URL+"/x-{month}.mmdb", dst); err == nil {
		t.Fatal("мусор не должен приниматься за базу")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("файл базы не должен появиться")
	}
}

func TestFlag(t *testing.T) {
	if Flag("ru") != "🇷🇺" || Flag("") != "" {
		t.Errorf("Flag: %q", Flag("ru"))
	}
}
