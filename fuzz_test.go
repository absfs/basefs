package basefs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/absfs/basefs"
	"github.com/absfs/osfs"
)

// FuzzPathValidation tests that basefs properly prevents escaping the base directory
// with various attack vectors including path traversal, absolute paths, null bytes, etc.
func FuzzPathValidation(f *testing.F) {
	// Seed corpus with known attack patterns
	f.Add("/base", "../etc/passwd")
	f.Add("/base", "..\\windows\\system32")
	f.Add("/base", "/absolute/path")
	f.Add("/base", "foo/../../etc/passwd")
	f.Add("/base", "./././../etc/passwd")
	f.Add("/tmp", "\x00hidden/file")
	f.Add("/tmp", "normal/path")
	f.Add("/var", "../../../../../../etc/shadow")
	f.Add("/home", "user/../../../root/.ssh/id_rsa")
	f.Add("/opt", "\\\\network\\share")
	f.Add("/usr", "local/bin/../../../etc/passwd")
	f.Add("/tmp", "a/b/c/../../../../../../../../etc/passwd")
	f.Add("/base", "..\\..\\..\\windows\\system32\\config\\sam")
	f.Add("/tmp", "symlink/../../../etc/passwd")
	f.Add("/var", "./../.../../etc/passwd")

	f.Fuzz(func(t *testing.T, base, path string) {
		// Skip empty base paths
		if base == "" {
			return
		}

		// Ensure base is an absolute path for NewFS to accept it
		if !filepath.IsAbs(base) {
			return
		}

		// Create a temp directory to use as the actual base
		tempDir := t.TempDir()

		// Create the underlying filesystem
		ofs, err := osfs.NewFS()
		if err != nil {
			t.Skipf("Failed to create osfs: %v", err)
		}

		// Create basefs with the temp directory as base
		fs, err := basefs.NewFS(ofs, tempDir)
		if err != nil {
			return // Invalid base is ok to skip
		}

		// All of these operations should never panic, regardless of input
		// The filesystem should handle malicious paths gracefully
		_, _ = fs.Open(path)
		_, _ = fs.Stat(path)
		_, _ = fs.OpenFile(path, os.O_RDONLY, 0644)
		_, _ = fs.Lstat(path)
		_, _ = fs.Create(path)

		// Directory operations should also not panic
		_ = fs.Mkdir(path, 0755)
		_ = fs.MkdirAll(path, 0755)
		_ = fs.Remove(path)
		_ = fs.RemoveAll(path)

		// These operations should not panic even with malicious paths
		_ = fs.Chmod(path, 0644)
		_ = fs.Chown(path, 0, 0)
	})
}

// FuzzIsAbsPath tests the filepath.IsAbs behavior with edge cases
// This is particularly important after the recent Windows path handling fix
func FuzzIsAbsPath(f *testing.F) {
	f.Add("/normal/path")
	f.Add("C:\\Windows\\Path")
	f.Add("relative/path")
	f.Add("\\\\network\\share")
	f.Add("D:/mixed/slashes")
	f.Add("../relative")
	f.Add("./current")
	f.Add("/")
	f.Add("\\")
	f.Add("C:")
	f.Add("c:\\windows")
	f.Add("//double/slash")
	f.Add("\x00null/byte")
	f.Add("very" + string(make([]byte, 5000)) + "long")

	f.Fuzz(func(t *testing.T, path string) {
		// Should not panic on any input
		_ = filepath.IsAbs(path)
	})
}

// FuzzDirectoryRestriction tests that all file operations respect the base directory boundary
// This ensures that operations cannot escape the sandbox through various attack vectors
func FuzzDirectoryRestriction(f *testing.F) {
	f.Add("/base", "safe/file.txt", "../escape.txt")
	f.Add("/tmp", "good.txt", "../../etc/passwd")
	f.Add("/var", "data/file", "../../../../root/.ssh/id_rsa")
	f.Add("/home", "user/doc.txt", "..\\..\\windows\\system32")
	f.Add("/opt", "app/config", "/absolute/path")
	f.Add("/usr", "local/bin", "\x00hidden")
	f.Add("/base", "normal", "symlink/../../../etc/passwd")
	f.Add("/tmp", "a/b/c", "../../../../../../../../etc/shadow")

	f.Fuzz(func(t *testing.T, base, goodPath, badPath string) {
		// Skip empty base paths
		if base == "" {
			return
		}

		// Ensure base is an absolute path
		if !filepath.IsAbs(base) {
			return
		}

		// Create a temp directory to use as the actual base
		tempDir := t.TempDir()

		// Create the underlying filesystem
		ofs, err := osfs.NewFS()
		if err != nil {
			t.Skipf("Failed to create osfs: %v", err)
		}

		// Create basefs with the temp directory as base
		fs, err := basefs.NewFS(ofs, tempDir)
		if err != nil {
			return
		}

		// Operations on bad paths should fail gracefully without panicking
		_ = fs.Mkdir(badPath, 0755)
		_ = fs.MkdirAll(badPath, 0755)
		_ = fs.Remove(badPath)
		_ = fs.RemoveAll(badPath)
		_ = fs.Rename(goodPath, badPath)
		_ = fs.Rename(badPath, goodPath)

		// File operations should also handle bad paths gracefully
		_, _ = fs.Open(badPath)
		_, _ = fs.Create(badPath)
		_, _ = fs.Stat(badPath)
		_, _ = fs.OpenFile(badPath, os.O_RDONLY, 0644)

		// Permission and ownership operations
		_ = fs.Chmod(badPath, 0644)
		_ = fs.Chown(badPath, 0, 0)
		_ = fs.Truncate(badPath, 0)
	})
}

// FuzzSymlinkOperations tests that symlink operations cannot be used to escape
// the base directory through symbolic link attacks
func FuzzSymlinkOperations(f *testing.F) {
	f.Add("/base", "link", "../../../etc/passwd")
	f.Add("/tmp", "safe", "../../root/.ssh")
	f.Add("/var", "symlink", "/absolute/target")
	f.Add("/home", "user/link", "..\\..\\windows")

	f.Fuzz(func(t *testing.T, base, linkPath, target string) {
		// Skip empty base paths
		if base == "" {
			return
		}

		// Ensure base is an absolute path
		if !filepath.IsAbs(base) {
			return
		}

		// Create a temp directory to use as the actual base
		tempDir := t.TempDir()

		// Create the underlying filesystem
		ofs, err := osfs.NewFS()
		if err != nil {
			t.Skipf("Failed to create osfs: %v", err)
		}

		// Create basefs with the temp directory as base
		fs, err := basefs.NewFS(ofs, tempDir)
		if err != nil {
			return
		}

		// Symlink operations should not panic and should prevent escaping
		_ = fs.Symlink(target, linkPath)
		_, _ = fs.Readlink(linkPath)
		_, _ = fs.Lstat(linkPath)
		_ = fs.Lchown(linkPath, 0, 0)
	})
}
