package basefs_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/absfs/absfs"
	"github.com/absfs/basefs"
	"github.com/absfs/fstesting"
	"github.com/absfs/osfs"
	"github.com/absfs/osfs/fastwalk"
)

func TestAbsfs(t *testing.T) {
	ofs, err := osfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}

	testpath := os.TempDir()
	abs, err := filepath.Abs(testpath)
	if err != nil {
		t.Fatal(err)
	}
	testpath = abs

	bfs, err := basefs.NewFS(ofs, testpath)
	if err != nil {
		t.Fatal(err)
	}

	var fs absfs.SymlinkFileSystem
	fs = bfs
	_ = fs
}

func TestWalk(t *testing.T) {

	ofs, err := osfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}
	testpath := ".."
	abs, err := filepath.Abs(testpath)
	if err != nil {
		t.Fatal(err)
	}

	testpath = abs

	fs, err := basefs.NewFS(ofs, testpath)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Walk", func(t *testing.T) {
		list := make(map[string]bool)
		count := 0
		err = filepath.Walk(testpath, func(path string, info os.FileInfo, err error) error {
			p := strings.TrimPrefix(path, testpath)
			if p == "" {
				p = "/"
			}
			list[p] = true
			count++
			return nil
		})
		if err != nil {
			t.Error(err)
		}

		count2 := 0
		err = fs.Walk("/", func(path string, info os.FileInfo, err error) error {
			if !list[path] {
				return fmt.Errorf("file not found %q", path)
			}
			delete(list, path)
			count2++
			return nil
		})
		if err != nil {
			t.Error(err)
		}
		if count < 10 || count != count2 {
			t.Errorf("incorrect file count: %d, %d", count, count2)
		}
		if len(list) > 0 {
			i := 0
			for k := range list {
				i++
				if i >= 10 {
					break
				}
				t.Errorf("path not removed %q", k)
			}
		}
	})

	t.Run("FastWalk", func(t *testing.T) {
		list := make(map[string]bool)
		count := 0
		x := sync.Mutex{}
		err = fastwalk.Walk(testpath, func(path string, mode os.FileMode) error {
			p := strings.TrimPrefix(path, testpath)
			if p == "" {
				p = "/"
			}
			x.Lock()
			list[p] = true
			count++
			x.Unlock()
			return nil
		})
		if err != nil {
			t.Error(err)
		}

		count2 := 0
		err = fs.FastWalk("/", func(path string, mode os.FileMode) error {
			x.Lock()
			if !list[path] {
				return fmt.Errorf("file not found %q", path)
			}
			delete(list, path)
			count2++
			x.Unlock()
			return nil
		})
		if err != nil {
			t.Error(err)
		}
		if count < 10 || count != count2 {
			t.Errorf("incorrect file count: %d, %d", count, count2)
		}
		if len(list) > 0 {
			i := 0
			for k := range list {
				i++
				if i >= 10 {
					break
				}
				t.Errorf("path not removed %q", k)
			}
		}
	})
}

func TestBaseFSSuite(t *testing.T) {
	ofs, err := osfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}

	tempdir := ofs.TempDir()
	bfs, err := basefs.NewFS(ofs, tempdir)
	if err != nil {
		t.Fatal(err)
	}

	if bfs.TempDir() == "" {
		t.Fatalf("basefs TempDir() returned empty path")
	}

	features := fstesting.DefaultFeatures()
	// Windows doesn't support Unix-style permissions
	if runtime.GOOS == "windows" {
		features.Permissions = false
	}

	suite := &fstesting.Suite{
		FS:       bfs,
		Features: features,
	}

	suite.Run(t)
}

func remove(target string) {
	target = filepath.Clean(target)
	_, err := os.Lstat(target)
	if err == nil {
		os.Remove(target)
	}
}

func symlinksSetup(target, source string) (func(), error) {
	remove(target)
	remove(source)
	dirs := false
	if strings.HasSuffix(target, "/") {
		dirs = true
	}
	target = filepath.Clean(target)
	source = filepath.Clean(source)
	fn := func() {
		remove(target)
		remove(source)
	}
	if dirs {
		err := os.Mkdir(target, 0755)
		if err != nil {
			return fn, err
		}
	} else {
		err := ioutil.WriteFile(target, []byte("foo"), 0644)
		if err != nil {
			return fn, err
		}
	}

	return fn, nil
}

func TestSymlinks(t *testing.T) {
	var fs absfs.SymlinkFileSystem
	var err error

	fs, err = osfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	bfs, err := basefs.NewFS(fs, cwd)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		FS       absfs.SymlinkFileSystem
		Target   string
		Source   string
		Readlink string
	}{
		{
			FS:       fs,
			Target:   "testdir/",
			Source:   "testdirLinked/",
			Readlink: filepath.Join(cwd, "testdir"),
		},
		{
			FS:       fs,
			Target:   "testfile.txt",
			Source:   "testfileLinked.txt",
			Readlink: filepath.Join(cwd, "testfile.txt"),
		},
		{
			FS:       bfs,
			Target:   "testdir/",
			Source:   "testdirLinked/",
			Readlink: "/testdir",
		},
		{
			FS:       bfs,
			Target:   "testfile.txt",
			Source:   "testfileLinked.txt",
			Readlink: "/testfile.txt",
		},
	}
	for i, test := range tests {
		func() {
			cleanup, err := symlinksSetup(test.Target, test.Source)
			if err != nil {
				t.Error(err)
			}
			defer cleanup()

			target := filepath.Clean(test.Target)
			source := filepath.Clean(test.Source)
			// Symlnk
			err = test.FS.Symlink(target, source)
			if err != nil {
				t.Errorf("Symlink(%q, %q) %s", target, source, err)
			}

			// Readlink
			readlink, err := test.FS.Readlink(source)
			if err != nil {
				t.Error(err)
			}

			if readlink != test.Readlink {
				t.Errorf("%d: Readlink returned incorrect response: %q expected %q", i, readlink, test.Readlink)
			}

			info, err := test.FS.Stat(source)
			if err != nil {
				t.Error(err)
			}
			lInfo, err := test.FS.Lstat(source)
			if err != nil {
				t.Error(err)
			}

			if info.Mode() == lInfo.Mode() {
				t.Errorf("%d: Expected Lstat to differ from Stat: %s == %s", i, info.Mode(), lInfo.Mode())
			}
			if info.Mode()&os.ModeSymlink != 0 {
				t.Errorf("%d: Expected Stat to return link target %s", i, info.Mode())
			}
			if lInfo.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%d: Expected Lstat to return a link %s", i, lInfo.Mode())
			}

			// sourceAbs, err := filepath.Abs(source)
			// if err != nil {
			// 	t.Error(err)
			// }

			// t.Logf("%s\n", fstools.NewFileInfo(test.FS, info, sourceAbs))
			// t.Logf("%s\n", fstools.NewFileInfo(test.FS, lInfo, sourceAbs))
		}()

	}

}
