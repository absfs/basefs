//go:build unix

package basefs_test

import (
	"os"
	"syscall"
	"testing"
)

// TestFileInfoRootSys checks that the root, the one info basefs still wraps
// to hide the host's name for it, keeps the host's device and inode in Sys,
// so a layer above can identify it on Unix where os.SameFile cannot.
func TestFileInfoRootSys(t *testing.T) {
	e, fses := osIdentityFSes(t)
	hostInfo, err := os.Stat(e.root)
	if err != nil {
		t.Fatal(err)
	}
	host := hostInfo.Sys().(*syscall.Stat_t)
	for kind, b := range fses {
		info, err := b.Stat("/")
		if err != nil {
			t.Fatal(err)
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatalf("%s: Stat(/).Sys() is %T, want *syscall.Stat_t", kind, info.Sys())
		}
		if st.Dev != host.Dev || st.Ino != host.Ino {
			t.Errorf("%s: Stat(/) is dev %d ino %d, want the root's dev %d ino %d", kind, st.Dev, st.Ino, host.Dev, host.Ino)
		}
	}
}
