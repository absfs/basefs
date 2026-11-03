// Package basefs re-roots an absfs filesystem at one of its directories.
//
// A basefs filesystem presents a Unix-style virtual namespace whose "/" is a
// directory (the base, or root) of an underlying absfs.FileSystem. Every name
// a caller passes in is resolved lexically: it is joined to the current
// working directory if relative, cleaned, and rejected with ErrPathEscapes if
// its ".." elements would climb above the root. Only then is it joined to the
// root to form the path given to the underlying filesystem, and that path is
// checked once more to lie on or below the root on a path-separator boundary.
// A root of /d/x therefore never reaches a sibling such as /d/xfoo.
//
// On Windows, backslashes in names are treated as separators, and names that
// carry a volume (C:\x, c:x, \\server\share, \\?\C:\x), contain a colon
// (f:stream), or have an element that is a reserved device name (CON, NUL,
// ...) are rejected with ErrPathEscapes.
//
// What basefs guarantees is limited to names: it never hands the underlying
// filesystem a path outside the root. It does not control what the
// underlying filesystem does with that path. In particular, when the
// underlying filesystem follows symbolic links (as osfs does, through the
// host OS), a link inside the root whose target lies outside it is followed
// out of the root.
//
// Links created through Symlink cannot be made to do that. Their targets are
// checked against the root and stored without any ".." (see Symlink), so
// neither a later Rename of the link or of a directory above it, nor a
// symlinked directory it is created beneath, can make them point outside
// the root. Readlink never
// reports a host path outside the root. A link placed by another process,
// however, is followed by the host wherever it points. Use an OS-level
// mechanism (os.Root, a chroot, a container) when the tree may contain links
// that basefs did not create.
//
// Errors report the caller's names, never the underlying host paths.
package basefs

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/absfs/absfs"
)

// ErrPathEscapes is the error (wrapped in an *os.PathError or *os.LinkError)
// returned when a name, symlink target, or symlink content would resolve to a
// location outside the basefs root.
var ErrPathEscapes = errors.New("path escapes from base directory")

// windowsPaths enables Windows path rules (backslash separators, volume
// names, case-insensitive root comparison). It is a variable so the rules can
// be unit tested on every platform.
var windowsPaths = runtime.GOOS == "windows"

// toSlash converts Windows separators to forward slashes when Windows path
// rules apply. On other systems a backslash is an ordinary file name byte.
func toSlash(p string) string {
	if windowsPaths {
		return strings.ReplaceAll(p, `\`, "/")
	}
	return p
}

// hasVolume reports whether p begins with a Windows volume: a drive letter
// ("C:", "c:x") or two leading separators (UNC "\\server\share" and the
// device forms "\\?\" and "\\.\").
func hasVolume(p string) bool {
	if len(p) < 2 {
		return false
	}
	if p[1] == ':' && unicode.IsLetter(rune(p[0])) {
		return true
	}
	return isSep(p[0]) && isSep(p[1])
}

func isSep(c byte) bool {
	return c == '/' || c == '\\'
}

// normalizeForComparison converts a path to a canonical format for comparison.
// This handles the case where paths may be in different formats (e.g., "C:\Users\..."
// vs "/c/Users/..." on Windows when using osfs as the underlying filesystem).
func normalizeForComparison(p string) string {
	p = toSlash(p)

	// On Windows, also handle drive letter normalization: C:/... → /c/...
	if windowsPaths && len(p) >= 2 && p[1] == ':' && unicode.IsLetter(rune(p[0])) {
		p = "/" + strings.ToLower(p[:1]) + "/" + p[2:]
	}

	// Clean, keeping a UNC "//" lead intact on Windows.
	if windowsPaths && strings.HasPrefix(p, "//") {
		return "/" + path.Clean(p)
	}
	return path.Clean(p)
}

// relTo reports whether host lies on or below root, comparing on path
// separator boundaries, and if so returns the equivalent virtual path.
func relTo(root, host string) (string, bool) {
	r := normalizeForComparison(root)
	h := normalizeForComparison(host)
	if equalPath(h, r) {
		return "/", true
	}
	if !strings.HasSuffix(r, "/") {
		r += "/"
	}
	if len(h) <= len(r) || !equalPath(h[:len(r)], r) {
		return "", false
	}
	return "/" + h[len(r):], true
}

func equalPath(a, b string) bool {
	if windowsPaths {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// cleanVirtual cleans an absolute virtual path without clamping ".." at the
// root, reporting false if the path climbs above "/".
func cleanVirtual(p string) (string, bool) {
	rel := path.Clean(strings.TrimLeft(p, "/"))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	if rel == "." {
		return "/", true
	}
	if windowsPaths && !windowsLocal(rel) {
		return "", false
	}
	return "/" + rel, true
}

// windowsLocal reports whether rel, a clean slash-separated relative path,
// names an ordinary file under Windows rules: it contains no ':' (a drive
// letter or an alternate data stream such as "f:stream") and no element that
// is a reserved device name. On Windows itself filepath.IsLocal has the last
// word, since some Windows versions also reserve names such as "NUL.txt".
func windowsLocal(rel string) bool {
	if strings.Contains(rel, ":") {
		return false
	}
	for _, elem := range strings.Split(rel, "/") {
		if isReservedName(elem) {
			return false
		}
	}
	return runtime.GOOS != "windows" || filepath.IsLocal(rel)
}

// isReservedName reports whether elem is a Windows reserved device name,
// ignoring case and trailing dots and spaces, which Windows strips.
func isReservedName(elem string) bool {
	base := strings.ToUpper(strings.TrimRight(elem, ". "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		return base[3] >= '1' && base[3] <= '9'
	}
	return false
}

// baseFS contains the implementation shared by FileSystem and SymlinkFileSystem.
type baseFS struct {
	cwd    string // clean, absolute virtual path
	prefix string // cleaned root in the underlying filesystem's syntax
	fs     absfs.FileSystem
}

// newBase validates dir and returns a baseFS rooted at it.
func newBase(fsys absfs.FileSystem, dir string) (baseFS, error) {
	if dir == "" {
		return baseFS{}, os.ErrInvalid
	}
	if !path.IsAbs(dir) && !filepath.IsAbs(dir) {
		return baseFS{}, errors.New("not an absolute path")
	}
	if hasVolume(dir) {
		dir = filepath.Clean(dir)
	} else {
		dir = path.Clean(dir)
	}
	info, err := fsys.Stat(dir)
	if err != nil {
		return baseFS{}, err
	}
	if !info.IsDir() {
		return baseFS{}, errors.New("not a directory")
	}
	return baseFS{cwd: "/", prefix: dir, fs: fsys}, nil
}

// virtual resolves name to a clean, absolute virtual path, reporting false if
// it would leave the root.
func (b *baseFS) virtual(name string) (string, bool) {
	if windowsPaths && hasVolume(name) {
		return "", false
	}
	name = toSlash(name)
	if name == "" {
		return b.cwd, true
	}
	if !path.IsAbs(name) {
		name = b.cwd + "/" + name
	}
	return cleanVirtual(name)
}

// host joins a clean virtual path to the root.
func (b *baseFS) host(vpath string) string {
	if vpath == "/" {
		return b.prefix
	}
	rel, sep := vpath[1:], "/"
	if windowsPaths && hasVolume(b.prefix) {
		rel, sep = strings.ReplaceAll(rel, "/", `\`), `\`
	}
	if strings.HasSuffix(b.prefix, sep) {
		return b.prefix + rel
	}
	return b.prefix + sep + rel
}

// path resolves name to its virtual and host paths. A name that escapes the
// root yields an *os.PathError for op wrapping ErrPathEscapes.
func (b *baseFS) path(op, name string) (vpath, hpath string, err error) {
	vpath, ok := b.virtual(name)
	if ok {
		hpath = b.host(vpath)
		// Defence in depth: the joined path must sit on or below the root
		// on a separator boundary.
		_, ok = relTo(b.prefix, hpath)
	}
	if !ok {
		return "", "", &os.PathError{Op: op, Path: name, Err: ErrPathEscapes}
	}
	return vpath, hpath, nil
}

// Chdir changes the current working directory. The directory must exist
// inside the root.
func (b *baseFS) Chdir(dir string) error {
	vpath, hpath, err := b.path("chdir", dir)
	if err != nil {
		return err
	}
	info, err := b.fs.Stat(hpath)
	if err != nil {
		return b.fixErr("chdir", dir, err)
	}
	if !info.IsDir() {
		return &os.PathError{Op: "chdir", Path: dir, Err: errors.New("not a directory")}
	}
	b.cwd = vpath
	return nil
}

// Getwd returns the current working directory as a virtual path.
func (b *baseFS) Getwd() (dir string, err error) {
	return b.cwd, nil
}

// TempDir returns the underlying filesystem's temporary directory as a virtual
// path if it lies inside the root, and "/tmp" otherwise.
func (b *baseFS) TempDir() string {
	if vpath, ok := relTo(b.prefix, b.fs.TempDir()); ok {
		return vpath
	}
	return "/tmp"
}

// OpenFile opens a file using the given flags and the given mode.
func (b *baseFS) OpenFile(name string, flags int, perm os.FileMode) (absfs.File, error) {
	_, hpath, err := b.path("open", name)
	if err != nil {
		return new(absfs.InvalidFile), err
	}
	file, err := b.fs.OpenFile(hpath, flags, perm)
	if err != nil {
		return new(absfs.InvalidFile), b.fixErr("open", name, err)
	}
	return &File{file, b, name}, nil
}

// Open opens the named file for reading.
func (b *baseFS) Open(name string) (absfs.File, error) {
	_, hpath, err := b.path("open", name)
	if err != nil {
		return nil, err
	}
	file, err := b.fs.Open(hpath)
	if err != nil {
		return nil, b.fixErr("open", name, err)
	}
	return &File{file, b, name}, nil
}

// Create creates or truncates the named file.
func (b *baseFS) Create(name string) (absfs.File, error) {
	_, hpath, err := b.path("open", name)
	if err != nil {
		return nil, err
	}
	file, err := b.fs.Create(hpath)
	if err != nil {
		return nil, b.fixErr("open", name, err)
	}
	return &File{file, b, name}, nil
}

// Mkdir creates a directory in the filesystem, return an error if any
// happens.
func (b *baseFS) Mkdir(name string, perm os.FileMode) error {
	_, hpath, err := b.path("mkdir", name)
	if err != nil {
		return err
	}
	return b.fixErr("mkdir", name, b.fs.Mkdir(hpath, perm))
}

// MkdirAll creates a directory and any missing parents.
func (b *baseFS) MkdirAll(name string, perm os.FileMode) error {
	_, hpath, err := b.path("mkdir", name)
	if err != nil {
		return err
	}
	return b.fixErr("mkdir", name, b.fs.MkdirAll(hpath, perm))
}

// Remove removes a file identified by name, returning an error, if any
// happens.
func (b *baseFS) Remove(name string) error {
	_, hpath, err := b.path("remove", name)
	if err != nil {
		return err
	}
	return b.fixErr("remove", name, b.fs.Remove(hpath))
}

// RemoveAll removes name and any children it contains.
func (b *baseFS) RemoveAll(name string) error {
	_, hpath, err := b.path("remove", name)
	if err != nil {
		return err
	}
	return b.fixErr("remove", name, b.fs.RemoveAll(hpath))
}

// Rename renames (moves) oldname to newname. Both must lie inside the root.
func (b *baseFS) Rename(oldname, newname string) error {
	_, oldpath, err := b.path("rename", oldname)
	if err == nil {
		var newpath string
		_, newpath, err = b.path("rename", newname)
		if err == nil {
			err = b.fs.Rename(oldpath, newpath)
		}
	}
	return b.fixLinkErr("rename", oldname, newname, err)
}

// Stat returns the FileInfo structure describing file. If there is an error,
// it will be of type *PathError.
func (b *baseFS) Stat(name string) (os.FileInfo, error) {
	vpath, hpath, err := b.path("stat", name)
	if err != nil {
		return nil, err
	}
	info, err := b.fs.Stat(hpath)
	if err != nil {
		return nil, b.fixErr("stat", name, err)
	}
	return &fileinfo{info, path.Base(vpath)}, nil
}

// Chmod changes the mode of the named file to mode.
func (b *baseFS) Chmod(name string, mode os.FileMode) error {
	_, hpath, err := b.path("chmod", name)
	if err != nil {
		return err
	}
	return b.fixErr("chmod", name, b.fs.Chmod(hpath, mode))
}

// Chtimes changes the access and modification times of the named file
func (b *baseFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	_, hpath, err := b.path("chtimes", name)
	if err != nil {
		return err
	}
	return b.fixErr("chtimes", name, b.fs.Chtimes(hpath, atime, mtime))
}

// Chown changes the owner and group ids of the named file
func (b *baseFS) Chown(name string, uid, gid int) error {
	_, hpath, err := b.path("chown", name)
	if err != nil {
		return err
	}
	return b.fixErr("chown", name, b.fs.Chown(hpath, uid, gid))
}

// Truncate changes the size of the named file.
func (b *baseFS) Truncate(name string, size int64) error {
	_, hpath, err := b.path("truncate", name)
	if err != nil {
		return err
	}
	return b.fixErr("truncate", name, b.fs.Truncate(hpath, size))
}

// ReadDir reads the named directory and returns a list of directory entries sorted by filename.
func (b *baseFS) ReadDir(name string) ([]fs.DirEntry, error) {
	_, hpath, err := b.path("open", name)
	if err != nil {
		return nil, err
	}
	entries, err := b.fs.ReadDir(hpath)
	return entries, b.fixErr("open", name, err)
}

// ReadFile reads the named file and returns its contents.
func (b *baseFS) ReadFile(name string) ([]byte, error) {
	_, hpath, err := b.path("open", name)
	if err != nil {
		return nil, err
	}
	data, err := b.fs.ReadFile(hpath)
	return data, b.fixErr("open", name, err)
}

// Sub returns a read-only fs.FS for the subtree rooted at dir. The returned
// FS operates through this basefs, so it is confined to the root as well.
func (b *baseFS) Sub(dir string) (fs.FS, error) {
	vpath, _, err := b.path("sub", dir)
	if err != nil {
		return nil, err
	}
	sub, err := absfs.FilerToFS(b, vpath)
	if err != nil {
		return nil, b.fixErr("sub", dir, err)
	}
	return sub, nil
}

// SymlinkFileSystem is a basefs over an absfs.SymlinkFileSystem.
type SymlinkFileSystem struct {
	baseFS
	sfs absfs.SymlinkFileSystem
}

var _ absfs.SymlinkFileSystem = (*SymlinkFileSystem)(nil)

// NewFS creates a new SymlinkFileSystem from a `absfs.SymlinkFileSystem` compatible object
// and a path. The path must be absolute (Unix-style, or native on Windows)
// and must already exist in the fs provided otherwise an error is returned.
func NewFS(fs absfs.SymlinkFileSystem, dir string) (*SymlinkFileSystem, error) {
	base, err := newBase(fs, dir)
	if err != nil {
		return nil, err
	}
	return &SymlinkFileSystem{baseFS: base, sfs: fs}, nil
}

// Lstat returns the FileInfo for name without following a final symlink.
func (f *SymlinkFileSystem) Lstat(name string) (os.FileInfo, error) {
	vpath, hpath, err := f.path("lstat", name)
	if err != nil {
		return nil, err
	}
	info, err := f.sfs.Lstat(hpath)
	if err != nil {
		return nil, f.fixErr("lstat", name, err)
	}
	return &fileinfo{info, path.Base(vpath)}, nil
}

// Lchown changes the owner and group ids of name without following a final
// symlink.
func (f *SymlinkFileSystem) Lchown(name string, uid, gid int) error {
	_, hpath, err := f.path("lchown", name)
	if err != nil {
		return err
	}
	return f.fixErr("lchown", name, f.sfs.Lchown(hpath, uid, gid))
}

// Readlink returns the target of the symlink name.
//
// A relative target is returned unchanged; it is relative to the link's
// directory. An absolute target inside the root is returned as a virtual
// path. An absolute target outside the root has no virtual equivalent and is
// never revealed: Readlink returns an *os.PathError wrapping ErrPathEscapes.
func (f *SymlinkFileSystem) Readlink(name string) (string, error) {
	_, hpath, err := f.path("readlink", name)
	if err != nil {
		return "", err
	}
	target, err := f.sfs.Readlink(hpath)
	if err != nil {
		return "", f.fixErr("readlink", name, err)
	}

	// Relative symlinks are relative to the link location, not the root.
	if !path.IsAbs(target) && !filepath.IsAbs(target) && !(windowsPaths && hasVolume(target)) {
		return target, nil
	}
	vpath, ok := relTo(f.prefix, target)
	if !ok {
		return "", &os.PathError{Op: "readlink", Path: name, Err: ErrPathEscapes}
	}
	return vpath, nil
}

// Symlink creates newname as a symbolic link to oldname.
//
// oldname is resolved to a virtual path: an absolute oldname is taken as is,
// a relative one is joined to the directory of newname. ".." elements are
// resolved lexically, before any symlink is followed, so "l/../f" names f
// beside l whatever l points to. If the result would climb above the root,
// Symlink returns an *os.LinkError wrapping ErrPathEscapes and creates
// nothing.
//
// The target stored in the link never contains "..", so the link cannot be
// made to point outside the root by moving it, or a directory above it, with
// Rename, or by creating it beneath a symlinked directory:
//
//   - A relative oldname that does not climb out of the link's directory
//     once cleaned ("f", "sub/f", "l/../f") is stored cleaned and relative.
//     It only descends from wherever the link is.
//   - An absolute oldname, or a relative one that climbs ("../f"), is stored
//     as the absolute host path of its virtual target. Readlink reports it
//     as an absolute virtual path ("/f"), not as the text passed in, and the
//     link breaks if the root itself is moved on the host.
func (f *SymlinkFileSystem) Symlink(oldname, newname string) error {
	vlink, hlink, err := f.path("symlink", newname)
	if err != nil {
		return f.fixLinkErr("symlink", oldname, newname, err)
	}
	if oldname == "" {
		// No target to resolve; let the underlying filesystem decide.
		return f.fixLinkErr("symlink", oldname, newname, f.sfs.Symlink("", hlink))
	}

	target := toSlash(oldname)
	rel := !path.IsAbs(target)
	if rel {
		target = path.Clean(target)
	}
	vtarget, ok := cleanVirtual(target)
	if rel {
		vtarget, ok = cleanVirtual(path.Dir(vlink) + "/" + target)
	}
	if !ok || (windowsPaths && hasVolume(oldname)) {
		return &os.LinkError{Op: "symlink", Old: oldname, New: newname, Err: ErrPathEscapes}
	}
	if !rel || target == ".." || strings.HasPrefix(target, "../") {
		target = f.host(vtarget)
	}
	return f.fixLinkErr("symlink", oldname, newname, f.sfs.Symlink(target, hlink))
}

// FileSystem is a basefs over an absfs.FileSystem without symlink support.
type FileSystem struct {
	baseFS
}

var _ absfs.FileSystem = (*FileSystem)(nil)

// NewFileSystem creates a new FileSystem from a `absfs.FileSystem` compatible object
// and a path. The path must be absolute (Unix-style, or native on Windows)
// and must already exist in the fs provided otherwise an error is returned.
func NewFileSystem(fs absfs.FileSystem, dir string) (*FileSystem, error) {
	base, err := newBase(fs, dir)
	if err != nil {
		return nil, err
	}
	return &FileSystem{baseFS: base}, nil
}
