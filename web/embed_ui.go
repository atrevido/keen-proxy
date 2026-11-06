//go:build ui

package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS serves the frontend bundle produced in web/dist by make web.
func FS() fs.FS {
	sub, _ := fs.Sub(dist, "dist")
	return sub
}
