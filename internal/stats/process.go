package stats

import (
	"os"
	"strconv"
	"strings"
)

// processSampler keeps the process and router counters from the same interval.
type processSampler struct {
	ticks, total uint64
}

func (s *processSampler) sample(path string, total uint64) *ProcessUsage {
	ticks, rss, ok := readProcess(path)
	before := *s
	*s = processSampler{ticks, total}
	if !ok {
		*s = processSampler{}
		return nil
	}
	result := &ProcessUsage{RSSBytes: rss}
	if before.total > 0 && total > before.total && ticks >= before.ticks {
		elapsed := total - before.total
		cpu := round1(100 * float64(min(ticks-before.ticks, elapsed)) / float64(elapsed))
		result.CPU = &cpu
	}
	return result
}

// readProcess reads Linux stat fields utime, stime and rss for this process only.
func readProcess(path string) (ticks, rss uint64, ok bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, false
	}
	line := string(raw)
	end := strings.LastIndexByte(line, ')') // The command name may contain spaces and parentheses.
	if end < 0 {
		return 0, 0, false
	}
	fields := strings.Fields(line[end+1:]) // Starts at field 3 (state).
	if len(fields) < 22 {
		return 0, 0, false
	}
	user, userErr := strconv.ParseUint(fields[11], 10, 64)
	kernel, kernelErr := strconv.ParseUint(fields[12], 10, 64)
	pages, pagesErr := strconv.ParseUint(fields[21], 10, 64)
	if userErr != nil || kernelErr != nil || pagesErr != nil {
		return 0, 0, false
	}
	return user + kernel, pages * uint64(os.Getpagesize()), true
}
