package basefs_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/absfs/absfs"
	"github.com/absfs/fstools"

	"github.com/absfs/basefs"
	"github.com/absfs/fstesting"
	"github.com/absfs/osfs"
)

func TestOpenFile(t *testing.T) {
	// var err error
	// var ofs absfs.FileSystem
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

	testdir, cleanup, err := fstesting.FsTestDir(bfs, bfs.TempDir())
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	maxerrors := 10

	fstesting.AutoTest(0, func(testcase *fstesting.Testcase) error {
		result, err := fstesting.FsTest(bfs, testdir, testcase)
		if err != nil {
			t.Fatal(err)
		}
		Errors := result.Errors

		for op, report := range testcase.Errors {
			if Errors[op] == nil {
				t.Logf("expected: \n%s\n", testcase.Report())
				t.Logf("  result: \n%s\n", result.Report())
				// buff := new(bytes.Buffer)
				// data, _ := json.Marshal(testcase)
				// json.Indent(buff, data, "\t", "  ")
				// t.Logf("testcase: \n%s\n", string(buff.Bytes()))
				// buff = new(bytes.Buffer)
				// data, _ = json.Marshal(result)
				// json.Indent(buff, data, "\t", "  ")
				// t.Logf("result: \n%s\n", string(buff.Bytes()))

				t.Fatalf("%d: On %q got nil but expected to get an err of type (%T)\n", testcase.TestNo, op, testcase.Errors[op].Type())
				continue
			}
			if report.Err == nil {
				if Errors[op].Err == nil {
					continue
				}

				buff := new(bytes.Buffer)
				data, _ := json.Marshal(testcase)
				json.Indent(buff, data, "\t", "  ")
				t.Logf("testcase: \n%s\n", string(buff.Bytes()))
				buff = new(bytes.Buffer)
				data, _ = json.Marshal(result)
				json.Indent(buff, data, "\t", "  ")
				t.Logf("result: \n%s\n", string(buff.Bytes()))

				t.Fatalf("%d: On %q expected `err == nil` but got err: (%T) %q\n%s", testcase.TestNo, op, Errors[op].Type(), Errors[op].String(), Errors[op].Stack())
				maxerrors--
				continue
			}

			if Errors[op].Err == nil {
				buff := new(bytes.Buffer)
				data, _ := json.Marshal(testcase)
				json.Indent(buff, data, "\t", "  ")
				t.Logf("testcase: \n%s\n", string(buff.Bytes()))
				buff = new(bytes.Buffer)
				data, _ = json.Marshal(result)
				json.Indent(buff, data, "\t", "  ")
				t.Logf("result: \n%s\n", string(buff.Bytes()))

				t.Fatalf("%d: On %q got `err == nil` but expected err: (%T) %q\n%s", testcase.TestNo, op, testcase.Errors[op].Type(), testcase.Errors[op].String(), Errors[op].Stack())
				maxerrors--
			}
			if !report.TypesEqual(Errors[op]) {
				buff := new(bytes.Buffer)
				data, _ := json.Marshal(testcase)
				json.Indent(buff, data, "\t", "  ")
				t.Logf("testcase: \n%s\n", string(buff.Bytes()))
				buff = new(bytes.Buffer)
				data, _ = json.Marshal(result)
				json.Indent(buff, data, "\t", "  ")
				t.Logf("result: \n%s\n", string(buff.Bytes()))

				t.Fatalf("%d: On %q got different error types, expected (%T) but got (%T)\n%s", testcase.TestNo, op, testcase.Errors[op].Type(), Errors[op].Type(), Errors[op].Stack())
				maxerrors--
			}
			if !report.Equal(Errors[op]) {
				buff := new(bytes.Buffer)
				data, _ := json.Marshal(testcase)
				json.Indent(buff, data, "\t", "  ")
				t.Logf("testcase: \n%s\n", string(buff.Bytes()))
				buff = new(bytes.Buffer)
				data, _ = json.Marshal(result)
				json.Indent(buff, data, "\t", "  ")
				t.Logf("result: \n%s\n", string(buff.Bytes()))

				t.Fatalf("%d: On %q got different error values, expected %q but got %q\n%s", testcase.TestNo, op, testcase.Errors[op], Errors[op].String(), Errors[op].Stack())
				maxerrors--
			}

			if maxerrors < 1 {
				t.Fatal("too many errors")
			}
			fmt.Printf("  %10d Tests\r", testcase.TestNo)
		}
		// if testcase.TestNo > 10 {
		// 	return errors.New("stop")
		// }
		// fmt.Printf("  %10d Testcases\r", testcase.TestNo)
		return nil
	})
	if err != nil && err.Error() != "stop" {
		t.Fatal(err)
	}
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

			sourceAbs, err := filepath.Abs(source)
			if err != nil {
				t.Error(err)
			}

			t.Logf("%s\n", fstools.NewFileInfo(test.FS, info, sourceAbs))
			t.Logf("%s\n", fstools.NewFileInfo(test.FS, lInfo, sourceAbs))
		}()

	}

}
