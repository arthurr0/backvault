package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

func FS() fs.FS {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		return assets
	}
	return sub
}
