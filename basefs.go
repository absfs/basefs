package basefs

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/absfs/absfs"
)

// baseFS contains the common implementation shared by FileSystem and SymlinkFileSystem
type baseFS struct {
	cwd    string
	prefix string
}

// Common methods shared by both FileSystem and SymlinkFileSystem

func (b *baseFS) Chdir(dir string) error {
	dir = path.Clean(dir)
	if path.IsAbs(dir) {
		b.cwd = dir
		return nil
	}

	b.cwd = path.Join(b.cwd, dir)
	return nil
}

func (b *baseFS) Getwd() (dir string, err error) {
	return b.cwd, nil
}

func (b *baseFS) path(name string) (string, error) {
	if name == "" {
		name = b.cwd
	}

	if name == "/" {
		return b.prefix, nil
	}

	if !path.IsAbs(name) {
		name = path.Clean(name)
	}
	// Use path.Join for Unix-style virtual paths, then convert to OS-native
	// for the underlying filesystem operations
	name = path.Join(b.prefix, name)

	// We mustn't let any trickery escape the prefix path.
	if !strings.HasPrefix(name, b.prefix) {
		return "", &os.PathError{Op: "open", Path: name, Err: errors.New("no such file or directory")}
	}
	return name, nil
}

type SymlinkFileSystem struct {
	baseFS
	fs absfs.SymlinkFileSystem
}

// NewFS creates a new SymlinkFileSystem from a `absfs.SymlinkFileSystem` compatible object
// and a path. The path must be an absolute path (Unix-style, starting with /)
// and must already exist in the fs provided otherwise an error is returned.
func NewFS(fs absfs.SymlinkFileSystem, dir string) (*SymlinkFileSystem, error) {
	if dir == "" {
		return nil, os.ErrInvalid
	}

	if !filepath.IsAbs(dir) {
		return nil, errors.New("not an absolute path")
	}
	info, err := fs.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("not a directory")
	}

	return &SymlinkFileSystem{
		baseFS: baseFS{cwd: "/", prefix: dir},
		fs:     fs,
	}, nil
}

// OpenFile opens a file using the given flags and the given mode.
func (f *SymlinkFileSystem) OpenFile(name string, flags int, perm os.FileMode) (absfs.File, error) {
	// flag := absfs.Flags(flags)
	ppath, err := f.path(name)
	if err != nil {
		return new(absfs.InvalidFile), err
	}

	file, err := f.fs.OpenFile(ppath, flags, perm)
	if err != nil {
		return new(absfs.InvalidFile), err
	}

	return &File{file, f.prefix, name}, fixerr(f.prefix, err)
}

// Mkdir creates a directory in the filesystem, return an error if any
// happens.
func (f *SymlinkFileSystem) Mkdir(name string, perm os.FileMode) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}
	err = f.fs.Mkdir(ppath, perm)
	return fixerr(f.prefix, err)
}

// Remove removes a file identified by name, returning an error, if any
// happens.
func (f *SymlinkFileSystem) Remove(name string) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	err = f.fs.Remove(ppath)
	return fixerr(f.prefix, err)
}

func (f *SymlinkFileSystem) Rename(oldname, newname string) error {
	linkErr := os.LinkError{Op: "rename", Old: oldname, New: newname}
	oldpath, err := f.path(oldname)
	if err != nil {
		linkErr.Err = err
		return &linkErr
	}
	newpath, err := f.path(newname)
	if err != nil {
		linkErr.Err = err
		return &linkErr
	}
	err = f.fs.Rename(oldpath, newpath)
	return fixerr(f.prefix, err)
}

// Stat returns the FileInfo structure describing file. If there is an error,
// it will be of type *PathError.
func (f *SymlinkFileSystem) Stat(name string) (os.FileInfo, error) {
	ppath, err := f.path(name)
	if err != nil {
		return nil, err
	}

	info, err := f.fs.Stat(ppath)
	if err != nil {
		return nil, fixerr(f.prefix, err)
	}

	return &fileinfo{info, path.Base(name)}, nil
}

// Chmod changes the mode of the named file to mode.
func (f *SymlinkFileSystem) Chmod(name string, mode os.FileMode) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	err = f.fs.Chmod(ppath, mode)
	return fixerr(f.prefix, err)
}

// Chtimes changes the access and modification times of the named file
func (f *SymlinkFileSystem) Chtimes(name string, atime time.Time, mtime time.Time) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}
	err = f.fs.Chtimes(ppath, atime, mtime)
	return fixerr(f.prefix, err)
}

// Chown changes the owner and group ids of the named file
func (f *SymlinkFileSystem) Chown(name string, uid, gid int) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	err = f.fs.Chown(ppath, uid, gid)
	return fixerr(f.prefix, err)
}

func (f *SymlinkFileSystem) TempDir() string {
	tmpdir := f.fs.TempDir()

	if strings.HasPrefix(tmpdir, f.prefix+"/") {
		return strings.TrimPrefix(tmpdir, f.prefix)
	}

	// We can't return the underlying TempDir if it breaks out of the prefix path.
	return "/tmp"
}

func (f *SymlinkFileSystem) Open(name string) (absfs.File, error) {
	ppath, err := f.path(name)
	if err != nil {
		return nil, err
	}

	file, err := f.fs.Open(ppath)
	if err != nil {
		err = fixerr(f.prefix, err)
		return nil, err
	}

	return &File{file, f.prefix, name}, nil
}

func (f *SymlinkFileSystem) Create(name string) (absfs.File, error) {
	ppath, err := f.path(name)
	if err != nil {
		return nil, err
	}

	file, err := f.fs.Create(ppath)
	if err != nil {
		return nil, err
	}

	return &File{file, f.prefix, name}, err
}

func (f *SymlinkFileSystem) MkdirAll(name string, perm os.FileMode) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	return f.fs.MkdirAll(ppath, perm)
}

func (f *SymlinkFileSystem) RemoveAll(name string) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	return f.fs.RemoveAll(ppath)
}

func (f *SymlinkFileSystem) Truncate(name string, size int64) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	return f.fs.Truncate(ppath, size)
}

func (f *SymlinkFileSystem) Lstat(name string) (os.FileInfo, error) {
	ppath, err := f.path(name)
	if err != nil {
		return nil, err
	}

	info, err := f.fs.Lstat(ppath)
	return info, fixerr(f.prefix, err)
}

// ess

func (f *SymlinkFileSystem) Lchown(name string, uid, gid int) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	err = f.fs.Lchown(ppath, uid, gid)
	return fixerr(f.prefix, err)
}

func (f *SymlinkFileSystem) Readlink(name string) (string, error) {
	ppath, err := f.path(name)
	if err != nil {
		return "", err
	}

	target, err := f.fs.Readlink(ppath)
	if err != nil {
		return "", fixerr(f.prefix, err)
	}

	// If the target is a relative path, return it as-is
	// (relative symlinks are relative to the link location, not the basefs root)
	if !path.IsAbs(target) && !filepath.IsAbs(target) {
		return target, nil
	}

	// If the target is within our prefix, convert it to a virtual path
	if strings.HasPrefix(target, f.prefix) {
		target = strings.TrimPrefix(target, f.prefix)
		// Convert OS path separators to forward slashes for virtual paths
		target = filepath.ToSlash(target)
		// Ensure the result is an absolute path and clean it
		if target == "" || !strings.HasPrefix(target, "/") {
			target = "/" + target
		}
		target = path.Clean(target)
	}

	return target, nil
}

func (f *SymlinkFileSystem) Symlink(oldname, newname string) error {
	// Only translate newname (the link path) through basefs prefix
	// oldname (the target) should be:
	// - Kept as-is if relative (symlink target is relative to link location)
	// - Translated if absolute (symlink target points to absolute path in virtual fs)
	pnewname, err := f.path(newname)
	if err != nil {
		return err
	}

	var target string
	if path.IsAbs(oldname) {
		// Absolute virtual path - translate to real path
		target, err = f.path(oldname)
		if err != nil {
			return err
		}
	} else {
		// Relative path - keep as-is, symlink is relative to link location
		target = oldname
	}

	err = f.fs.Symlink(target, pnewname)
	return fixerr(f.prefix, err)
}

// ReadDir reads the named directory and returns a list of directory entries sorted by filename.
func (f *SymlinkFileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	ppath, err := f.path(name)
	if err != nil {
		return nil, err
	}

	entries, err := f.fs.ReadDir(ppath)
	return entries, fixerr(f.prefix, err)
}

// ReadFile reads the named file and returns its contents.
func (f *SymlinkFileSystem) ReadFile(name string) ([]byte, error) {
	ppath, err := f.path(name)
	if err != nil {
		return nil, err
	}

	data, err := f.fs.ReadFile(ppath)
	return data, fixerr(f.prefix, err)
}

// Sub returns an fs.FS corresponding to the subtree rooted at dir.
func (f *SymlinkFileSystem) Sub(dir string) (fs.FS, error) {
	ppath, err := f.path(dir)
	if err != nil {
		return nil, err
	}

	return absfs.FilerToFS(f.fs, ppath)
}

type FileSystem struct {
	baseFS
	fs absfs.FileSystem
}

// NewFileSystem creates a new FileSystem from a `absfs.FileSystem` compatible object
// and a path. The path must be an absolute path (Unix-style, starting with /)
// and must already exist in the fs provided otherwise an error is returned.
func NewFileSystem(fs absfs.FileSystem, dir string) (*FileSystem, error) {
	if dir == "" {
		return nil, os.ErrInvalid
	}

	if !filepath.IsAbs(dir) {
		return nil, errors.New("not an absolute path")
	}
	info, err := fs.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("not a directory")
	}

	return &FileSystem{
		baseFS: baseFS{cwd: "/", prefix: dir},
		fs:     fs,
	}, nil
}

// OpenFile opens a file using the given flags and the given mode.
func (f *FileSystem) OpenFile(name string, flags int, perm os.FileMode) (absfs.File, error) {
	// flag := absfs.Flags(flags)
	ppath, err := f.path(name)
	if err != nil {
		return new(absfs.InvalidFile), err
	}

	file, err := f.fs.OpenFile(ppath, flags, perm)
	if err != nil {
		return new(absfs.InvalidFile), err
	}

	return &File{file, f.prefix, name}, fixerr(f.prefix, err)
}

// Mkdir creates a directory in the filesystem, return an error if any
// happens.
func (f *FileSystem) Mkdir(name string, perm os.FileMode) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}
	err = f.fs.Mkdir(ppath, perm)
	return fixerr(f.prefix, err)
}

// Remove removes a file identified by name, returning an error, if any
// happens.
func (f *FileSystem) Remove(name string) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	err = f.fs.Remove(ppath)
	return fixerr(f.prefix, err)
}

func (f *FileSystem) Rename(oldname, newname string) error {
	linkErr := os.LinkError{Op: "rename", Old: oldname, New: newname}
	oldpath, err := f.path(oldname)
	if err != nil {
		linkErr.Err = err
		return &linkErr
	}
	newpath, err := f.path(newname)
	if err != nil {
		linkErr.Err = err
		return &linkErr
	}
	err = f.fs.Rename(oldpath, newpath)
	return fixerr(f.prefix, err)
}

// Stat returns the FileInfo structure describing file. If there is an error,
// it will be of type *PathError.
func (f *FileSystem) Stat(name string) (os.FileInfo, error) {
	ppath, err := f.path(name)
	if err != nil {
		return nil, err
	}

	info, err := f.fs.Stat(ppath)
	if err != nil {
		return nil, fixerr(f.prefix, err)
	}

	return &fileinfo{info, path.Base(name)}, nil
}

// Chmod changes the mode of the named file to mode.
func (f *FileSystem) Chmod(name string, mode os.FileMode) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	err = f.fs.Chmod(ppath, mode)
	return fixerr(f.prefix, err)
}

// Chtimes changes the access and modification times of the named file
func (f *FileSystem) Chtimes(name string, atime time.Time, mtime time.Time) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}
	err = f.fs.Chtimes(ppath, atime, mtime)
	return fixerr(f.prefix, err)
}

// Chown changes the owner and group ids of the named file
func (f *FileSystem) Chown(name string, uid, gid int) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	err = f.fs.Chown(ppath, uid, gid)
	return fixerr(f.prefix, err)
}

func (f *FileSystem) TempDir() string {
	tmpdir := f.fs.TempDir()

	if strings.HasPrefix(tmpdir, f.prefix+"/") {
		return strings.TrimPrefix(tmpdir, f.prefix)
	}

	// We can't return the underlying TempDir if it breaks out of the prefix path.
	return "/tmp"
}

func (f *FileSystem) Open(name string) (absfs.File, error) {
	ppath, err := f.path(name)
	if err != nil {
		return nil, err
	}

	file, err := f.fs.Open(ppath)
	if err != nil {
		err = fixerr(f.prefix, err)
		return nil, err
	}

	return &File{file, f.prefix, name}, nil
}

func (f *FileSystem) Create(name string) (absfs.File, error) {
	ppath, err := f.path(name)
	if err != nil {
		return nil, err
	}

	file, err := f.fs.Create(ppath)
	if err != nil {
		return nil, err
	}

	return &File{file, f.prefix, name}, err
}

func (f *FileSystem) MkdirAll(name string, perm os.FileMode) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	return f.fs.MkdirAll(ppath, perm)
}

func (f *FileSystem) RemoveAll(name string) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	return f.fs.RemoveAll(ppath)
}

func (f *FileSystem) Truncate(name string, size int64) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	return f.fs.Truncate(ppath, size)
}

// ReadDir reads the named directory and returns a list of directory entries sorted by filename.
func (f *FileSystem) ReadDir(name string) ([]fs.DirEntry, error) {
	ppath, err := f.path(name)
	if err != nil {
		return nil, err
	}

	entries, err := f.fs.ReadDir(ppath)
	return entries, fixerr(f.prefix, err)
}

// ReadFile reads the named file and returns its contents.
func (f *FileSystem) ReadFile(name string) ([]byte, error) {
	ppath, err := f.path(name)
	if err != nil {
		return nil, err
	}

	data, err := f.fs.ReadFile(ppath)
	return data, fixerr(f.prefix, err)
}

// Sub returns an fs.FS corresponding to the subtree rooted at dir.
func (f *FileSystem) Sub(dir string) (fs.FS, error) {
	ppath, err := f.path(dir)
	if err != nil {
		return nil, err
	}

	return absfs.FilerToFS(f.fs, ppath)
}

