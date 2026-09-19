package docs

import (
	"embed"
	"io/fs"
)

//go:embed *.md
//go:embed sources
//go:embed destinations
//go:embed images
var files embed.FS

func FS() fs.FS {
	return files
}
