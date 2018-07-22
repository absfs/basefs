package basefs_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/absfs/absfs"
	"github.com/absfs/basefs"
	"github.com/absfs/fstesting"
	"github.com/absfs/osfs"
)

func TestOpenFile(t *testing.T) {
	var err error
	var ofs absfs.FileSystem
	ofs, err = osfs.NewFS()
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

	fstesting.AutoTest(func(testcase *fstesting.Testcase) error {
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
