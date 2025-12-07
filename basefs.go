package basefs

import (
	"errors"
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

func (b *baseFS) Separator() uint8 {
	return '/'
}

func (b *baseFS) ListSeparator() uint8 {
	return ':'
}

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
	name = filepath.Join(b.prefix, name)

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
// and a path. The path must be an absolute path and must already exist in the
// fs provided otherwise an error is returned.
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
		return "", err
	}

	// If the target is within our prefix, convert it to a virtual path
	if strings.HasPrefix(target, f.prefix) {
		target = strings.TrimPrefix(target, f.prefix)
		// Ensure the result is an absolute path
		if target == "" || !strings.HasPrefix(target, "/") {
			target = "/" + target
		}
	}

	return target, fixerr(f.prefix, err)
}

func (f *SymlinkFileSystem) Symlink(oldname, newname string) error {
	poldname, err := f.path(oldname)
	if err != nil {
		return err
	}
	pnewname, err := f.path(newname)
	if err != nil {
		return err
	}

	err = f.fs.Symlink(poldname, pnewname)
	return fixerr(f.prefix, err)
}

type FileSystem struct {
	baseFS
	fs absfs.FileSystem
}

// NewFileSystem creates a new FileSystem from a `absfs.FileSystem` compatible object
// and a path. The path must be an absolute path and must already exist in the
// fs provided otherwise an error is returned.
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

type walker interface {
	Walk(string, func(string, os.FileInfo, error) error) error
}

type fastwalker interface {
	FastWalk(string, func(string, os.FileMode) error) error
}

var errNoWalk = errors.New("walk function not supported by underlying filesystem")
var errNoFastWalk = errors.New("fastwalk function not supported by underlying filesystem")

func (fs *SymlinkFileSystem) Walk(name string, fn filepath.WalkFunc) error {
	ppath, err := fs.path(name)
	if err != nil {
		return err
	}
	wfs, ok := fs.fs.(walker)
	if !ok {
		return errNoWalk
	}
	return wfs.Walk(ppath, func(path string, info os.FileInfo, err error) error {
		p := strings.TrimPrefix(path, fs.prefix)
		if p == "" {
			p = "/"
		}
		return fn(p, info, err)
	})
}

func (fs *SymlinkFileSystem) FastWalk(name string, fn absfs.FastWalkFunc) error {
	ppath, err := fs.path(name)
	if err != nil {
		return err
	}
	wfs, ok := fs.fs.(fastwalker)
	if !ok {
		return errNoFastWalk
	}
	return wfs.FastWalk(ppath, func(path string, mode os.FileMode) error {
		p := strings.TrimPrefix(path, fs.prefix)
		if p == "" {
			p = "/"
		}
		return fn(p, mode)
	})
}
