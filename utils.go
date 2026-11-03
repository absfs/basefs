package basefs

import (
	"github.com/absfs/absfs"
)

// Unwrap checks if `fs` is a `*basefs.FileSystem` or `*basefs.SymlinkFileSystem`
// and if so returns the underlying `absfs.FileSystem`, otherwise it returns `fs`
//
// It returns the filesystem given to NewFS or NewFileSystem even when basefs
// operates through a confined view of it (see Confined), so operations on
// the result are not confined to the root.
func Unwrap(fs absfs.FileSystem) absfs.FileSystem {
	switch bfs := fs.(type) {
	case *SymlinkFileSystem:
		return bfs.under
	case *FileSystem:
		return bfs.under
	}
	return fs
}

// Confined reports whether `fs` is a basefs filesystem whose underlying
// filesystem has confined itself to the root, so that the host cannot follow
// a symlink out of it. That is the case over osfs built with Go 1.25 or
// later on a Unix system, where basefs operates through an osfs RootFS.
// Otherwise basefs constrains only the names it is given; see the package
// documentation.
func Confined(fs absfs.FileSystem) bool {
	switch bfs := fs.(type) {
	case *SymlinkFileSystem:
		return bfs.confined != nil
	case *FileSystem:
		return bfs.confined != nil
	}
	return false
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
