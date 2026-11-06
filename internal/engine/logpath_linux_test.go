//go:build linux

package engine

import (
	"strings"
	"testing"
)

func TestLinuxPipeLogPathDoesNotRequireDevFD(t *testing.T) {
	if p := PipeLogPath(6); p != "/proc/self/fd/6" || strings.HasPrefix(p, "/dev/") {
		t.Fatal(p)
	}
}
