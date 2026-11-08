package stats

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSampleProcessUsage(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "self"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("meminfo", "MemTotal: 262144 kB\nMemAvailable: 131072 kB\n")
	sample := func(router, guest, user, system, rss int) {
		t.Helper()
		write("stat", fmt.Sprintf("cpu %d 0 10 100 0 0 0 0 %d 0\n", router, guest))
		write("self/stat", fmt.Sprintf("7 (keen proxy ) test) S 0 0 0 0 0 0 0 0 0 0 %d %d 900 800 0 0 1 0 0 10000000 %d\n", user, system, rss))
	}
	var sampler systemSampler
	sample(100, 20, 15, 5, 20)
	sampler.sample(root)
	first := sampler.last.Process
	if first == nil || first.CPU != nil || first.RSSBytes != 20*uint64(os.Getpagesize()) {
		t.Fatalf("unexpected first sample: %+v", first)
	}
	sample(200, 60, 19, 6, 25)
	sampler.sample(root)
	second := sampler.last.Process
	if second == nil || second.CPU == nil || *second.CPU != 5 || second.RSSBytes != 25*uint64(os.Getpagesize()) {
		t.Fatalf("unexpected second sample: %+v", second)
	}
	if sampler.last.CPU != 100 || sampler.last.MemPct != 50 || sampler.last.MemMB != 256 || first.RSSBytes != 20*uint64(os.Getpagesize()) {
		t.Fatalf("router totals or earlier snapshot changed: %+v", sampler.last)
	}
	write("stat", "cpu invalid\n")
	sampler.sample(root)
	if got := sampler.last.Process; got == nil || got.CPU != nil || got.RSSBytes != second.RSSBytes {
		t.Fatalf("RSS should remain available when router counters are missing: %+v", got)
	}
	sample(250, 60, 20, 6, 25)
	sampler.sample(root)
	if sampler.last.Process.CPU != nil {
		t.Fatal("first valid sample after interruption must establish a new baseline")
	}
	sample(350, 60, 20, 6, 25)
	sampler.sample(root)
	if cpu := sampler.last.Process.CPU; cpu == nil || *cpu != 0 {
		t.Fatal("idle process should report a measured zero")
	}
	sample(450, 60, 5, 1, 25)
	sampler.sample(root)
	if sampler.last.Process.CPU != nil {
		t.Fatal("a decreasing process counter cannot establish usage for the interval")
	}
	sample(550, 60, 120, 2, 25)
	sampler.sample(root)
	if cpu := sampler.last.Process.CPU; cpu == nil || *cpu != 100 {
		t.Fatal("sampling skew must not produce usage greater than total router CPU time")
	}
	sampler.sample(root)
	if sampler.last.Process.CPU != nil {
		t.Fatal("unchanged router counters must not produce a percentage")
	}
	write("self/stat", "truncated")
	sampler.sample(root)
	if sampler.last.Process != nil {
		t.Fatal("invalid process counters must not expose a stale measurement")
	}
	sample(1, 0, 120, 2, 25)
	write("stat", "cpu 1 0 0 0\n")
	sampler.sample(root)
	sample(101, 0, 124, 3, 25)
	write("stat", "cpu 101 0 0 0\n")
	sampler.sample(root)
	if cpu := sampler.last.Process.CPU; cpu == nil || *cpu != 5 {
		t.Fatal("a nonzero baseline should work even after a reset")
	}
}

func TestReadProcessRejectsInvalidStat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stat")
	if ticks, rss, ok := readProcess(path); ok || ticks != 0 || rss != 0 {
		t.Fatal("a missing stat file returned a sample")
	}
	fields := strings.Fields("S 0 0 0 0 0 0 0 0 0 0 1 2 0 0 0 0 1 0 0 4096 1")
	for _, invalid := range []struct {
		name string
		stat string
	}{
		{"missing command delimiter", "42 invalid"},
		{"too few fields", "42 (short) S 0"},
		{"invalid user ticks", "42 (user) " + strings.Replace(strings.Join(fields, " "), "0 1 2", "0 x 2", 1)},
		{"invalid kernel ticks", "42 (kernel) " + strings.Replace(strings.Join(fields, " "), "0 1 2", "0 1 x", 1)},
		{"negative resident pages", "42 (rss) " + strings.Join(fields[:21], " ") + " -1"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(invalid.stat), 0600); err != nil {
				t.Fatal(err)
			}
			if ticks, rss, ok := readProcess(path); ok || ticks != 0 || rss != 0 {
				t.Fatal("invalid procfs data returned a sample")
			}
		})
	}
}
