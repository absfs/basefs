package basefs

import (
	"flag"
	"os"
	"runtime"
	"testing"

	"github.com/absfs/fstesting"
	"github.com/absfs/osfs"
)

// suiteSkip names the one fstesting subtest basefs deliberately fails.
// RelativeSymlink expects Readlink to return "../rel_target.txt" verbatim,
// but basefs stores a climbing relative target as an absolute path so that
// Rename cannot carry it out of the root (see Symlink), and Readlink then
// reports "/.../rel_target.txt". TestSymlinkCannotBeMadeToEscape and
// TestSymlinkTargets pin the actual behaviour. fstesting has no per-subtest
// skip (Suite.RunWithSkips ignores its arguments), so the -skip flag is set
// here unless the caller passed one.
const suiteSkip = `^TestBaseFSSuite$/^Symlinks(#[0-9]+)?$/^RelativeSymlink$`

func TestMain(m *testing.M) {
	flag.Parse()
	if f := flag.Lookup("test.skip"); f != nil && f.Value.String() == "" {
		_ = f.Value.Set(suiteSkip)
	}
	os.Exit(m.Run())
}

// TestBaseFSSuite runs the standard fstesting suite against basefs wrapping osfs.
func TestBaseFSSuite(t *testing.T) {
	// Create a temporary directory for the test.
	// Note: os.MkdirTemp returns a native path (e.g., "C:\Users\..." on Windows).
	tmpDir, err := os.MkdirTemp("", "basefs-fstesting-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create the underlying osfs
	underlying, err := osfs.NewFS()
	if err != nil {
		t.Fatalf("failed to create osfs: %v", err)
	}

	// Create basefs rooted at the temp directory.
	// basefs.NewFS expects a path that the underlying filesystem can stat.
	// When using osfs as the underlying fs, native paths work because osfs
	// detects and handles them. For other underlying filesystems, you may
	// need to convert the path appropriately.
	fs, err := NewFS(underlying, tmpDir)
	if err != nil {
		t.Fatalf("failed to create basefs: %v", err)
	}

	// Use platform-appropriate features
	features := fstesting.OSFeatures()

	// Override case sensitivity based on platform
	if runtime.GOOS == "darwin" {
		// macOS is typically case-insensitive (HFS+/APFS default)
		features.CaseSensitive = false
	}

	suite := &fstesting.Suite{
		FS:       fs,
		Features: features,
	}

	suite.Run(t)
}
