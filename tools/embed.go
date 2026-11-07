// Package tools exposes the release installer to the managed Entware updater.
package tools

import _ "embed"

// Bootstrap is the same installer used for command-line release transactions.
//
//go:embed bootstrap.sh
var Bootstrap []byte
