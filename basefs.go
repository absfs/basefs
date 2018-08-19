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

type FileSystem struct {
	fs     absfs.SymlinkFileSystem
	cwd    string
	prefix string
}

// NewFS creates a new FileSystem from a `absfs.FileSystem` compatible object
// and a path. The path must be an absolute path and must already exist in the
// fs provided otherwise an error is returned.
func NewFS(fs absfs.SymlinkFileSystem, dir string) (*FileSystem, error) {
	if dir == "" {
		return nil, os.ErrInvalid
	}

	if !path.IsAbs(dir) {
		return nil, errors.New("not an absolute path")
	}
	info, err := fs.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("not a directory")
	}

	return &FileSystem{fs, "/", dir}, nil
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

//Chmod changes the mode of the named file to mode.
func (f *FileSystem) Chmod(name string, mode os.FileMode) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	err = f.fs.Chmod(ppath, mode)
	return fixerr(f.prefix, err)
}

//Chtimes changes the access and modification times of the named file
func (f *FileSystem) Chtimes(name string, atime time.Time, mtime time.Time) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}
	err = f.fs.Chtimes(ppath, atime, mtime)
	return fixerr(f.prefix, err)
}

//Chown changes the owner and group ids of the named file
func (f *FileSystem) Chown(name string, uid, gid int) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	err = f.fs.Chown(ppath, uid, gid)
	return fixerr(f.prefix, err)
}

func (f *FileSystem) Separator() uint8 {
	return '/'
}

func (f *FileSystem) ListSeparator() uint8 {
	return ':'
}

func (f *FileSystem) Chdir(dir string) error {
	dir = path.Clean(dir)
	if path.IsAbs(dir) {
		f.cwd = dir
		return nil
	}

	f.cwd = path.Join(f.cwd, dir)
	return nil
}

func (f *FileSystem) Getwd() (dir string, err error) {
	return f.cwd, nil
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

func (f *FileSystem) path(name string) (string, error) {
	if name == "" {
		name = f.cwd
		//return "", &os.PathError{Op: "open", Path: "", Err: errors.New("no such file or directory")}
	}

	if name == "/" {
		return f.prefix, nil
	}

	if !path.IsAbs(name) {
		name = path.Clean(name)
	}
	name = filepath.Join(f.prefix, name)

	// We mustn't let any trickery escape the prefix path.
	if !strings.HasPrefix(name, f.prefix) {
		return "", &os.PathError{Op: "open", Path: name, Err: errors.New("no such file or directory")}
	}
	return name, nil
}

func (f *FileSystem) Lstat(name string) (os.FileInfo, error) {
	ppath, err := f.path(name)
	if err != nil {
		return nil, err
	}

	info, err := f.fs.Lstat(ppath)
	return info, fixerr(f.prefix, err)
}

// ess

func (f *FileSystem) Lchown(name string, uid, gid int) error {
	ppath, err := f.path(name)
	if err != nil {
		return err
	}

	err = f.fs.Lchown(ppath, uid, gid)
	return fixerr(f.prefix, err)
}

func (f *FileSystem) Readlink(name string) (string, error) {
	ppath, err := f.path(name)
	if err != nil {
		return "", err
	}

	target, err := f.fs.Readlink(ppath)
	if err != nil {
		return "", err
	}

	target = strings.TrimPrefix(target, f.prefix)

	return target, fixerr(f.prefix, err)
}

func (f *FileSystem) Symlink(oldname, newname string) error {
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
