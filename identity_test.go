package basefs_test

import (
	"fmt"
	"io/fs"
	"os"
	"testing"

	"github.com/absfs/absfs"
	"github.com/absfs/basefs"
)

// identityFS is the part of a basefs the identity tests use.
type identityFS interface {
	Stat(name string) (os.FileInfo, error)
	Open(name string) (absfs.File, error)
	ReadDir(name string) ([]fs.DirEntry, error)
}

// osIdentityFSes returns a basefs of each kind over osfs, confined where
// osfs can confine and unconfined, with the env they share. It also makes a
// link /l to inside.txt, through the host so the plain FileSystem has one.
func osIdentityFSes(t *testing.T) (*env, map[string]identityFS) {
	t.Helper()
	e := osEnv(t)
	if err := os.Symlink("inside.txt", e.join(e.root, "l")); err != nil {
		t.Skipf("cannot make a symlink: %v", err)
	}
	unconfined, err := basefs.NewFSUnconfined(e.under, e.root)
	if err != nil {
		t.Fatal(err)
	}
	plainUnconfined, err := basefs.NewFileSystemUnconfined(e.under, e.root)
	if err != nil {
		t.Fatal(err)
	}
	fses := map[string]identityFS{
		"NewFS":                   e.symlinkFS(t),
		"NewFileSystem":           e.plainFS(t),
		"NewFSUnconfined":         unconfined,
		"NewFileSystemUnconfined": plainUnconfined,
	}
	t.Cleanup(func() {
		for _, b := range fses {
			if c, ok := b.(interface{ Close() error }); ok {
				c.Close()
			}
		}
	})
	return e, fses
}

// fileStat is Stat on the open file name.
func fileStat(t *testing.T, b identityFS, name string) os.FileInfo {
	t.Helper()
	f, err := b.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// TestFileInfoIdentity checks that os.SameFile works on the FileInfo values
// basefs returns below its root. basefs used to wrap every Stat, Lstat and
// File.Stat result to rename it, which made os.SameFile false for all of
// them, and left Lstat and ReadDir entries of different types. Layers above,
// such as unionfs, use os.SameFile to tell a case-only rename of a file from
// a rename onto another file, and silently did nothing.
func TestFileInfoIdentity(t *testing.T) {
	e, fses := osIdentityFSes(t)
	hostStat := func(p string) os.FileInfo {
		t.Helper()
		info, err := os.Stat(e.join(e.root, p))
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	hostLstat := func(p string) os.FileInfo {
		t.Helper()
		info, err := os.Lstat(e.join(e.root, p))
		if err != nil {
			t.Fatal(err)
		}
		return info
	}
	for kind, b := range fses {
		t.Run(kind, func(t *testing.T) {
			same := func(what string, got, want os.FileInfo, wantName string) {
				t.Helper()
				if !os.SameFile(got, want) {
					t.Errorf("%s: os.SameFile with the host's info is false (got %T)", what, got)
				}
				if got.Name() != wantName {
					t.Errorf("%s: Name() = %q, want %q", what, got.Name(), wantName)
				}
			}
			for _, c := range []struct{ name, host, base string }{
				{"/inside.txt", "inside.txt", "inside.txt"},
				{"sub", "sub", "sub"},
				{"/sub/deep/", "sub/deep", "deep"},
				{"sub/../sub/deep/f.txt", "sub/deep/f.txt", "f.txt"},
				{"/l", "inside.txt", "l"}, // Stat follows the link
			} {
				info, err := b.Stat(c.name)
				if err != nil {
					t.Fatal(err)
				}
				same(fmt.Sprintf("Stat(%q)", c.name), info, hostStat(c.host), c.base)
				same(fmt.Sprintf("Open(%q).Stat()", c.name), fileStat(t, b, c.name), hostStat(c.host), c.base)
			}

			lfs, ok := b.(*basefs.SymlinkFileSystem)
			if !ok {
				return
			}
			for _, c := range []struct{ name, base string }{
				{"/inside.txt", "inside.txt"},
				{"/sub/deep", "deep"},
				{"/l", "l"}, // Lstat does not follow the link
			} {
				info, err := lfs.Lstat(c.name)
				if err != nil {
					t.Fatal(err)
				}
				same(fmt.Sprintf("Lstat(%q)", c.name), info, hostLstat(c.name[1:]), c.base)
			}
		})
	}
}

// TestFileInfoListingMatchesLstat checks that the infos ReadDir and Readdir
// list are the same type as Lstat's and identify the same files, so a layer
// can match a listed entry against an Lstat result with os.SameFile.
func TestFileInfoListingMatchesLstat(t *testing.T) {
	_, fses := osIdentityFSes(t)
	b := fses["NewFS"].(*basefs.SymlinkFileSystem)
	entries, err := b.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	f, err := b.Open("/")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	listed, err := f.Readdir(-1)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]os.FileInfo{}
	for _, info := range listed {
		byName[info.Name()] = info
	}
	if len(entries) == 0 || len(entries) != len(listed) {
		t.Fatalf("ReadDir listed %d entries, Readdir %d", len(entries), len(listed))
	}
	for _, ent := range entries {
		info, err := ent.Info()
		if err != nil {
			t.Fatal(err)
		}
		l, err := b.Lstat("/" + ent.Name())
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%T", info) != fmt.Sprintf("%T", l) {
			t.Errorf("%s: ReadDir info is %T, Lstat %T", ent.Name(), info, l)
		}
		if !os.SameFile(info, l) {
			t.Errorf("%s: os.SameFile(ReadDir info, Lstat) is false", ent.Name())
		}
		if r := byName[ent.Name()]; r == nil || !os.SameFile(r, l) {
			t.Errorf("%s: os.SameFile(Readdir info, Lstat) is false", ent.Name())
		}
	}
}

// TestFileInfoRootName checks that the root is still named "/" and does not
// reveal the host's name for it, while its Sys passes through.
func TestFileInfoRootName(t *testing.T) {
	for _, e := range envs(t) {
		t.Run(e.name, func(t *testing.T) {
			b := e.symlinkFS(t)
			t.Cleanup(func() { b.Close() })
			host, err := e.under.Stat(e.root)
			if err != nil {
				t.Fatal(err)
			}
			check := func(what string, info os.FileInfo, err error) {
				t.Helper()
				if err != nil {
					t.Fatalf("%s: %v", what, err)
				}
				if info.Name() != "/" {
					t.Errorf("%s: Name() = %q, want /", what, info.Name())
				}
				if !info.IsDir() || info.Mode() != host.Mode() || info.Size() != host.Size() || !info.ModTime().Equal(host.ModTime()) {
					t.Errorf("%s: %v %v, want the root's %v %v", what, info.Mode(), info.ModTime(), host.Mode(), host.ModTime())
				}
				if info.Sys() == nil {
					t.Errorf("%s: Sys() is nil", what)
				}
			}
			for _, name := range []string{"/", "", ".", "sub/.."} {
				info, err := b.Stat(name)
				check(fmt.Sprintf("Stat(%q)", name), info, err)
				info, err = b.Lstat(name)
				check(fmt.Sprintf("Lstat(%q)", name), info, err)
			}
			check("Open(/).Stat()", fileStat(t, b, "/"), nil)
		})
	}
}

// TestFileInfoMemfs checks that over memfs, Stat, Lstat, File.Stat and the
// ReadDir entries below the root are memfs's own infos, carrying its inode.
func TestFileInfoMemfs(t *testing.T) {
	e := memEnv(t)
	b := e.symlinkFS(t)
	host, err := e.under.Stat(e.join(e.root, "inside.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%T", host)
	entries, err := b.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	var listed os.FileInfo
	for _, ent := range entries {
		if ent.Name() == "inside.txt" {
			listed, _ = ent.Info()
		}
	}
	for what, get := range map[string]func() (os.FileInfo, error){
		"Stat":      func() (os.FileInfo, error) { return b.Stat("/inside.txt") },
		"Lstat":     func() (os.FileInfo, error) { return b.Lstat("/inside.txt") },
		"File.Stat": func() (os.FileInfo, error) { return fileStat(t, b, "/inside.txt"), nil },
		"ReadDir":   func() (os.FileInfo, error) { return listed, nil },
	} {
		info, err := get()
		if err != nil || info == nil {
			t.Fatalf("%s: %v, %v", what, info, err)
		}
		if got := fmt.Sprintf("%T", info); got != want {
			t.Errorf("%s returned %s, want memfs's %s", what, got, want)
		}
		if info.Sys() != host.Sys() {
			t.Errorf("%s: Sys() = %v, want the same inode as memfs's %v", what, info.Sys(), host.Sys())
		}
	}
}
