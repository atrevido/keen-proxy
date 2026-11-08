package stats

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// System contains router resource usage displayed in the panel.
type System struct {
	CPU     float64       `json:"cpu"`     // %
	MemPct  float64       `json:"mem_pct"` // Used memory percentage.
	MemMB   int           `json:"mem_mb"`  // Total memory in MiB.
	Process *ProcessUsage `json:"process,omitempty"`
}

// ProcessUsage covers the daemon and its embedded engine. CPU is absent until
// two consecutive samples can be compared against aggregate router CPU time.
type ProcessUsage struct {
	CPU      *float64 `json:"cpu,omitempty"`
	RSSBytes uint64   `json:"rss_bytes"`
}

// systemSampler derives CPU load from successive procfs counter samples.
type systemSampler struct {
	lastIdle, lastTotal uint64
	process             processSampler
	last                System
}

func (s *systemSampler) sample(root string) {
	idle, total, ok := readCPU(filepath.Join(root, "stat"))
	if ok {
		if s.lastTotal > 0 && total > s.lastTotal && idle >= s.lastIdle {
			elapsed := total - s.lastTotal
			busy := elapsed - min(elapsed, idle-s.lastIdle)
			s.last.CPU = round1(float64(busy) / float64(elapsed) * 100)
		}
		s.lastIdle, s.lastTotal = idle, total
	}
	s.last.Process = s.process.sample(filepath.Join(root, "self", "stat"), total)
	if total, avail, ok := readMem(filepath.Join(root, "meminfo")); ok && total > 0 {
		s.last.MemPct = round1(float64(total-avail) / float64(total) * 100)
		s.last.MemMB = int(total / 1024)
	}
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

// readCPU reads aggregate procfs CPU counters, counting both idle and iowait as idle time.
func readCPU(path string) (idle, total uint64, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, 0, false
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	// Guest time is already included in user and nice.
	for i, v := range fields[1:min(len(fields), 9)] {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += n
		if i == 3 || i == 4 { // idle + iowait
			idle += n
		}
	}
	return idle, total, true
}

// readMem reads MemTotal and MemAvailable in procfs KiB units; missing totals mark the sample invalid.
func readMem(path string) (total, avail uint64, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, found := strings.Cut(sc.Text(), ":")
		if !found {
			continue
		}
		n, _ := strconv.ParseUint(strings.Fields(v)[0], 10, 64)
		switch k {
		case "MemTotal":
			total = n
		case "MemAvailable":
			avail = n
		}
	}
	return total, avail, total > 0
}
