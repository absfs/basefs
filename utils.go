package basefs

import (
	"github.com/absfs/absfs"
)

// Unwrap checks if `fs` is a `*basefs.FileSystem` and if so returns the
// underlying `absfs.FileSystem`, otherwise it returns `fs`
func Unwrap(fs absfs.FileSystem) absfs.FileSystem {
	bfs, ok := fs.(*FileSystem)
	if ok {
		return bfs.fs
	}
	return fs
}

// Prefix checks if `fs` is a `*basefs.FileSystem` and if so returns the prefix.
// otherwise it returns an empty string
func Prefix(fs absfs.FileSystem) string {
	bfs, ok := fs.(*FileSystem)
	if ok {
		return bfs.prefix
	}
	return ""
}
