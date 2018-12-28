package basefs

import (
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/absfs/absfs"
)

type File struct {
	f      absfs.File
	prefix string
	name   string
}

func fixerr(prefix string, err error) error {
	if err == nil {
		return nil
	}

	if err == io.EOF {
		return io.EOF
	}
	switch v := err.(type) {
	case *os.PathError:
		p := strings.Replace(v.Path, prefix, "", 0)
		return &os.PathError{Op: v.Op, Path: p, Err: v.Err}
	default:
		return errors.New(strings.Replace(err.Error(), prefix, "", 0))
	}
}

func (f *File) Name() string {
	return f.name
}

func (f *File) Read(p []byte) (n int, err error) {
	n, err = f.f.Read(p)

	return n, fixerr(f.prefix, err)
}

func (f *File) ReadAt(b []byte, off int64) (n int, err error) {
	n, err = f.f.ReadAt(b, off)

	return n, fixerr(f.prefix, err)
}

func (f *File) Write(p []byte) (n int, err error) {
	n, err = f.f.Write(p)

	return n, fixerr(f.prefix, err)
}

func (f *File) WriteAt(b []byte, off int64) (n int, err error) {
	n, err = f.f.WriteAt(b, off)

	return n, fixerr(f.prefix, err)
}

func (f *File) Close() error {
	err := f.f.Close()

	return fixerr(f.prefix, err)
}

func (f *File) Seek(offset int64, whence int) (ret int64, err error) {
	ret, err = f.f.Seek(offset, whence)

	return ret, fixerr(f.prefix, err)
}

func (f *File) Stat() (os.FileInfo, error) {
	info, err := f.f.Stat()
	if err != nil {
		return nil, fixerr(f.prefix, err)
	}

	return &fileinfo{info, path.Base(f.name)}, nil
}

func (f *File) Sync() error {
	return fixerr(f.prefix, f.f.Sync())
}

func (f *File) Readdir(n int) (dirs []os.FileInfo, err error) {
	// fmt.Printf("absfs/basefs Readdir %d\n", n)
	dirs, err = f.f.Readdir(n)
	// if err != nil {
	// 	fmt.Printf("absfs/basefs Readdir Error %s\n", err)
	// }
	return dirs, fixerr(f.prefix, err)
}

func (f *File) Readdirnames(n int) (names []string, err error) {
	names, err = f.f.Readdirnames(n)
	return names, fixerr(f.prefix, err)
}

func (f *File) Truncate(size int64) error {
	return fixerr(f.prefix, f.f.Truncate(size))
}

func (f *File) WriteString(s string) (n int, err error) {
	n, err = f.f.WriteString(s)

	return n, fixerr(f.prefix, err)
}

type fileinfo struct {
	info os.FileInfo
	name string
}

func (i *fileinfo) Name() string {
	return i.name
}

func (i *fileinfo) Size() int64 {
	return i.info.Size()
}

func (i *fileinfo) ModTime() time.Time {
	return i.info.ModTime()
}

func (i *fileinfo) Mode() os.FileMode {
	return i.info.Mode()
}

func (i *fileinfo) Sys() interface{} {
	return i.info.Sys()
}

func (i *fileinfo) IsDir() bool {
	return i.info.IsDir()
}
