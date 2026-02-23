package webui

import (
	"embed"
	"io/fs"
)

//go:embed dist
var distFS embed.FS

func DistFS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
