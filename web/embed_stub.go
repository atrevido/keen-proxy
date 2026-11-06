//go:build !ui

package web

import (
	"embed"
	"io/fs"
)

//go:embed stub
var stub embed.FS

// FS serves a placeholder without the ui build tag so tests and vet need no frontend build.
func FS() fs.FS {
	sub, _ := fs.Sub(stub, "stub")
	return sub
}
