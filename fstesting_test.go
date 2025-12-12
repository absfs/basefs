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
	// Create a temporary directory for the test
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

	// Create basefs rooted at the temp directory
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
