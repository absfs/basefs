package basefs

import (
	"io/fs"
	"os"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/absfs/absfs"
	"github.com/absfs/osfs"
)

// File is an open file in a basefs filesystem. Errors it returns report the
// name the file was opened with, never the underlying host path.
type File struct {
	f    absfs.File
	b    *baseFS
	name string
}

// scrubbedError carries an underlying error whose message mentioned the host
// root. Error reports the message with the root removed; Unwrap keeps
// errors.Is and errors.As working against the original.
type scrubbedError struct {
	msg string
	err error
}

func (e *scrubbedError) Error() string { return e.msg }
func (e *scrubbedError) Unwrap() error { return e.err }

// rootForms returns the spellings of the root that may appear in host error
// messages, without trailing separators. A root of "/" has nothing to strip.
func (b *baseFS) rootForms() []string {
	forms := []string{b.prefix}
	if windowsPaths {
		// "C:\x", "C:/x", "/c/x" and, for an osfs-style root, native "C:\x".
		slash := normalizeForComparison(b.prefix)
		forms = append(forms, toSlash(b.prefix), slash)
		if len(slash) >= 2 && slash[0] == '/' && unicode.IsLetter(rune(slash[1])) && (len(slash) == 2 || slash[2] == '/') {
			forms = append(forms, strings.ToUpper(slash[1:2])+":"+strings.ReplaceAll(slash[2:], "/", `\`))
		}
	}
	out := forms[:0]
	for _, f := range forms {
		if f = strings.TrimRight(f, `/\`); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// scrub removes the host root from err's message.
func (b *baseFS) scrub(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	clean := msg
	for _, form := range b.rootForms() {
		clean = strings.ReplaceAll(clean, form, "")
	}
	if clean == msg {
		return err
	}
	return &scrubbedError{msg: clean, err: err}
}

// fixErr rewrites an error from the underlying filesystem as an
// *os.PathError that reports the caller's name instead of the host path. A
// bare error (some filesystems return a syscall.Errno) is wrapped under op.
// A confined basefs always reports op, since os.Root reports its own *at
// system call names (openat, statat, ...) rather than the os ones.
func (b *baseFS) fixErr(op, name string, err error) error {
	switch e := err.(type) {
	case nil:
		return nil
	case *os.PathError:
		op, err = b.op(op, e.Op), e.Err
	case *os.LinkError:
		op, err = b.op(op, e.Op), e.Err
	}
	return &os.PathError{Op: op, Path: name, Err: b.scrub(escapes(err))}
}

// op picks the operation name to report: the underlying filesystem's,
// unless the basefs is confined.
func (b *baseFS) op(own, under string) string {
	if b.confined != nil {
		return own
	}
	return under
}

// escapes reports a confined underlying filesystem's refusal to leave the
// root as ErrPathEscapes.
func escapes(err error) error {
	if err == osfs.ErrPathEscapes {
		return ErrPathEscapes
	}
	return err
}

// fixLinkErr is fixErr for two-name operations; the result is always an
// *os.LinkError carrying the caller's names.
func (b *baseFS) fixLinkErr(op, oldname, newname string, err error) error {
	switch e := err.(type) {
	case nil:
		return nil
	case *os.LinkError:
		op, err = b.op(op, e.Op), e.Err
	case *os.PathError:
		err = e.Err
	}
	return &os.LinkError{Op: op, Old: oldname, New: newname, Err: b.scrub(escapes(err))}
}

// fixErr reports the file's name in place of the host path. Other errors,
// such as io.EOF, are returned unchanged unless they mention the host root.
func (f *File) fixErr(err error) error {
	if e, ok := err.(*os.PathError); ok {
		return &os.PathError{Op: e.Op, Path: f.name, Err: f.b.scrub(e.Err)}
	}
	return f.b.scrub(err)
}

// Name returns the name the file was opened with.
func (f *File) Name() string {
	return f.name
}

func (f *File) Read(p []byte) (n int, err error) {
	n, err = f.f.Read(p)
	return n, f.fixErr(err)
}

func (f *File) ReadAt(b []byte, off int64) (n int, err error) {
	n, err = f.f.ReadAt(b, off)
	return n, f.fixErr(err)
}

func (f *File) Write(p []byte) (n int, err error) {
	n, err = f.f.Write(p)
	return n, f.fixErr(err)
}

func (f *File) WriteAt(b []byte, off int64) (n int, err error) {
	n, err = f.f.WriteAt(b, off)
	return n, f.fixErr(err)
}

func (f *File) Close() error {
	return f.fixErr(f.f.Close())
}

func (f *File) Seek(offset int64, whence int) (ret int64, err error) {
	ret, err = f.f.Seek(offset, whence)
	return ret, f.fixErr(err)
}

func (f *File) Stat() (os.FileInfo, error) {
	info, err := f.f.Stat()
	if err != nil {
		return nil, f.fixErr(err)
	}
	return named(info, path.Base(f.name)), nil
}

func (f *File) Sync() error {
	return f.fixErr(f.f.Sync())
}

func (f *File) Readdir(n int) (dirs []os.FileInfo, err error) {
	dirs, err = f.f.Readdir(n)
	return dirs, f.fixErr(err)
}

func (f *File) Readdirnames(n int) (names []string, err error) {
	names, err = f.f.Readdirnames(n)
	return names, f.fixErr(err)
}

func (f *File) ReadDir(n int) ([]fs.DirEntry, error) {
	entries, err := f.f.ReadDir(n)
	return entries, f.fixErr(err)
}

func (f *File) Truncate(size int64) error {
	return f.fixErr(f.f.Truncate(size))
}

func (f *File) WriteString(s string) (n int, err error) {
	n, err = f.f.WriteString(s)
	return n, f.fixErr(err)
}

// named returns info under the name name. When the underlying filesystem
// already reports that name, it returns info itself, so callers keep its
// concrete type: os.SameFile works on the result, and Stat and Lstat return
// the same type as the entries ReadDir and Readdir pass through. osfs, like
// os.Lstat, os.Root and os.File.Stat, names a result after the base of the
// path given, and below the root that is the virtual base name too. Only the
// root, which the host knows by its own base name (or "."), is wrapped, so
// its host name does not leak. A wrapped root keeps Sys, so identity checks
// on Sys (device and inode on Unix) still work on it; os.SameFile does not.
func named(info os.FileInfo, name string) os.FileInfo {
	if info.Name() == name {
		return info
	}
	return &fileinfo{info, name}
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
