package basefs_test

import (
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/absfs/basefs"
)

// The fuzz targets run every operation against a root parent/x that sits
// beside parent/xfoo/secret ("SECRET") and parent/secret ("PARENT"), on both
// osfs and memfs (see env). For any input they require that no read returns
// either secret, that nothing outside the root changes, and that no error
// reveals the host parent path.

var nameSeeds = []string{
	"../xfoo/secret",
	"../xfoo/secret/",
	"../x/../xfoo/secret",
	"/../secret",
	"sub/../../xfoo/secret",
	"../etc/passwd",
	`..\windows\system32`,
	`..\xfoo\secret`,
	"/absolute/path",
	"foo/../../etc/passwd",
	"./././../etc/passwd",
	"\x00hidden/file",
	"normal/path",
	"inside.txt",
	"sub/deep/f.txt",
	"",
	"/",
	".",
	strings.Repeat("../", 12) + "etc/shadow",
	`\\network\share`,
	`C:\Windows`,
	"c:secret",
	"NUL",
	"symlink/../../../etc/passwd",
	"./../.../../etc/passwd",
}

type fuzzProbe struct {
	t *testing.T
	e *env
	b *basefs.SymlinkFileSystem
}

// fuzzEnvs runs fn once per underlying filesystem with a fresh environment
// and checks afterwards that nothing outside the root changed.
func fuzzEnvs(t *testing.T, fn func(p *fuzzProbe)) {
	for _, e := range envs(t) {
		before := e.outsideSnapshot(t)
		fn(&fuzzProbe{t: t, e: e, b: e.symlinkFS(t)})
		if after := e.outsideSnapshot(t); after != before {
			t.Fatalf("%s: outside of root changed:\nbefore\n%s\nafter\n%s", e.name, before, after)
		}
	}
}

// err fails if err reveals the host parent. The names in a PathError or
// LinkError are the caller's own, so only the wrapped error is inspected.
func (p *fuzzProbe) err(op string, err error) {
	if err == nil {
		return
	}
	inner := err
	var pe *os.PathError
	var le *os.LinkError
	switch {
	case errors.As(err, &pe):
		inner = pe.Err
	case errors.As(err, &le):
		inner = le.Err
	}
	msg := inner.Error()
	if strings.Contains(msg, p.e.parent) || strings.Contains(msg, filepath.ToSlash(p.e.parent)) {
		p.t.Errorf("%s: %s error leaks host path: %v", p.e.name, op, err)
	}
}

// read fails if data came from outside the root.
func (p *fuzzProbe) read(op, name string) {
	data, err := p.b.ReadFile(name)
	p.err(op+" ReadFile", err)
	if s := string(data); s == "SECRET" || s == "PARENT" {
		p.t.Errorf("%s: %s ReadFile(%q) read outside the root: %q", p.e.name, op, name, s)
	}
	f, err := p.b.Open(name)
	p.err(op+" Open", err)
	if err == nil {
		data, _ := io.ReadAll(f)
		_ = f.Close()
		if s := string(data); s == "SECRET" || s == "PARENT" {
			p.t.Errorf("%s: %s Open(%q) read outside the root: %q", p.e.name, op, name, s)
		}
	}
}

// write truncates and writes through name; the outside snapshot catches any
// write that lands outside the root.
func (p *fuzzProbe) write(name string) {
	f, err := p.b.OpenFile(name, os.O_WRONLY|os.O_TRUNC, 0)
	p.err("OpenFile", err)
	if err == nil {
		_, _ = f.Write([]byte("PWNED"))
		_ = f.Close()
	}
}

// virtualOK fails unless v is a clean virtual path.
func (p *fuzzProbe) virtualOK(op, v string) {
	if !strings.HasPrefix(v, "/") || path.Clean(v) != v || strings.Contains(v, p.e.parent) {
		p.t.Errorf("%s: %s returned %q, not a clean virtual path", p.e.name, op, v)
	}
}

// readlink fails if a link target reveals the host parent, and returns it.
func (p *fuzzProbe) readlink(name string) (string, error) {
	got, err := p.b.Readlink(name)
	p.err("Readlink", err)
	if strings.Contains(got, p.e.parent) || strings.Contains(got, filepath.ToSlash(p.e.parent)) {
		p.t.Errorf("%s: Readlink(%q) = %q leaks host path", p.e.name, name, got)
	}
	return got, err
}

// FuzzPathValidation runs every path-taking method on one name.
func FuzzPathValidation(f *testing.F) {
	for _, s := range nameSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		fuzzEnvs(t, func(p *fuzzProbe) {
			b := p.b
			ignore := func(_ interface{}, err error) error { return err }
			p.read("initial", name)
			p.readlink(name)
			p.err("Stat", ignore(b.Stat(name)))
			p.err("Lstat", ignore(b.Lstat(name)))
			p.err("ReadDir", ignore(b.ReadDir(name)))
			p.err("Sub", ignore(b.Sub(name)))
			p.err("Chmod", b.Chmod(name, 0o755))
			p.err("Chown", b.Chown(name, os.Getuid(), os.Getgid()))
			p.err("Lchown", b.Lchown(name, os.Getuid(), os.Getgid()))
			p.err("Chtimes", b.Chtimes(name, time.Unix(1, 0), time.Unix(1, 0)))
			if err := b.Chdir(name); err == nil {
				wd, _ := b.Getwd()
				p.virtualOK("Getwd", wd)
				p.read("after Chdir", "../xfoo/secret")
				_ = b.Chdir("/")
			} else {
				p.err("Chdir", err)
			}
			p.write(name)
			p.err("Truncate", b.Truncate(name, 0))
			if fh, err := b.Create(name); err == nil {
				_ = fh.Close()
			} else {
				p.err("Create", err)
			}
			p.err("Mkdir", b.Mkdir(name, 0o755))
			p.err("MkdirAll", b.MkdirAll(name, 0o755))
			p.err("Remove", b.Remove(name))
			p.err("RemoveAll", b.RemoveAll(name))
		})
	})
}

// FuzzDirectoryRestriction moves entries between a name and another name,
// then tries to read and write through both.
func FuzzDirectoryRestriction(f *testing.F) {
	f.Add("inside.txt", "../xfoo/secret")
	f.Add("sub", "../xfoo")
	f.Add("../xfoo/secret", "stolen")
	f.Add("../secret", "stolen")
	f.Add("good.txt", "../../etc/passwd")
	f.Add("data/file", "../../../../root/.ssh/id_rsa")
	f.Add("user/doc.txt", `..\..\windows\system32`)
	f.Add("app/config", "/absolute/path")
	f.Add("local/bin", "\x00hidden")
	f.Add("normal", "symlink/../../../etc/passwd")
	f.Add("a/b/c", "../../../../../../../../etc/shadow")
	f.Fuzz(func(t *testing.T, a, b string) {
		fuzzEnvs(t, func(p *fuzzProbe) {
			p.err("Rename", p.b.Rename(a, b))
			p.read("after Rename", b)
			p.err("Rename back", p.b.Rename(b, a))
			p.read("after Rename back", a)
			p.write(a)
			p.write(b)
			p.err("MkdirAll", p.b.MkdirAll(b, 0o755))
			p.err("RemoveAll", p.b.RemoveAll(b))
		})
	})
}

// FuzzSymlinkOperations creates a link with an arbitrary target and name in
// a tree that holds no other links. Symlink must either refuse it or store it
// so that Readlink returns the target exactly as given, and reading or
// writing through the link must stay inside the root. (Rearranging links
// afterwards is a documented limitation; see TestSymlinkHostFollowLimits.)
func FuzzSymlinkOperations(f *testing.F) {
	f.Add("link", "../../../etc/passwd")
	f.Add("/a/l", "../xfoo/secret")
	f.Add("/a/l", "../inside.txt")
	f.Add("/a/b/l", "../../xfoo/secret")
	f.Add("/a/b/l", "../../sub/./deep//f.txt")
	f.Add("/a/b/evil", "l/../xfoo/secret")
	f.Add("/a/evil", "up/../secret")
	f.Add("safe", "../../root/.ssh")
	f.Add("symlink", "/absolute/target")
	f.Add("symlink", "/sub/../inside.txt")
	f.Add("symlink", "/sub/./deep//f.txt")
	f.Add("/a/b/l", "/a/../../secret")
	f.Add("user/link", `..\..\windows`)
	f.Add("/l", "/")
	f.Add("/l", "")
	f.Fuzz(func(t *testing.T, link, target string) {
		fuzzEnvs(t, func(p *fuzzProbe) {
			b := p.b
			if err := b.MkdirAll("/a/b", 0o755); err != nil {
				t.Fatal(err)
			}
			err := b.Symlink(target, link)
			p.err("Symlink", err)
			got, rerr := p.readlink(link)
			if err == nil && target != "" {
				want := filepath.ToSlash(target)
				if path.IsAbs(want) && path.Clean(want) != want {
					t.Errorf("%s: Symlink(%q, %q) accepted an absolute target that is not clean", p.e.name, target, link)
				}
				if rerr != nil || filepath.ToSlash(got) != want {
					t.Errorf("%s: Readlink(%q) = %q, %v after Symlink(%q)", p.e.name, link, got, rerr, target)
				}
			}
			p.read("link", link)
			p.write(link)
			p.err("Lchown", b.Lchown(link, os.Getuid(), os.Getgid()))
		})
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
