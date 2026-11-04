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
// out of the root, unless the underlying filesystem confines itself to the
// root (see below).
//
// Symlink stores a relative target verbatim, and an absolute target as the
// root's host path followed by the target, so Readlink returns exactly the
// text given to Symlink, as POSIX tools expect. It refuses a target that,
// resolved lexically from the link's directory, would climb above the root,
// and an absolute target that is not clean ("/a/../b"). Rename makes the
// same check on a relative link it moves, from the link's new directory, and
// refuses a move that would take the target above the root. Those checks are
// made on names, when a link is created or moved. basefs alone cannot stop
// the host from following a link physically, so a link that was inside the
// root when made can lead out of it when the host follows it later:
//
//   - a relative link such as "../f", carried by a Rename of a directory
//     holding it somewhere shallower, now climbs further (Rename does not
//     look inside the directories it moves);
//   - a relative target such as "l/../f", where l is itself a symlink, is
//     resolved by the host through wherever l points, not lexically;
//   - a link created beneath, or moved by Rename into, a symlinked
//     directory is resolved by the host from the directory it actually
//     lands in.
//
// Readlink never reports a host path outside the root.
//
// When the underlying filesystem has an OpenRoot(dir string)
// (absfs.SymlinkFileSystem, error) method, NewFS and NewFileSystem operate
// through what it returns for the root. osfs, built with Go 1.25 or later on
// a Unix system, returns a filesystem confined to the root by os.Root. The
// host then never follows a link out of the root: each case above fails with
// ErrPathEscapes, as does any link placed on the host that leads outside.
// Links are judged on the names os.Root sees: a relative link that climbs
// out and back in ("../root/f") fails too, and an absolute link resolves
// inside the root only if it starts with the root's path as given to NewFS
// or its canonical path (filepath.EvalSymlinks), as the absolute links that
// basefs stores do. Confined reports whether a basefs is confined this way.
// If OpenRoot reports errors.ErrUnsupported (osfs before Go 1.25, or on
// Windows), or a permission error (os.Root must read the root, so a root
// the process can search but not read, such as mode 0333, cannot be
// opened), basefs uses the underlying filesystem directly and constrains
// names only. Any other OpenRoot error makes NewFS fail. NewFSUnconfined and
// NewFileSystemUnconfined never confine. Use another OS-level mechanism (a
// chroot, a container) when the host's own resolution of links must stay
// inside the root and Confined is false.
//
// A confined basefs holds its root directory open, until Close or garbage
// collection, and is bound to that directory rather than to its path: if
// the root is renamed on the host, the basefs follows it, and a directory
// created at the old path is not seen; if the root is removed, every
// operation fails, even after a directory is created at the same path.
//
// Errors report the caller's names, never the underlying host paths.
package basefs

import (
	"errors"
	"io"
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
// location outside the basefs root, or, for an absolute symlink target that
// is not clean, could do so when the host follows it.
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

// rootOpener is implemented by an underlying filesystem that can confine
// itself to one of its directories, as osfs does with os.Root.
type rootOpener interface {
	OpenRoot(dir string) (absfs.SymlinkFileSystem, error)
}

// baseFS contains the implementation shared by FileSystem and SymlinkFileSystem.
type baseFS struct {
	cwd      string // clean, absolute virtual path
	prefix   string // cleaned root in the underlying filesystem's syntax
	fs       absfs.FileSystem
	under    absfs.FileSystem        // the filesystem given to NewFS
	confined absfs.SymlinkFileSystem // fs, when confined to the root
}

// newBase validates dir and returns a baseFS rooted at it. With confine, it
// operates through the underlying filesystem's OpenRoot where that works.
func newBase(fsys absfs.FileSystem, dir string, confine bool) (baseFS, error) {
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
	b := baseFS{cwd: "/", prefix: dir, fs: fsys, under: fsys}
	if !confine {
		return b, nil
	}
	// Where the underlying filesystem can confine itself to the root (osfs
	// with Go 1.25 on Unix), let it, so the host cannot follow a link out.
	// os.Root must open the root for reading, so a root the process can
	// search but not read (a drop box, mode 0333) stays unconfined.
	if ro, ok := fsys.(rootOpener); ok {
		confined, err := ro.OpenRoot(dir)
		switch {
		case err == nil:
			b.fs, b.confined = confined, confined
		case !errors.Is(err, errors.ErrUnsupported) && !errors.Is(err, fs.ErrPermission):
			return baseFS{}, err
		}
	}
	return b, nil
}

// Close releases what a confined basefs holds open: the root directory of
// the confined view of the underlying filesystem (an os.Root over osfs).
// Operations after Close then fail, including through a Sub of it. Close of
// an unconfined basefs does nothing and returns nil. A basefs that is not
// closed releases the directory when it is garbage collected.
func (b *baseFS) Close() error {
	if c, ok := b.confined.(io.Closer); ok {
		return c.Close()
	}
	return nil
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
//
// RemoveAll of the root ("/") removes everything in it. Unless the basefs is
// Confined, it then removes the root directory too. A confined basefs keeps
// the root directory, which it holds open: os.Root cannot remove its own
// root.
func (b *baseFS) RemoveAll(name string) error {
	vpath, hpath, err := b.path("remove", name)
	if err != nil {
		return err
	}
	if vpath == "/" && b.confined != nil {
		return b.fixErr("remove", name, b.removeChildren())
	}
	return b.fixErr("remove", name, b.fs.RemoveAll(hpath))
}

// removeChildren removes everything in the root, but not the root.
func (b *baseFS) removeChildren() error {
	entries, err := b.fs.ReadDir(b.prefix)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := b.fs.RemoveAll(b.host("/" + e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// Rename renames (moves) oldname to newname. Both must lie inside the root.
//
// When oldname is itself a symbolic link with a relative target, Rename
// resolves that target lexically against the directory of newname, as
// Symlink does at creation, and refuses the move with an *os.LinkError
// wrapping ErrPathEscapes if the target would then climb above the root:
// Symlink("../../x", "/a/b/e") is accepted, but Rename("/a/b/e", "/d") is
// not, and the link stays where it was. An absolute target is stored under
// the root and stays inside wherever the link moves, so it is not checked.
// The check is made whether or not the basefs is Confined. An underlying
// filesystem with no Lstat and Readlink cannot report links, so a
// FileSystem over it has nothing to check.
//
// The check is made on names, before the move. It resolves the target from
// the destination's name, so a link moved into a symlinked directory is
// resolved by the host from the directory it actually lands in, and a link
// swapped in between the check and the move, by this or another process, is
// not checked. Both rely on confinement; see the package documentation.
//
// Rename does not look inside a directory it moves: a relative link carried
// by a Rename of a directory holding it can climb above the root from its
// new place. Checking every link in the subtree would cost time in
// proportion to its size and would race with changes made meanwhile. When
// the basefs is Confined, the host refuses to follow such a link out of the
// root; otherwise the host follows it wherever it leads. See the package
// documentation.
func (b *baseFS) Rename(oldname, newname string) error {
	_, oldpath, err := b.path("rename", oldname)
	if err == nil {
		var vnew, newpath string
		vnew, newpath, err = b.path("rename", newname)
		if err == nil {
			err = b.checkMovedLink(oldpath, vnew)
		}
		if err == nil {
			err = b.fs.Rename(oldpath, newpath)
		}
	}
	return b.fixLinkErr("rename", oldname, newname, err)
}

// linkReader is the part of absfs.SymLinker that checkMovedLink needs.
type linkReader interface {
	Lstat(name string) (os.FileInfo, error)
	Readlink(name string) (string, error)
}

// checkMovedLink returns ErrPathEscapes if hpath is a symbolic link with a
// relative target that, resolved lexically from the directory of the
// virtual path vdst, would climb above the root. If hpath cannot be
// examined, it returns nil and leaves Rename to report the error.
func (b *baseFS) checkMovedLink(hpath, vdst string) error {
	lr, ok := b.fs.(linkReader)
	if !ok {
		return nil
	}
	info, err := lr.Lstat(hpath)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return nil
	}
	target, err := lr.Readlink(hpath)
	if err != nil {
		return err
	}
	target = toSlash(target)
	if path.IsAbs(target) || (windowsPaths && hasVolume(target)) {
		return nil
	}
	if _, ok := cleanVirtual(path.Dir(vdst) + "/" + target); !ok {
		return ErrPathEscapes
	}
	return nil
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
	return named(info, path.Base(vpath)), nil
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
//
// Where fs has an OpenRoot method that succeeds for dir (osfs with Go 1.25
// or later on a Unix system), the result is Confined; see the package
// documentation. Call Close to release the root directory it holds open.
func NewFS(fs absfs.SymlinkFileSystem, dir string) (*SymlinkFileSystem, error) {
	return newFS(fs, dir, true)
}

// NewFSUnconfined is NewFS without confinement: it never calls the
// underlying filesystem's OpenRoot, so it constrains names only, and the
// host follows symlinks wherever they lead, out of the root included. It
// keeps the behaviour of NewFS before confinement, for trees whose links
// are meant to lead outside the root.
func NewFSUnconfined(fs absfs.SymlinkFileSystem, dir string) (*SymlinkFileSystem, error) {
	return newFS(fs, dir, false)
}

func newFS(fs absfs.SymlinkFileSystem, dir string, confine bool) (*SymlinkFileSystem, error) {
	base, err := newBase(fs, dir, confine)
	if err != nil {
		return nil, err
	}
	if base.confined != nil {
		fs = base.confined
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
	return named(info, path.Base(vpath)), nil
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
// A relative target is returned exactly as stored; it is relative to the
// link's directory. An absolute target is returned as an absolute virtual
// path: the root's host path is removed from the front and the rest is
// returned as written, so Readlink(l) after Symlink(t, l) returns t. The
// judgement is lexical and made on the target as written, not cleaned: a
// target that does not start with the root's host path on a separator
// boundary, or whose rest would climb above the root, has no virtual
// equivalent and is never revealed. Readlink returns an *os.PathError
// wrapping ErrPathEscapes for it. That includes a detour such as
// <parent>/xfoo/../x/f for a root of <parent>/x, and, on Windows, a rooted
// target without a volume (\Windows) or on another volume.
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
	// toSlash makes a rooted Windows target such as \Windows absolute here,
	// as it is in Symlink.
	if !path.IsAbs(toSlash(target)) && !(windowsPaths && hasVolume(target)) {
		return target, nil
	}
	// The target, as written, must lie on or below the root and must not
	// climb above it from there; it is not cleaned first, so a detour such
	// as <parent>/xfoo/../x/f is refused rather than read as /f.
	vpath, ok := verbatimRel(f.prefix, target)
	if ok {
		_, ok = cleanVirtual(vpath)
	}
	if !ok {
		return "", &os.PathError{Op: "readlink", Path: name, Err: ErrPathEscapes}
	}
	return vpath, nil
}

// verbatimRel is relTo without cleaning host: it removes root from the front
// of host on a separator boundary and returns the rest, as written but with
// forward slashes, as a virtual path.
func verbatimRel(root, host string) (string, bool) {
	r := normalizeForComparison(root)
	h := toSlash(host)
	if windowsPaths && len(h) >= 2 && h[1] == ':' && unicode.IsLetter(rune(h[0])) {
		// C:/x -> /c/x, as normalizeForComparison does, minus the cleaning.
		rest := h[2:]
		if !strings.HasPrefix(rest, "/") {
			rest = "/" + rest
		}
		h = "/" + strings.ToLower(h[:1]) + rest
	}
	if !strings.HasSuffix(r, "/") {
		if equalPath(h, r) {
			return "/", true
		}
		r += "/"
	}
	if len(h) < len(r) || !equalPath(h[:len(r)], r) {
		return "", false
	}
	return "/" + h[len(r):], true
}

// Symlink creates newname as a symbolic link to oldname.
//
// Readlink of the new link returns oldname unchanged. What is stored on the
// underlying filesystem is:
//
//   - for a relative oldname ("f", "../f"), oldname itself;
//   - for an absolute oldname ("/a/f"), the root's host path followed by
//     oldname, so the host resolves it inside the root. A program that reads
//     the link directly from the underlying filesystem, bypassing basefs,
//     sees that host path (for example "/srv/data/a/f" for a root of
//     /srv/data), and the link breaks if the root directory itself is moved
//     on the host.
//
// On Windows, through osfs, the host stores targets with backslashes and
// they read back with forward slashes, relative ones included: "..\x" reads
// back as "../x".
//
// At creation, oldname is checked lexically: a relative oldname is joined to
// the directory of newname, and ".." elements are applied to the name
// without following any symlink. If the result would climb above the root,
// Symlink returns an *os.LinkError wrapping ErrPathEscapes and creates
// nothing. An absolute oldname must also be clean, as path.Clean leaves it:
// "/a/../b", "/a/./b", "//a" and "/a/" are refused the same way, because the
// host would apply ".." physically, through any symlink before it, and
// because Windows stores absolute targets cleaned, so they could not read
// back as written. On Windows, an oldname carrying a volume is refused too.
//
// That check concerns names, at creation time; Rename repeats it for a link
// it moves. The host follows a stored link physically, so unless the basefs
// is Confined, a link that named a place inside the root when it was made
// can resolve outside it later: a relative link inside a directory moved by
// Rename somewhere shallower, or a relative target whose ".." follows an
// element that is itself a symlink. See the package documentation.
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
	abs := path.IsAbs(target)
	resolved := target
	if !abs {
		resolved = path.Dir(vlink) + "/" + target
	}
	_, ok := cleanVirtual(resolved)
	// An absolute target must be clean. The host applies ".." physically,
	// through any symlink before it ("/l/../f" with l a link to "/" leaves
	// the root), and Windows stores absolute targets cleaned, so anything
	// else could not read back as written.
	if !ok || (abs && path.Clean(target) != target) || (windowsPaths && hasVolume(oldname)) {
		return &os.LinkError{Op: "symlink", Old: oldname, New: newname, Err: ErrPathEscapes}
	}
	stored := oldname
	if abs {
		stored = f.host(target)
	}
	return f.fixLinkErr("symlink", oldname, newname, f.sfs.Symlink(stored, hlink))
}

// FileSystem is a basefs over an absfs.FileSystem without symlink support.
type FileSystem struct {
	baseFS
}

var _ absfs.FileSystem = (*FileSystem)(nil)

// NewFileSystem creates a new FileSystem from a `absfs.FileSystem` compatible object
// and a path. The path must be absolute (Unix-style, or native on Windows)
// and must already exist in the fs provided otherwise an error is returned.
//
// Like NewFS, it confines itself where fs has an OpenRoot method that
// succeeds for dir. Call Close to release the root directory it holds open.
func NewFileSystem(fs absfs.FileSystem, dir string) (*FileSystem, error) {
	return newFileSystem(fs, dir, true)
}

// NewFileSystemUnconfined is NewFileSystem without confinement, as
// NewFSUnconfined is for NewFS.
func NewFileSystemUnconfined(fs absfs.FileSystem, dir string) (*FileSystem, error) {
	return newFileSystem(fs, dir, false)
}

func newFileSystem(fs absfs.FileSystem, dir string, confine bool) (*FileSystem, error) {
	base, err := newBase(fs, dir, confine)
	if err != nil {
		return nil, err
	}
	return &FileSystem{baseFS: base}, nil
}
