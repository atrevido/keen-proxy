//go:build linux

package engine

import "strconv"

// PipeLogPath works on Entware routers whose /dev does not provide /dev/fd.
func PipeLogPath(fd uintptr) string { return "/proc/self/fd/" + strconv.FormatUint(uint64(fd), 10) }
