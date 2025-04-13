package assets

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed *
var Assets embed.FS

// GetFileSystem returns a http.FileSystem that serves the embedded assets
func GetFileSystem() http.FileSystem {
	fsys, err := fs.Sub(Assets, ".")
	if err != nil {
		panic(err)
	}
	return http.FS(fsys)
}