package basefs

import (
	"os"
	"runtime"
	"testing"

	"github.com/absfs/fstesting"
	"github.com/absfs/osfs"
)

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
