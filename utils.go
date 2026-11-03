package basefs

import (
	"github.com/absfs/absfs"
)

// Unwrap checks if `fs` is a `*basefs.FileSystem` or `*basefs.SymlinkFileSystem`
// and if so returns the underlying `absfs.FileSystem`, otherwise it returns `fs`
func Unwrap(fs absfs.FileSystem) absfs.FileSystem {
	switch bfs := fs.(type) {
	case *SymlinkFileSystem:
		return bfs.sfs
	case *FileSystem:
		return bfs.fs
	}
	return fs
}

// Prefix checks if `fs` is a `*basefs.FileSystem` or `*basefs.SymlinkFileSystem`
// and if so returns the prefix (the cleaned root directory in the underlying
// filesystem), otherwise it returns an empty string
func Prefix(fs absfs.FileSystem) string {
	switch bfs := fs.(type) {
	case *SymlinkFileSystem:
		return bfs.prefix
	case *FileSystem:
		return bfs.prefix
	}
	return ""
}
