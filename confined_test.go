package basefs_test

import (
	"errors"
	"os"
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
