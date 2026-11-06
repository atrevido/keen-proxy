//go:build !linux

package engine

import "strconv"

// PipeLogPath names an inherited descriptor for sing-box logging on non-Linux hosts.
func PipeLogPath(fd uintptr) string { return "/dev/fd/" + strconv.FormatUint(uint64(fd), 10) }
