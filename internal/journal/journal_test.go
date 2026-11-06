package journal

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atrevido/keenetic-proxy/internal/model"
)

func TestFindPagesAndFilters(t *testing.T) {
	j := New(Options{Memory: 100})
	for i := 0; i < 30; i++ {
		out := "fi"
		if i%3 == 0 {
			out = "direct"
		}
		j.Add(Entry{Host: "h" + string(rune('a'+i%26)) + ".example", Outbound: out, Client: "192.168.7.10:5000" + string(rune('0'+i%10))})
	}
	p := j.Find(Query{Limit: 10}, Fields[Routes])
	if len(p.Entries) != 10 || !p.HasMore || p.Entries[0].ID != 30 {
		t.Fatalf("первая страница: %d записей, more=%v, первый id %d", len(p.Entries), p.HasMore, p.Entries[0].ID)
	}
	next := j.Find(Query{Limit: 10, Before: p.Entries[9].ID}, nil)
	if next.Entries[0].ID != 20 {
		t.Errorf("вторая страница начинается с %d", next.Entries[0].ID)
	}
	direct := j.Find(Query{Limit: 100, Filters: map[string]string{"outbound": "direct"}}, nil)
	if len(direct.Entries) != 10 {
		t.Errorf("direct: %d", len(direct.Entries))
	}
	// Device filters ignore the source port.
	if n := len(j.Find(Query{Limit: 100, Filters: map[string]string{"client": "192.168.7.10"}}, nil).Entries); n != 30 {
		t.Errorf("по устройству: %d", n)
	}
	if n := len(j.Find(Query{Limit: 100, Text: "HB.EXAMPLE"}, nil).Entries); n != 2 {
		t.Errorf("поиск: %d", n)
	}
	if strings.Join(p.Facets["outbound"], ",") != "direct,fi" || strings.Join(p.Facets["client"], ",") != "192.168.7.10" {
		t.Errorf("значения фильтров: %v", p.Facets)
	}
}

func TestMemoryLimit(t *testing.T) {
	j := New(Options{Memory: 8})
	for i := 0; i < 50; i++ {
		j.Add(Entry{Message: "x"})
	}
	p := j.Find(Query{Limit: 100}, nil)
	if len(p.Entries) > 8 || p.Entries[0].ID != 50 {
		t.Errorf("в памяти %d записей, последняя %d", len(p.Entries), p.Entries[0].ID)
	}
}

func TestDedup(t *testing.T) {
	j := New(Options{Dedup: true})
	for i := 0; i < 5; i++ {
		j.Add(Entry{Level: "WARN", Message: "правила пропали"})
	}
	j.Add(Entry{Level: "INFO", Message: "другое"})
	p := j.Find(Query{}, nil)
	if len(p.Entries) != 2 || p.Entries[1].Count != 5 {
		t.Errorf("повторы не схлопнулись: %+v", p.Entries)
	}
}

func TestFileRotationAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "j", "routes.jsonl")
	j := New(Options{File: path, MaxBytes: 2000})
	for i := 0; i < 60; i++ {
		j.Add(Entry{Host: "example.com", Outbound: "fi", Rule: "группа «Видео»"})
	}
	j.Close()
	// Restart preserves numbering and restores entries from disk.
	j2 := New(Options{File: path, MaxBytes: 2000})
	e := j2.Add(Entry{Host: "after.restart"})
	if e.ID != 61 {
		t.Errorf("id после перезапуска %d", e.ID)
	}
	if got := j2.Find(Query{Limit: 1000}, nil); len(got.Entries) < 10 {
		t.Errorf("с диска прочитано %d записей", len(got.Entries))
	}
	var buf bytes.Buffer
	if err := j2.Export(&buf, Routes, time.Now().Add(-time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "after.restart") || !strings.Contains(buf.String(), "группа «Видео»") {
		t.Errorf("выгрузка:\n%s", buf.String())
	}
}

func TestSubscribe(t *testing.T) {
	j := New(Options{})
	ch, cancel := j.Subscribe()
	defer cancel()
	j.Add(Entry{Message: "hello"})
	select {
	case e := <-ch:
		if e.Message != "hello" {
			t.Errorf("%+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("запись не пришла")
	}
}

const rulesYAML = `
lan: {interface: br0}
dns: {upstreams: ["1.1.1.1"]}
connections:
  - {id: fi, type: link, link: "socks5://user:s3cr3tpass@203.0.113.9:1080"}
groups:
  - {name: Видео, target: fi, entries: [youtube.com]}
  - {name: Steam напрямую, target: direct, entries: [steampowered.com]}
  - {name: Всё, кроме России, target: fi, countries: [RU], except: true}
networks:
  - {name: Дом, cidr: 192.168.7.0/24, mode: split}
  - {name: Гости, cidr: 10.1.30.0/24, mode: "off"}
`

func TestDescribeRule(t *testing.T) {
	cfg, err := model.Parse([]byte(strings.Replace(rulesYAML, "name: Всё, кроме России", `name: "Всё, кроме России"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	for raw, want := range map[string]string{
		"":                   "ни одно правило не подошло — напрямую",
		"ip_is_private=true": "частный адрес — всегда напрямую",
		"source_ip_cidr=192.168.7.0/24 rule_set=group-1":                "сеть «Дом»: группа «Steam напрямую»",
		"source_ip_cidr=192.168.7.0/24 && !(rule_set=geoip-ru)":         "сеть «Дом»: группа «Всё, кроме России» (все, кроме RU)",
		"rule_set=clients-split && rule_set=group-0":                    "устройство: группа «Видео»",
		"rule_set=clients-split":                                        "устройство: ни одна группа не подошла — напрямую",
		"rule_set=clients-off":                                          "устройство не участвует",
		"rule_set=clients-full-fi":                                      "устройство в полном туннеле",
		"source_ip_cidr=192.168.7.0/24":                                 "сеть «Дом»: ни одна группа не подошла — напрямую",
		"source_ip_cidr=10.1.30.0/24":                                   "сеть «Гости» не участвует",
		"source_ip_cidr=[10.1.30.0/24 192.168.7.0/24] rule_set=group-0": "сеть «Гости»: группа «Видео»",
	} {
		if got := DescribeRule(cfg, raw); got != want {
			t.Errorf("%q:\n  получили %q\n  ждали    %q", raw, got, want)
		}
	}
}

func TestMasker(t *testing.T) {
	cfg, err := model.Parse([]byte(strings.Replace(rulesYAML, "name: Всё, кроме России", `name: "Всё, кроме России"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	mask := Masker(cfg, "panel-token-123")
	got := mask("dial socks5://user:s3cr3tpass@203.0.113.9:1080 failed; pass s3cr3tpass; token panel-token-123")
	if strings.Contains(got, "s3cr3tpass") || strings.Contains(got, "panel-token-123") {
		t.Errorf("секрет остался: %s", got)
	}
}

func TestReadSingBox(t *testing.T) {
	j := New(Options{Dedup: true})
	in := "+0300 2026-11-05 23:24:50 WARN [3881452 5.1s] outbound/fi: dial tcp: i/o timeout\n" +
		"+0300 2026-11-05 23:24:51 DEBUG пропускается\n" +
		"ERROR router: rule-set geoip-ru: not found\n"
	var mirror bytes.Buffer
	ReadSingBox(strings.NewReader(in), j, &mirror)
	p := j.Find(Query{}, nil)
	if len(p.Entries) != 2 || p.Entries[1].Level != "WARN" || p.Entries[1].Message != "outbound/fi: dial tcp: i/o timeout" || p.Entries[0].Source != "sing-box" {
		t.Errorf("%+v", p.Entries)
	}
	if !strings.Contains(mirror.String(), "DEBUG") {
		t.Error("в stderr строки должны идти без изменений")
	}
}

func TestHandler(t *testing.T) {
	j := New(Options{})
	var out bytes.Buffer
	log := slog.New(NewHandler(slog.NewTextHandler(&out, nil), j)).With("component", "dns")
	log.Debug("не пишется")
	log.Warn("upstream не отвечает", "addr", "1.1.1.1:53")
	log.Handler().Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelError, "ошибка", 0))
	p := j.Find(Query{}, nil)
	if len(p.Entries) != 2 || p.Entries[1].Message != "upstream не отвечает component=dns addr=1.1.1.1:53" || p.Entries[0].Level != "ERROR" {
		t.Errorf("%+v", p.Entries)
	}
}

func TestClientFilterPreservesIPv6Identity(t *testing.T) {
	for _, value := range []string{"[2001:db8::1]:5000", "2001:db8::1", "[2001:db8::1]"} {
		e := Entry{Client: value}
		if got := e.field("client"); got != "2001:db8::1" {
			t.Fatalf("%s -> %s", value, got)
		}
	}
}
