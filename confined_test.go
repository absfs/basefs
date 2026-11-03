package basefs_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/absfs/absfs"
	"github.com/absfs/basefs"
	"github.com/absfs/memfs"
	"github.com/absfs/osfs"
)

// opener is a memfs that answers OpenRoot as told.
type opener struct {
	*memfs.FileSystem
	confined absfs.SymlinkFileSystem
	err      error
	dir      string
}

func (o *opener) OpenRoot(dir string) (absfs.SymlinkFileSystem, error) {
	o.dir = dir
	return o.confined, o.err
}

func newOpener(t *testing.T) *opener {
	t.Helper()
	mfs, err := memfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}
	if err := mfs.MkdirAll("/r", 0o755); err != nil {
		t.Fatal(err)
	}
	return &opener{FileSystem: mfs}
}

// TestRootOpener checks how basefs uses an underlying filesystem's OpenRoot.
func TestRootOpener(t *testing.T) {
	t.Run("confined", func(t *testing.T) {
		o := newOpener(t)
		// The confined view is another memfs, so where operations go shows.
		view, err := memfs.NewFS()
		if err != nil {
			t.Fatal(err)
		}
		if err := view.MkdirAll("/r", 0o755); err != nil {
			t.Fatal(err)
		}
		o.confined = view
		b, err := basefs.NewFS(o, "/r/")
		if err != nil {
			t.Fatal(err)
		}
		if o.dir != "/r" {
			t.Errorf("OpenRoot got %q, want the cleaned root", o.dir)
		}
		if !basefs.Confined(b) {
			t.Error("Confined = false")
		}
		if basefs.Unwrap(b) != absfs.FileSystem(o) {
			t.Error("Unwrap did not return the filesystem given to NewFS")
		}
		if err := b.Symlink("f", "/l"); err != nil {
			t.Fatal(err)
		}
		if _, err := view.Lstat("/r/l"); err != nil {
			t.Errorf("Symlink did not go through the confined view: %v", err)
		}
		if _, err := o.Lstat("/r/l"); err == nil {
			t.Error("Symlink went to the unconfined filesystem")
		}

		pb, err := basefs.NewFileSystem(o, "/r")
		if err != nil {
			t.Fatal(err)
		}
		if !basefs.Confined(pb) || basefs.Unwrap(pb) != absfs.FileSystem(o) {
			t.Error("NewFileSystem is not confined the same way")
		}
	})
	t.Run("unsupported", func(t *testing.T) {
		o := newOpener(t)
		o.err = &os.PathError{Op: "openroot", Path: "/r", Err: errors.ErrUnsupported}
		b, err := basefs.NewFS(o, "/r")
		if err != nil {
			t.Fatal(err)
		}
		if basefs.Confined(b) {
			t.Error("Confined = true without a confined view")
		}
		if err := b.Mkdir("/d", 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := o.Stat("/r/d"); err != nil {
			t.Errorf("Mkdir did not reach the underlying filesystem: %v", err)
		}
	})
	t.Run("failure", func(t *testing.T) {
		o := newOpener(t)
		o.err = errors.New("no root for you")
		if _, err := basefs.NewFS(o, "/r"); err != o.err {
			t.Errorf("NewFS = %v, want the OpenRoot error", err)
		}
		if _, err := basefs.NewFileSystem(o, "/r"); err != o.err {
			t.Errorf("NewFileSystem = %v, want the OpenRoot error", err)
		}
	})
	t.Run("not basefs", func(t *testing.T) {
		if basefs.Confined(newOpener(t)) {
			t.Error("Confined of a non-basefs filesystem")
		}
	})
	t.Run("permission", func(t *testing.T) {
		// os.Root cannot open a root it cannot read; basefs falls back.
		o := newOpener(t)
		o.err = &os.PathError{Op: "open", Path: "/r", Err: fs.ErrPermission}
		b, err := basefs.NewFS(o, "/r")
		if err != nil {
			t.Fatal(err)
		}
		pb, err := basefs.NewFileSystem(o, "/r")
		if err != nil {
			t.Fatal(err)
		}
		if basefs.Confined(b) || basefs.Confined(pb) {
			t.Error("Confined = true after OpenRoot failed")
		}
		if err := b.Mkdir("/d", 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := o.Stat("/r/d"); err != nil {
			t.Errorf("Mkdir did not reach the underlying filesystem: %v", err)
		}
	})
	t.Run("unconfined", func(t *testing.T) {
		// The Unconfined constructors never ask; a failing OpenRoot shows.
		o := newOpener(t)
		o.err = errors.New("OpenRoot called")
		b, err := basefs.NewFSUnconfined(o, "/r")
		if err != nil {
			t.Fatal(err)
		}
		pb, err := basefs.NewFileSystemUnconfined(o, "/r")
		if err != nil {
			t.Fatal(err)
		}
		if o.dir != "" || basefs.Confined(b) || basefs.Confined(pb) {
			t.Errorf("unconfined basefs called OpenRoot(%q) or is Confined", o.dir)
		}
		if err := b.Close(); err != nil {
			t.Errorf("Close of an unconfined basefs = %v", err)
		}
		if _, err := basefs.NewFSUnconfined(o, "/missing"); err == nil {
			t.Error("NewFSUnconfined accepted a missing root")
		}
		if _, err := basefs.NewFileSystemUnconfined(o, "/missing"); err == nil {
			t.Error("NewFileSystemUnconfined accepted a missing root")
		}
	})
	t.Run("view errors", func(t *testing.T) {
		o, v := newView(t)
		b, err := basefs.NewFS(o, "/r")
		if err != nil {
			t.Fatal(err)
		}

		// The caller's operation name is reported, not the view's, and the
		// view's escape error becomes ErrPathEscapes.
		_, err = b.Stat("/x")
		var pe *os.PathError
		if !errors.As(err, &pe) || pe.Op != "stat" || pe.Path != "/x" || !errors.Is(err, basefs.ErrPathEscapes) {
			t.Errorf("Stat(/x) = %#v, want stat /x: ErrPathEscapes", err)
		}
		err = b.Rename("/x", "/y")
		var le *os.LinkError
		if !errors.As(err, &le) || le.Op != "rename" || !errors.Is(err, basefs.ErrPathEscapes) {
			t.Errorf("Rename = %#v, want rename: ErrPathEscapes", err)
		}

		if err := b.Close(); err != nil || v.closed != 1 {
			t.Errorf("Close = %v, view closed %d times, want once", err, v.closed)
		}
		pb, err := basefs.NewFileSystem(o, "/r")
		if err != nil {
			t.Fatal(err)
		}
		if err := pb.Close(); err != nil || v.closed != 2 {
			t.Errorf("FileSystem.Close = %v, view closed %d times", err, v.closed)
		}
	})
	t.Run("remove root", func(t *testing.T) {
		o, v := newView(t)
		for _, d := range []string{"/r/a/b", "/r/c"} {
			if err := v.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		b, err := basefs.NewFS(o, "/r")
		if err != nil {
			t.Fatal(err)
		}
		if err := b.RemoveAll("/"); err != nil {
			t.Fatal(err)
		}
		if entries, err := v.ReadDir("/r"); err != nil || len(entries) != 0 {
			t.Errorf("after RemoveAll(/), root has %d entries, %v", len(entries), err)
		}

		// A failure to remove a child is reported for the caller's name.
		if err := v.Mkdir("/r/a", 0o755); err != nil {
			t.Fatal(err)
		}
		v.removeErr = errors.New("busy")
		if err := b.RemoveAll("/"); !errors.Is(err, v.removeErr) || !strings.HasPrefix(err.Error(), "remove /:") {
			t.Errorf("RemoveAll(/) = %v, want remove /: busy", err)
		}
		// So is a failure to list the root.
		v.removeErr = nil
		if err := v.RemoveAll("/r"); err != nil {
			t.Fatal(err)
		}
		if err := b.RemoveAll("/"); err == nil {
			t.Error("RemoveAll(/) of a missing root succeeded")
		}
	})
}

// view is a memfs standing in for a confined view: Stat and Rename report
// osfs.ErrPathEscapes under os.Root's operation names, RemoveAll can be made
// to fail, and Close is counted.
type view struct {
	*memfs.FileSystem
	removeErr error
	closed    int
}

func (v *view) Stat(name string) (os.FileInfo, error) {
	if name == "/r" {
		return v.FileSystem.Stat(name)
	}
	return nil, &os.PathError{Op: "statat", Path: name, Err: osfs.ErrPathEscapes}
}

func (v *view) Rename(oldname, newname string) error {
	return &os.LinkError{Op: "renameat", Old: oldname, New: newname, Err: osfs.ErrPathEscapes}
}

func (v *view) RemoveAll(name string) error {
	if v.removeErr != nil {
		return &os.PathError{Op: "unlinkat", Path: name, Err: v.removeErr}
	}
	return v.FileSystem.RemoveAll(name)
}

func (v *view) Close() error {
	v.closed++
	return nil
}

// newView returns an opener whose OpenRoot succeeds with a view holding /r.
func newView(t *testing.T) (*opener, *view) {
	t.Helper()
	o := newOpener(t)
	mfs, err := memfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}
	if err := mfs.MkdirAll("/r", 0o755); err != nil {
		t.Fatal(err)
	}
	v := &view{FileSystem: mfs}
	o.confined = v
	return o, v
}

// goAtLeast125 reports whether the running Go release is 1.25 or later.
func goAtLeast125() bool {
	v, ok := strings.CutPrefix(runtime.Version(), "go1.")
	if !ok {
		return true // a development build
	}
	minor, err := strconv.Atoi(strings.FieldsFunc(v, func(r rune) bool { return r < '0' || r > '9' })[0])
	return err != nil || minor >= 25
}

// TestOsfsConfined checks that basefs confines itself over osfs exactly
// where osfs can: Go 1.25 or later on a Unix system.
func TestOsfsConfined(t *testing.T) {
	e := osEnv(t)
	b := e.symlinkFS(t)
	want := goAtLeast125() && runtime.GOOS != "windows" && runtime.GOOS != "plan9" &&
		runtime.GOOS != "js" && runtime.GOOS != "wasip1"
	if basefs.Confined(b) != want {
		t.Errorf("Confined = %v over osfs with %s on %s, want %v", basefs.Confined(b), runtime.Version(), runtime.GOOS, want)
	}
	if _, ok := basefs.Unwrap(b).(*osfs.FileSystem); !ok {
		t.Errorf("Unwrap = %T, want the *osfs.FileSystem given to NewFS", basefs.Unwrap(b))
	}
	if !want {
		return
	}

	// An absolute link is still followed, inside the root, and an absolute
	// link to a place outside the root, made on the host, is refused.
	if err := b.Symlink("/sub/deep/f.txt", "/abs"); err != nil {
		t.Fatal(err)
	}
	if data, err := b.ReadFile("/abs"); err != nil || string(data) != "deep" {
		t.Errorf("ReadFile(/abs) = %q, %v", data, err)
	}
	if err := b.Symlink("/sub", "/subl"); err != nil {
		t.Fatal(err)
	}
	if data, err := b.ReadFile("/subl/deep/f.txt"); err != nil || string(data) != "deep" {
		t.Errorf("ReadFile(/subl/deep/f.txt) = %q, %v", data, err)
	}
	if err := os.Symlink(e.join(e.parent, "secret"), e.join(e.root, "planted")); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReadFile("/planted"); !errors.Is(err, basefs.ErrPathEscapes) {
		t.Errorf("ReadFile(/planted) = %v, want ErrPathEscapes", err)
	}
}

// confinedOS returns an osfs env and a basefs over its root, skipping the
// test where osfs cannot confine.
func confinedOS(t *testing.T) (*env, *basefs.SymlinkFileSystem) {
	t.Helper()
	e := osEnv(t)
	b := e.symlinkFS(t)
	t.Cleanup(func() { b.Close() })
	if !basefs.Confined(b) {
		t.Skipf("osfs cannot confine with %s on %s", runtime.Version(), runtime.GOOS)
	}
	return e, b
}

// TestOsfsWriteOnlyRoot checks that a root the process can search and write
// but not read (a drop box) still works, unconfined, since os.Root cannot
// open it.
func TestOsfsWriteOnlyRoot(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permissions enforced for this user")
	}
	e := osEnv(t)
	if err := os.Chmod(e.root, 0o333); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(e.root, 0o755) })
	b, err := basefs.NewFS(e.under, e.root)
	if err != nil {
		t.Fatalf("NewFS over a 0333 root: %v", err)
	}
	if basefs.Confined(b) {
		t.Error("Confined = true for a root os.Root cannot open")
	}
	f, err := b.Create("/drop")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := os.Stat(filepath.Join(e.root, "drop")); err != nil {
		t.Error(err)
	}
}

// TestOsfsUnconfined checks that NewFSUnconfined follows a host link out of
// the root, as NewFS did before confinement, where a confined NewFS refuses.
func TestOsfsUnconfined(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	e := osEnv(t)
	if err := os.Symlink("../xfoo", filepath.Join(e.root, "shared")); err != nil {
		t.Fatal(err)
	}
	u, err := basefs.NewFSUnconfined(e.under, e.root)
	if err != nil {
		t.Fatal(err)
	}
	if basefs.Confined(u) {
		t.Error("NewFSUnconfined is Confined")
	}
	if data, err := u.ReadFile("/shared/secret"); err != nil || string(data) != "SECRET" {
		t.Errorf("unconfined ReadFile(/shared/secret) = %q, %v", data, err)
	}
	b := e.symlinkFS(t)
	defer b.Close()
	if !basefs.Confined(b) {
		return
	}
	if _, err := b.ReadFile("/shared/secret"); !errors.Is(err, basefs.ErrPathEscapes) {
		t.Errorf("confined ReadFile(/shared/secret) = %v, want ErrPathEscapes", err)
	}
}

// TestOsfsConfinedBehaviour pins what changes when basefs is confined by
// os.Root.
func TestOsfsConfinedBehaviour(t *testing.T) {
	t.Run("close", func(t *testing.T) {
		_, b := confinedOS(t)
		if err := b.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Stat("/inside.txt"); err == nil {
			t.Error("Stat after Close succeeded")
		}
	})
	t.Run("op names", func(t *testing.T) {
		// Errors report the os operation names, not os.Root's *at ones.
		_, b := confinedOS(t)
		for op, err := range map[string]error{
			"open":     second(b.Open("/missing")),
			"stat":     second(b.Stat("/missing")),
			"lstat":    second(b.Lstat("/missing")),
			"chmod":    b.Chmod("/missing", 0o644),
			"mkdir":    b.Mkdir("/missing/d", 0o755),
			"remove":   b.Remove("/missing"),
			"truncate": b.Truncate("/missing", 0),
			"readlink": second(b.Readlink("/missing")),
		} {
			var pe *os.PathError
			if !errors.As(err, &pe) || pe.Op != op || !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s /missing = %v, want op %q", op, err, op)
			}
		}
	})
	t.Run("remove root", func(t *testing.T) {
		e, b := confinedOS(t)
		if err := b.RemoveAll("/"); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(e.root)
		if err != nil || len(entries) != 0 {
			t.Errorf("root after RemoveAll(/): %d entries, %v; want empty and kept", len(entries), err)
		}
		if _, err := os.Stat(filepath.Join(e.parent, "secret")); err != nil {
			t.Errorf("RemoveAll(/) reached outside the root: %v", err)
		}

		// Unconfined, the root itself goes too, as before confinement.
		u, err := basefs.NewFSUnconfined(e.under, e.root)
		if err != nil {
			t.Fatal(err)
		}
		if err := u.RemoveAll("/"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(e.root); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("unconfined RemoveAll(/) kept the root: %v", err)
		}
	})
	t.Run("root renamed", func(t *testing.T) {
		// A confined basefs is bound to the root directory, not its path.
		e, b := confinedOS(t)
		moved := e.root + ".moved"
		if err := os.Rename(e.root, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(e.root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := b.Mkdir("/new", 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(moved, "new")); err != nil {
			t.Errorf("Mkdir did not follow the moved root: %v", err)
		}
		if _, err := os.Stat(filepath.Join(e.root, "new")); err == nil {
			t.Error("Mkdir went to the new directory at the root's old path")
		}
	})
	t.Run("links judged lexically", func(t *testing.T) {
		// A relative link that climbs out of the root and back in fails, as
		// does an absolute link spelling the root another way; the root's
		// own path works.
		e, b := confinedOS(t)
		for name, target := range map[string]string{
			"back":  "../x/inside.txt",
			"other": e.parent + "/xfoo/../x/inside.txt",
		} {
			if err := os.Symlink(target, filepath.Join(e.root, name)); err != nil {
				t.Fatal(err)
			}
			if _, err := b.ReadFile("/" + name); !errors.Is(err, basefs.ErrPathEscapes) {
				t.Errorf("ReadFile(/%s) via %s = %v, want ErrPathEscapes", name, target, err)
			}
		}
		if err := os.Symlink(filepath.Join(e.root, "inside.txt"), filepath.Join(e.root, "same")); err != nil {
			t.Fatal(err)
		}
		if data, err := b.ReadFile("/same"); err != nil || string(data) != "inside" {
			t.Errorf("ReadFile(/same) = %q, %v", data, err)
		}
	})
}

func second[T any](_ T, err error) error { return err }
