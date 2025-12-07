package basefs_test

import (
	"fmt"
	"log"
	"os"

	"github.com/absfs/basefs"
	"github.com/absfs/osfs"
)

// ExampleNewFS demonstrates creating a basefs filesystem that constrains
// access to a specific subdirectory.
func ExampleNewFS() {
	// Create an OS filesystem
	ofs, err := osfs.NewFS()
	if err != nil {
		log.Fatal(err)
	}

	// Get the system temp directory (works cross-platform)
	tmpdir := os.TempDir()

	// Create a basefs constrained to temp directory
	bfs, err := basefs.NewFS(ofs, tmpdir)
	if err != nil {
		log.Fatal(err)
	}

	// All operations are now relative to the temp directory
	// This creates a file in the temp directory
	f, err := bfs.Create("/example.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer bfs.Remove("/example.txt")
	defer f.Close()

	// Write some data
	_, err = f.Write([]byte("Hello from basefs!\n"))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("File created successfully")
	// Output: File created successfully
}

// ExampleNewFileSystem demonstrates the non-symlink filesystem interface.
func ExampleNewFileSystem() {
	// Create an OS filesystem
	ofs, err := osfs.NewFS()
	if err != nil {
		log.Fatal(err)
	}

	// Get the system temp directory (works cross-platform)
	tmpdir := os.TempDir()

	// Create a basefs constrained to temp directory
	bfs, err := basefs.NewFileSystem(ofs, tmpdir)
	if err != nil {
		log.Fatal(err)
	}

	// Create and write to a file
	f, err := bfs.Create("/test.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer bfs.Remove("/test.txt")

	_, err = f.WriteString("Testing basefs\n")
	if err != nil {
		log.Fatal(err)
	}
	f.Close()

	// Read the file back
	f, err = bfs.Open("/test.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	data := make([]byte, 100)
	n, err := f.Read(data)
	if err != nil && err.Error() != "EOF" {
		log.Fatal(err)
	}

	fmt.Printf("Read %d bytes\n", n)
	// Output: Read 15 bytes
}

// ExampleSymlinkFileSystem_path_security demonstrates that basefs prevents
// directory traversal attacks.
func Example_pathSecurity() {
	// Create an OS filesystem
	ofs, err := osfs.NewFS()
	if err != nil {
		log.Fatal(err)
	}

	// Get temp directory
	tmpdir := os.TempDir()

	// Create a basefs constrained to temp directory
	bfs, err := basefs.NewFS(ofs, tmpdir)
	if err != nil {
		log.Fatal(err)
	}

	// Try to access a file outside the base directory using ../
	// This will fail safely
	_, err = bfs.Open("/../etc/passwd")
	if err != nil {
		fmt.Println("Access denied: cannot escape base directory")
	}

	// Output: Access denied: cannot escape base directory
}

// ExampleUnwrap demonstrates unwrapping a basefs to get the underlying filesystem.
func ExampleUnwrap() {
	ofs, err := osfs.NewFS()
	if err != nil {
		log.Fatal(err)
	}

	tmpdir := os.TempDir()
	bfs, err := basefs.NewFS(ofs, tmpdir)
	if err != nil {
		log.Fatal(err)
	}

	// Unwrap to get the underlying filesystem
	underlying := basefs.Unwrap(bfs)

	fmt.Printf("Unwrapped: %T\n", underlying)
	// Output: Unwrapped: *osfs.FileSystem
}

// ExamplePrefix demonstrates getting the prefix path from a basefs.
func ExamplePrefix() {
	ofs, err := osfs.NewFS()
	if err != nil {
		log.Fatal(err)
	}

	tmpdir := os.TempDir()
	bfs, err := basefs.NewFS(ofs, tmpdir)
	if err != nil {
		log.Fatal(err)
	}

	// Get the prefix (base directory)
	prefix := basefs.Prefix(bfs)

	// Verify the prefix matches what we set
	if prefix == tmpdir {
		fmt.Println("Prefix matches temp directory")
	}
	// Output: Prefix matches temp directory
}

// ExampleSymlinkFileSystem_Walk demonstrates directory traversal using Walk.
func ExampleSymlinkFileSystem_Walk() {
	ofs, err := osfs.NewFS()
	if err != nil {
		log.Fatal(err)
	}

	// Create an isolated temp directory for this test
	tmpdir, err := os.MkdirTemp("", "basefs-walk-example-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmpdir)

	bfs, err := basefs.NewFS(ofs, tmpdir)
	if err != nil {
		log.Fatal(err)
	}

	// Create some test files, ensuring they are properly closed
	f1, err := bfs.Create("/walk-test-1.txt")
	if err != nil {
		log.Fatal(err)
	}
	if err := f1.Sync(); err != nil {
		log.Fatal(err)
	}
	f1.Close()

	f2, err := bfs.Create("/walk-test-2.txt")
	if err != nil {
		log.Fatal(err)
	}
	if err := f2.Sync(); err != nil {
		log.Fatal(err)
	}
	f2.Close()

	// Walk the filesystem
	count := 0
	err = bfs.Walk("/", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && len(path) > 10 && path[:10] == "/walk-test" {
			count++
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Found %d test files\n", count)
	// Output: Found 2 test files
}
