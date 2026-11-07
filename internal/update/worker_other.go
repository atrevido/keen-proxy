//go:build !linux

package update

import "log/slog"

// NewManaged reports no managed updater outside Entware Linux.
func NewManaged(string, *slog.Logger) *Manager { return nil }

func (*Manager) job() *Job { return nil }
