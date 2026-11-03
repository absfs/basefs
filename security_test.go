package basefs_test

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/absfs/absfs"
	"github.com/absfs/basefs"
	"github.com/absfs/memfs"
	"github.com/absfs/osfs"
)

// env is a base directory "x" with a sibling "xfoo" whose name starts with
// the base's name, both inside parent, on some underlying filesystem.
//
//	parent/
//	  secret            "PARENT"
//	  xfoo/secret       "SECRET"
//	  x/                <- basefs root
//	    inside.txt      "inside"
//	    sub/deep/f.txt  "deep"
type env struct {
	name   string
	under  absfs.SymlinkFileSystem
	parent string
	root   string
	join   func(elem ...string) string
}

func (e *env) write(t *testing.T, p, data string) {
	t.Helper()
	f, err := e.under.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func (e *env) populate(t *testing.T) {
	t.Helper()
	for _, d := range []string{e.join(e.root, "sub", "deep"), e.join(e.parent, "xfoo")} {
		if err := e.under.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.write(t, e.join(e.parent, "secret"), "PARENT")
	e.write(t, e.join(e.parent, "xfoo", "secret"), "SECRET")
	e.write(t, e.join(e.root, "inside.txt"), "inside")
	e.write(t, e.join(e.root, "sub", "deep", "f.txt"), "deep")
}

// snapshot records every entry under parent with its mode, size, mtime and
// contents, so a test can prove nothing outside the root changed.
func (e *env) snapshot(t *testing.T) map[string]string {
	t.Helper()
	return e.walkParent(t, true)
}

// outsideSnapshot is a snapshot of everything under parent except the root
// and its contents, rendered as one comparable string.
func (e *env) outsideSnapshot(t *testing.T) string {
	t.Helper()
	snap := e.walkParent(t, false)
	keys := make([]string, 0, len(snap))
	for k := range snap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s=%s\n", k, snap[k])
	}
	return sb.String()
}

func (e *env) walkParent(t *testing.T, intoRoot bool) map[string]string {
	t.Helper()
	out := map[string]string{}
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := e.under.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, ent := range entries {
			p := e.join(dir, ent.Name())
			if !intoRoot && p == e.root {
				continue
			}
			info, err := e.under.Lstat(p)
			if err != nil {
				t.Fatal(err)
			}
			desc := fmt.Sprintf("%v %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano())
			switch {
			case info.IsDir():
				walk(p)
			case info.Mode().IsRegular():
				data, err := e.under.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				desc += " " + string(data)
			}
			out[p] = desc
		}
	}
	walk(e.parent)
	return out
}

func osEnv(t *testing.T) *env {
	ofs, err := osfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	e := &env{name: "osfs", under: ofs, parent: parent, root: filepath.Join(parent, "x"), join: filepath.Join}
	e.populate(t)
	return e
}

func memEnv(t *testing.T) *env {
	mfs, err := memfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}
	e := &env{name: "memfs", under: mfs, parent: "/hostparent", root: "/hostparent/x", join: path.Join}
	e.populate(t)
	return e
}

func envs(t *testing.T) []*env {
	return []*env{osEnv(t), memEnv(t)}
}

func (e *env) symlinkFS(t *testing.T) *basefs.SymlinkFileSystem {
	t.Helper()
	b, err := basefs.NewFS(e.under, e.root)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (e *env) plainFS(t *testing.T) *basefs.FileSystem {
	t.Helper()
	b, err := basefs.NewFileSystem(e.under, e.root)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// escapeNames all resolve, lexically, outside the root.
func escapeNames(e *env) []string {
	names := []string{
		"..",
		"../",
		"/..",
		"/../",
		"../xfoo",
		"../xfoo/",
		"../xfoo/secret",
		"../xfoo/secret/",
		"/../xfoo/secret",
		"./../xfoo",
		"../secret",
		"../x",
		"../x/../xfoo/secret",
		"../../..",
		"a/../../xfoo/secret",
		"sub/../../xfoo/secret",
		"/sub/deep/../../../xfoo/secret",
		"sub/deep/../../../../../../etc/passwd",
		strings.Repeat("../", 40) + "etc/passwd",
		"//../xfoo/secret",
	}
	if runtime.GOOS == "windows" {
		names = append(names,
			`..\xfoo\secret`,
			`\..\xfoo\secret`,
			`sub\..\..\xfoo`,
			`sub\deep\..\..\..\secret`,
			`..\xfoo\`,
			`C:secret`,
			`C:\Windows`,
			`c:/Windows`,
			`\\server\share\x`,
			`//server/share/x`,
			`NUL`,
			`sub/CON`,
			`sub\aux.`,
			`CONIN$`,
			`inside.txt:stream`,
			`sub\f::$DATA`,
		)
		if e.name == "osfs" {
			sib := filepath.Join(e.parent, "xfoo", "secret")
			names = append(names, sib, filepath.ToSlash(sib), `\\?\`+sib)
		}
	}
	return names
}

type escapeOp struct {
	name   string
	op     string
	link   bool // returns *os.LinkError
	newArg bool // name is the second argument of a two-name op
	call   func(b absfs.FileSystem, name string) error
}

func fsOps() []escapeOp {
	ignore := func(_ interface{}, err error) error { return err }
	return []escapeOp{
		{name: "Open", op: "open", call: func(b absfs.FileSystem, n string) error { return ignore(b.Open(n)) }},
		{name: "OpenFile", op: "open", call: func(b absfs.FileSystem, n string) error {
			return ignore(b.OpenFile(n, os.O_RDWR|os.O_CREATE, 0o644))
		}},
		{name: "Create", op: "open", call: func(b absfs.FileSystem, n string) error { return ignore(b.Create(n)) }},
		{name: "Stat", op: "stat", call: func(b absfs.FileSystem, n string) error { return ignore(b.Stat(n)) }},
		{name: "Mkdir", op: "mkdir", call: func(b absfs.FileSystem, n string) error { return b.Mkdir(n, 0o755) }},
		{name: "MkdirAll", op: "mkdir", call: func(b absfs.FileSystem, n string) error { return b.MkdirAll(n, 0o755) }},
		{name: "Remove", op: "remove", call: func(b absfs.FileSystem, n string) error { return b.Remove(n) }},
		{name: "RemoveAll", op: "remove", call: func(b absfs.FileSystem, n string) error { return b.RemoveAll(n) }},
		{name: "Chmod", op: "chmod", call: func(b absfs.FileSystem, n string) error { return b.Chmod(n, 0o777) }},
		{name: "Chtimes", op: "chtimes", call: func(b absfs.FileSystem, n string) error {
			return b.Chtimes(n, time.Unix(1, 0), time.Unix(1, 0))
		}},
		{name: "Chown", op: "chown", call: func(b absfs.FileSystem, n string) error { return b.Chown(n, os.Getuid(), os.Getgid()) }},
		{name: "Truncate", op: "truncate", call: func(b absfs.FileSystem, n string) error { return b.Truncate(n, 0) }},
		{name: "ReadDir", op: "open", call: func(b absfs.FileSystem, n string) error { return ignore(b.ReadDir(n)) }},
		{name: "ReadFile", op: "open", call: func(b absfs.FileSystem, n string) error { return ignore(b.ReadFile(n)) }},
		{name: "Sub", op: "sub", call: func(b absfs.FileSystem, n string) error { return ignore(b.Sub(n)) }},
		{name: "Chdir", op: "chdir", call: func(b absfs.FileSystem, n string) error { return b.Chdir(n) }},
		{name: "RenameOld", op: "rename", link: true, call: func(b absfs.FileSystem, n string) error {
			return b.Rename(n, "/renamed")
		}},
		{name: "RenameNew", op: "rename", link: true, newArg: true, call: func(b absfs.FileSystem, n string) error {
			return b.Rename("/inside.txt", n)
		}},
	}
}

func symlinkOps() []escapeOp {
	sl := func(b absfs.FileSystem) absfs.SymlinkFileSystem { return b.(absfs.SymlinkFileSystem) }
	return []escapeOp{
		{name: "Lstat", op: "lstat", call: func(b absfs.FileSystem, n string) error {
			_, err := sl(b).Lstat(n)
			return err
		}},
		{name: "Lchown", op: "lchown", call: func(b absfs.FileSystem, n string) error {
			return sl(b).Lchown(n, os.Getuid(), os.Getgid())
		}},
		{name: "Readlink", op: "readlink", call: func(b absfs.FileSystem, n string) error {
			_, err := sl(b).Readlink(n)
			return err
		}},
		{name: "SymlinkNew", op: "symlink", link: true, newArg: true, call: func(b absfs.FileSystem, n string) error {
			return sl(b).Symlink("/inside.txt", n)
		}},
		// The target is checked too: every escape name escapes from "/".
		{name: "SymlinkOld", op: "symlink", link: true, call: func(b absfs.FileSystem, n string) error {
			return sl(b).Symlink(n, "/link")
		}},
	}
}

func checkEscapeErr(t *testing.T, e *env, kind string, op escapeOp, name string, err error) {
	t.Helper()
	op.name = kind + "." + op.name
	if !errors.Is(err, basefs.ErrPathEscapes) {
		t.Errorf("%s(%q) = %v, want ErrPathEscapes", op.name, name, err)
		return
	}
	// The caller's own name is echoed back; nothing else may mention the host.
	if strings.Contains(strings.ReplaceAll(err.Error(), name, ""), e.parent) {
		t.Errorf("%s(%q) error leaks host path: %v", op.name, name, err)
	}
	if op.link {
		var le *os.LinkError
		if !errors.As(err, &le) {
			t.Errorf("%s(%q) error %T, want *os.LinkError", op.name, name, err)
			return
		}
		got := le.Old
		if op.newArg {
			got = le.New
		}
		if le.Op != op.op || got != name {
			t.Errorf("%s(%q) LinkError{Op:%q Old:%q New:%q}", op.name, name, le.Op, le.Old, le.New)
		}
		return
	}
	var pe *os.PathError
	if !errors.As(err, &pe) {
		t.Errorf("%s(%q) error %T, want *os.PathError", op.name, name, err)
		return
	}
	if pe.Op != op.op || pe.Path != name {
		t.Errorf("%s(%q) PathError{Op:%q Path:%q}, want {%q %q}", op.name, name, pe.Op, pe.Path, op.op, name)
	}
}

func TestEscapeAttempts(t *testing.T) {
	for _, e := range envs(t) {
		t.Run(e.name, func(t *testing.T) {
			before := e.snapshot(t)
			targets := []struct {
				kind string
				fs   absfs.FileSystem
				ops  []escapeOp
			}{
				{"SymlinkFileSystem", e.symlinkFS(t), append(fsOps(), symlinkOps()...)},
				{"FileSystem", e.plainFS(t), fsOps()},
			}
			for _, tgt := range targets {
				for _, op := range tgt.ops {
					for _, name := range escapeNames(e) {
						err := op.call(tgt.fs, name)
						checkEscapeErr(t, e, tgt.kind, op, name, err)
					}
				}
				if wd, _ := tgt.fs.Getwd(); wd != "/" {
					t.Errorf("%s: Getwd after failed Chdir = %q, want /", tgt.kind, wd)
				}
			}
			after := e.snapshot(t)
			if fmt.Sprint(before) != fmt.Sprint(after) {
				t.Errorf("filesystem changed by escape attempts:\nbefore %v\nafter  %v", before, after)
			}
		})
	}
}

func TestSiblingPrefixRegression(t *testing.T) {
	for _, e := range envs(t) {
		t.Run(e.name, func(t *testing.T) {
			for _, root := range []string{e.root, e.root + "/"} {
				b, err := basefs.NewFS(e.under, root)
				if err != nil {
					t.Fatal(err)
				}
				data, err := b.ReadFile("../xfoo/secret")
				if err == nil || string(data) == "SECRET" {
					t.Fatalf("root %q: read sibling secret: %q, %v", root, data, err)
				}
				if !errors.Is(err, basefs.ErrPathEscapes) {
					t.Errorf("root %q: err = %v, want ErrPathEscapes", root, err)
				}
				if data, err := b.ReadFile("inside.txt"); err != nil || string(data) != "inside" {
					t.Errorf("root %q: ReadFile(inside.txt) = %q, %v", root, data, err)
				}
				if got := basefs.Prefix(b); got != e.root {
					t.Errorf("Prefix = %q, want cleaned %q", got, e.root)
				}
			}
		})
	}
}

func TestNamesInsideRoot(t *testing.T) {
	names := map[string]string{
		"inside.txt":                 "inside",
		"/inside.txt":                "inside",
		"./inside.txt":               "inside",
		"/./inside.txt":              "inside",
		"sub/../inside.txt":          "inside",
		"/sub/deep/../../inside.txt": "inside",
		"sub/deep/f.txt":             "deep",
		"/sub//deep/./f.txt":         "deep",
	}
	dirs := []string{"", ".", "/", "./", "sub", "sub/", "/sub/deep/", "sub/deep/..", "/sub/.."}
	if runtime.GOOS == "windows" {
		names[`sub\deep\f.txt`] = "deep"
		names[`\sub\..\inside.txt`] = "inside"
		dirs = append(dirs, `sub\`, `\`)
	} else {
		// On Windows "//inside.txt" is a UNC volume and is rejected.
		names["//inside.txt"] = "inside"
	}
	for _, e := range envs(t) {
		t.Run(e.name, func(t *testing.T) {
			b := e.symlinkFS(t)
			for name, want := range names {
				data, err := b.ReadFile(name)
				if err != nil || string(data) != want {
					t.Errorf("ReadFile(%q) = %q, %v; want %q", name, data, err, want)
				}
			}
			for _, name := range dirs {
				info, err := b.Stat(name)
				if err != nil || !info.IsDir() {
					t.Errorf("Stat(%q) = %v, %v; want a directory", name, info, err)
				}
			}
			if info, err := b.Stat("/"); err != nil || info.Name() != "/" {
				t.Errorf("Stat(/).Name() = %v, %v; want /", info, err)
			}
			if info, err := b.Lstat("sub/"); err != nil || info.Name() != "sub" {
				t.Errorf("Lstat(sub/).Name() = %v, %v; want sub", info, err)
			}
		})
	}
}

// TestHostAbsolutePath shows that a host path is just a name inside the root.
func TestHostAbsolutePath(t *testing.T) {
	for _, e := range envs(t) {
		t.Run(e.name, func(t *testing.T) {
			b := e.symlinkFS(t)
			name := e.join(e.parent, "xfoo", "secret")
			data, err := b.ReadFile(name)
			if err == nil || string(data) == "SECRET" {
				t.Fatalf("ReadFile(%q) = %q, %v", name, data, err)
			}
			if runtime.GOOS == "windows" && e.name == "osfs" {
				if !errors.Is(err, basefs.ErrPathEscapes) {
					t.Errorf("ReadFile(%q) err = %v, want ErrPathEscapes", name, err)
				}
				return
			}
			var pe *os.PathError
			if !errors.Is(err, fs.ErrNotExist) || !errors.As(err, &pe) || pe.Path != name {
				t.Errorf("ReadFile(%q) err = %#v, want not-exist PathError for the caller's name", name, err)
			}
			// Creating the host path creates it under the root.
			if err := b.MkdirAll(path.Dir(filepath.ToSlash(name)), 0o755); err != nil {
				t.Fatal(err)
			}
			if _, err := e.under.Stat(e.join(e.root, path.Dir(name))); err != nil {
				t.Errorf("host-path MkdirAll did not land under the root: %v", err)
			}
		})
	}
}

func TestRootIsSlash(t *testing.T) {
	e := memEnv(t)
	b, err := basefs.NewFS(e.under, "/")
	if err != nil {
		t.Fatal(err)
	}
	if data, err := b.ReadFile("/hostparent/xfoo/secret"); err != nil || string(data) != "SECRET" {
		t.Errorf("ReadFile = %q, %v", data, err)
	}
	if _, err := b.Stat("/.."); !errors.Is(err, basefs.ErrPathEscapes) {
		t.Errorf("Stat(/..) err = %v, want ErrPathEscapes", err)
	}
	if got := b.TempDir(); got != "/tmp" {
		t.Errorf("TempDir = %q", got)
	}
	if err := b.Symlink("/hostparent/x/inside.txt", "/abs"); err != nil {
		t.Fatal(err)
	}
	if got, err := b.Readlink("/abs"); err != nil || got != "/hostparent/x/inside.txt" {
		t.Errorf("Readlink = %q, %v", got, err)
	}
	if runtime.GOOS == "windows" {
		return // "//host/share" is a UNC volume there
	}
	// "//hostparent/x" is cleaned to "/hostparent/x".
	b2, err := basefs.NewFS(e.under, "//hostparent/x")
	if err != nil {
		t.Fatal(err)
	}
	if basefs.Prefix(b2) != "/hostparent/x" {
		t.Errorf("Prefix = %q", basefs.Prefix(b2))
	}
}

func TestReadlink(t *testing.T) {
	for _, e := range envs(t) {
		t.Run(e.name, func(t *testing.T) {
			b := e.symlinkFS(t)
			links := []struct {
				link, target, want string
				escapes            bool
			}{
				{"abs_in", e.join(e.root, "inside.txt"), "/inside.txt", false},
				{"abs_deep", e.join(e.root, "sub", "deep"), "/sub/deep", false},
				{"abs_root", e.root, "/", false},
				{"abs_sibling", e.join(e.parent, "xfoo", "secret"), "", true},
				{"abs_sibling_dir", e.join(e.parent, "xfoo"), "", true},
				{"abs_parent", e.parent, "", true},
				// An absolute target under the root reads back as written...
				{"abs_messy", e.root + "/sub/../inside.txt", winClean(e, "/sub/../inside.txt"), false},
				{"abs_slash", e.root + "/", "/", false},
				// ...unless as written it climbs above the root, even if
				// cleaning would bring it back. (A Windows host stores the
				// target cleaned, so there it is simply under the root.)
				{"abs_climb", e.root + "/../x/inside.txt", "/inside.txt", !winHost(e)},
				{"abs_detour", e.parent + "/xfoo/../x/inside.txt", "/inside.txt", !winHost(e)},
				{"rel_in", "inside.txt", "inside.txt", false},
				// Relative targets are returned unchanged; they hold no host prefix.
				{"rel_out", "../xfoo/secret", "../xfoo/secret", false},
			}
			for _, l := range links {
				if err := e.under.Symlink(l.target, e.join(e.root, l.link)); err != nil {
					t.Fatal(err)
				}
				got, err := b.Readlink(l.link)
				if l.escapes {
					var pe *os.PathError
					if !errors.Is(err, basefs.ErrPathEscapes) || !errors.As(err, &pe) || pe.Path != l.link || pe.Op != "readlink" {
						t.Errorf("Readlink(%q) = %q, %v; want ErrPathEscapes", l.link, got, err)
					}
					if got != "" || (err != nil && strings.Contains(err.Error(), e.parent)) {
						t.Errorf("Readlink(%q) leaks host path: %q, %v", l.link, got, err)
					}
					continue
				}
				if err != nil || got != l.want {
					t.Errorf("Readlink(%q) = %q, %v; want %q", l.link, got, err, l.want)
				}
			}
		})
	}
}

func TestSymlinkTargets(t *testing.T) {
	for _, e := range envs(t) {
		t.Run(e.name, func(t *testing.T) {
			b := e.symlinkFS(t)
			if err := b.Mkdir("/docs", 0o755); err != nil {
				t.Fatal(err)
			}
			ok := []struct {
				target, link, content string
				hostLink              []string // the link's host path, below the root
			}{
				{"/inside.txt", "/l_abs", "inside", []string{"l_abs"}},
				{"/sub/deep/f.txt", "/sub/l_abs_deep", "deep", []string{"sub", "l_abs_deep"}},
				{"inside.txt", "/l_rel", "inside", []string{"l_rel"}},
				// The end-to-end case: a link in /docs to a file in the root.
				{"../inside.txt", "/docs/up2", "inside", []string{"docs", "up2"}},
				{"../../inside.txt", "/sub/deep/l_up2", "inside", []string{"sub", "deep", "l_up2"}},
				{"deep/f.txt", "sub/l_down", "deep", []string{"sub", "l_down"}},
				{"deep/../deep/./f.txt", "/sub/l_messy", "deep", []string{"sub", "l_messy"}},
				{"./deep//f.txt", "/sub/l_messy2", "deep", []string{"sub", "l_messy2"}},
			}
			for _, c := range ok {
				if err := b.Symlink(c.target, c.link); err != nil {
					t.Fatalf("Symlink(%q, %q) = %v", c.target, c.link, err)
				}
				if data, err := b.ReadFile(c.link); err != nil || string(data) != c.content {
					t.Errorf("ReadFile(%q) = %q, %v", c.link, data, err)
				}
				// Readlink returns exactly the text given to Symlink.
				if got, err := b.Readlink(c.link); err != nil || got != c.target {
					t.Errorf("Readlink(%q) = %q, %v; want %q", c.link, got, err, c.target)
				}
				raw, err := e.under.Readlink(e.join(append([]string{e.root}, c.hostLink...)...))
				if err != nil {
					t.Fatal(err)
				}
				if !path.IsAbs(c.target) {
					// On the host, a relative target is the text itself.
					if raw != c.target {
						t.Errorf("Symlink(%q) stored %q, want it verbatim", c.target, raw)
					}
					continue
				}
				// An absolute target is stored as the host root followed by
				// the target, so the host resolves it inside the root.
				rel, inside := hostRel(e, raw)
				if !inside || (runtime.GOOS != "windows" && rel != c.target) {
					t.Errorf("Symlink(%q) stored %q, want it below the root %q", c.target, raw, e.root)
				}
			}

			bad := []struct{ target, link string }{
				{"../../xfoo/secret", "/sub/l_bad"},
				{"../../../xfoo", "/sub/deep/l_bad"},
				{"deep/../../../secret", "sub/l_bad"},
				// An absolute target must be clean, even inside the root.
				{"/sub/../inside.txt", "/l_bad"},
				{"/sub/./deep/f.txt", "/l_bad"},
				{"/sub/deep//f.txt", "/l_bad"},
				{"//inside.txt", "/l_bad"},
				{"/sub/", "/l_bad"},
				{"/..", "/l_bad"},
			}
			if runtime.GOOS == "windows" {
				bad = append(bad, struct{ target, link string }{`..\..\xfoo`, `sub\l_bad`})
			}
			for _, c := range bad {
				err := b.Symlink(c.target, c.link)
				var le *os.LinkError
				if !errors.Is(err, basefs.ErrPathEscapes) || !errors.As(err, &le) || le.Old != c.target || le.New != c.link {
					t.Errorf("Symlink(%q, %q) = %v, want LinkError wrapping ErrPathEscapes", c.target, c.link, err)
				}
				if _, err := b.Lstat(c.link); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("Symlink(%q, %q) created a link: %v", c.target, c.link, err)
				}
			}

			// Relative targets are checked against the cwd-resolved link.
			if err := b.Chdir("/sub/deep"); err != nil {
				t.Fatal(err)
			}
			if err := b.Symlink("../../inside.txt", "l_cwd"); err != nil {
				t.Errorf("Symlink from cwd = %v", err)
			}
			if err := b.Symlink("../../../x/inside.txt", "l_cwd_bad"); !errors.Is(err, basefs.ErrPathEscapes) {
				t.Errorf("Symlink escaping from cwd = %v", err)
			}

			// An empty target is passed through; the host decides. Either
			// way nothing outside the root is named or revealed.
			if err := b.Symlink("", "/l_empty"); err != nil {
				var le *os.LinkError
				if !errors.As(err, &le) || le.Old != "" || le.New != "/l_empty" || strings.Contains(err.Error(), e.parent) {
					t.Errorf("Symlink(\"\", /l_empty) = %#v", err)
				}
			} else if got, err := b.Readlink("/l_empty"); err != nil || got != "" {
				t.Errorf("Readlink(/l_empty) = %q, %v", got, err)
			}

			// An existing link name is reported with the caller's names.
			err := b.Symlink("/inside.txt", "/l_abs")
			var le *os.LinkError
			if err == nil || !errors.As(err, &le) || le.Op != "symlink" || le.Old != "/inside.txt" || le.New != "/l_abs" || strings.Contains(err.Error(), e.parent) {
				t.Errorf("Symlink onto existing = %#v", err)
			}
		})
	}
}

// winHost reports whether e is the host filesystem of a Windows machine,
// which stores absolute link targets cleaned.
func winHost(e *env) bool {
	return e.name == "osfs" && runtime.GOOS == "windows"
}

// winClean returns the absolute virtual path p as Readlink reports it for a
// link placed directly on e: unchanged, except on Windows hosts.
func winClean(e *env, p string) string {
	if winHost(e) {
		return path.Clean(p)
	}
	return p
}

func hostRel(e *env, host string) (string, bool) {
	h := filepath.ToSlash(host)
	r := filepath.ToSlash(e.root)
	if runtime.GOOS == "windows" {
		h = strings.ToLower(osfs.FromNative(osfs.ToNative(h)))
		r = strings.ToLower(osfs.FromNative(r))
	}
	if h == r {
		return "/", true
	}
	if strings.HasPrefix(h, r+"/") {
		return h[len(r):], true
	}
	return "", false
}

func TestChdir(t *testing.T) {
	for _, e := range envs(t) {
		t.Run(e.name, func(t *testing.T) {
			b := e.symlinkFS(t)
			step := func(dir, want string) {
				t.Helper()
				if err := b.Chdir(dir); err != nil {
					t.Fatalf("Chdir(%q) = %v", dir, err)
				}
				if wd, _ := b.Getwd(); wd != want {
					t.Fatalf("Getwd after Chdir(%q) = %q, want %q", dir, wd, want)
				}
			}
			step("sub", "/sub")
			// Relative names resolve against the working directory.
			if data, err := b.ReadFile("deep/f.txt"); err != nil || string(data) != "deep" {
				t.Errorf("ReadFile(deep/f.txt) = %q, %v", data, err)
			}
			if data, err := b.ReadFile("../inside.txt"); err != nil || string(data) != "inside" {
				t.Errorf("ReadFile(../inside.txt) = %q, %v", data, err)
			}
			if _, err := b.ReadFile("../../xfoo/secret"); !errors.Is(err, basefs.ErrPathEscapes) {
				t.Errorf("ReadFile(../../xfoo/secret) = %v", err)
			}
			if info, err := b.Stat(""); err != nil || info.Name() != "sub" {
				t.Errorf("Stat(\"\") = %v, %v", info, err)
			}
			f, err := b.Create("new.txt")
			if err != nil {
				t.Fatal(err)
			}
			f.Close()
			if _, err := e.under.Stat(e.join(e.root, "sub", "new.txt")); err != nil {
				t.Errorf("relative Create did not use the working directory: %v", err)
			}
			step("deep", "/sub/deep")
			step("../..", "/")
			step("/sub/deep/..", "/sub")
			step("/", "/")

			if err := b.Chdir("missing"); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("Chdir(missing) = %v", err)
			}
			var pe *os.PathError
			err = b.Chdir("inside.txt")
			if !errors.As(err, &pe) || pe.Op != "chdir" || pe.Path != "inside.txt" {
				t.Errorf("Chdir(file) = %v", err)
			}
			if wd, _ := b.Getwd(); wd != "/" {
				t.Errorf("Getwd after failed Chdir = %q", wd)
			}
		})
	}
}

// tempFS overrides TempDir on an underlying filesystem.
type tempFS struct {
	absfs.SymlinkFileSystem
	tmp string
}

func (f *tempFS) TempDir() string { return f.tmp }

func TestTempDir(t *testing.T) {
	e := memEnv(t)
	cases := map[string]string{
		"/hostparent/x/var/tmp": "/var/tmp",
		"/hostparent/x":         "/",
		"/hostparent/x/":        "/",
		"/hostparent/xfoo/tmp":  "/tmp", // sibling with the root's name as prefix
		"/hostparent/xfoo":      "/tmp",
		"/hostparent":           "/tmp",
		"/tmp":                  "/tmp",
	}
	for tmp, want := range cases {
		b, err := basefs.NewFS(&tempFS{e.under, tmp}, e.root)
		if err != nil {
			t.Fatal(err)
		}
		if got := b.TempDir(); got != want {
			t.Errorf("TempDir with underlying %q = %q, want %q", tmp, got, want)
		}
		p, err := basefs.NewFileSystem(&tempFS{e.under, tmp}, e.root)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.TempDir(); got != want {
			t.Errorf("FileSystem.TempDir with underlying %q = %q, want %q", tmp, got, want)
		}
	}

	// osfs: a root inside the OS temp directory never exposes the temp path.
	o := osEnv(t)
	if got := o.symlinkFS(t).TempDir(); got != "/tmp" {
		t.Errorf("osfs TempDir = %q, want /tmp", got)
	}
}

func TestSub(t *testing.T) {
	for _, e := range envs(t) {
		t.Run(e.name, func(t *testing.T) {
			b := e.symlinkFS(t)
			sub, err := b.Sub("/sub")
			if err != nil {
				t.Fatal(err)
			}
			if data, err := fs.ReadFile(sub, "deep/f.txt"); err != nil || string(data) != "deep" {
				t.Errorf("ReadFile(sub, deep/f.txt) = %q, %v", data, err)
			}
			if _, err := fs.ReadFile(sub, "../inside.txt"); err == nil {
				t.Error("fs.FS from Sub allowed ..")
			}
			if _, err := fs.ReadFile(sub, "missing"); err == nil || strings.Contains(err.Error(), e.parent) {
				t.Errorf("Sub error leaks host path: %v", err)
			}
			if err := b.Chdir("sub"); err != nil {
				t.Fatal(err)
			}
			deep, err := b.Sub("deep")
			if err != nil {
				t.Fatal(err)
			}
			if data, err := fs.ReadFile(deep, "f.txt"); err != nil || string(data) != "deep" {
				t.Errorf("ReadFile(deep, f.txt) = %q, %v", data, err)
			}
			for _, name := range []string{"/missing", "/inside.txt"} {
				_, err := b.Sub(name)
				var pe *os.PathError
				if !errors.As(err, &pe) || pe.Path != name || strings.Contains(err.Error(), e.parent) {
					t.Errorf("Sub(%q) = %v", name, err)
				}
			}
		})
	}
}

func TestNewFSValidation(t *testing.T) {
	e := memEnv(t)
	ctors := map[string]func(string) error{
		"NewFS": func(dir string) error {
			_, err := basefs.NewFS(e.under, dir)
			return err
		},
		"NewFileSystem": func(dir string) error {
			_, err := basefs.NewFileSystem(e.under, dir)
			return err
		},
	}
	for name, ctor := range ctors {
		if err := ctor(""); !errors.Is(err, os.ErrInvalid) {
			t.Errorf("%s(\"\") = %v", name, err)
		}
		for _, dir := range []string{"hostparent/x", "/hostparent/missing", "/hostparent/secret"} {
			if err := ctor(dir); err == nil {
				t.Errorf("%s(%q) succeeded", name, dir)
			}
		}
	}
}

func TestUnwrapPrefix(t *testing.T) {
	e := memEnv(t)
	s := e.symlinkFS(t)
	p := e.plainFS(t)
	if basefs.Unwrap(s) != e.under || basefs.Unwrap(p) != e.under {
		t.Error("Unwrap did not return the underlying filesystem")
	}
	if basefs.Prefix(s) != "/hostparent/x" || basefs.Prefix(p) != "/hostparent/x" {
		t.Errorf("Prefix = %q, %q", basefs.Prefix(s), basefs.Prefix(p))
	}
	if basefs.Unwrap(e.under) != e.under || basefs.Prefix(e.under) != "" {
		t.Error("Unwrap/Prefix of a non-basefs filesystem")
	}
}

// TestErrorsReportCallerNames checks that failures from the underlying
// filesystem report the caller's name and never the host root.
func TestErrorsReportCallerNames(t *testing.T) {
	for _, e := range envs(t) {
		t.Run(e.name, func(t *testing.T) {
			b := e.symlinkFS(t)
			ignore := func(_ interface{}, err error) error { return err }
			cases := []struct {
				name string
				err  error
			}{
				{"/missing", ignore(b.Stat("/missing"))},
				{"missing", ignore(b.Lstat("missing"))},
				{"/missing", ignore(b.Open("/missing"))},
				{"/missing", ignore(b.OpenFile("/missing", os.O_RDONLY, 0))},
				{"/nodir/f", ignore(b.Create("/nodir/f"))},
				{"/missing", ignore(b.ReadFile("/missing"))},
				{"/missing", ignore(b.ReadDir("/missing"))},
				{"/missing", b.Remove("/missing")},
				{"/sub", b.Mkdir("/sub", 0o755)},
				{"/missing", b.Chmod("/missing", 0o644)},
				{"/missing", b.Chtimes("/missing", time.Now(), time.Now())},
				{"/missing", b.Truncate("/missing", 0)},
				{"/missing", ignore(b.Readlink("/missing"))},
				{"/missing", b.Chdir("/missing")},
			}
			if e.name == "osfs" {
				// memfs.MkdirAll does not fail over an existing file.
				cases = append(cases, struct {
					name string
					err  error
				}{"/inside.txt/x", b.MkdirAll("/inside.txt/x", 0o755)})
			}
			if runtime.GOOS != "windows" {
				cases = append(cases,
					struct {
						name string
						err  error
					}{"/missing", b.Chown("/missing", os.Getuid(), os.Getgid())},
					struct {
						name string
						err  error
					}{"/missing", b.Lchown("/missing", os.Getuid(), os.Getgid())},
				)
			}
			for i, c := range cases {
				var pe *os.PathError
				if c.err == nil {
					t.Errorf("%d: operation on %q unexpectedly succeeded", i, c.name)
					continue
				}
				if !errors.As(c.err, &pe) || pe.Path != c.name {
					t.Errorf("%d: err = %#v, want PathError for %q", i, c.err, c.name)
				}
				if strings.Contains(c.err.Error(), e.parent) {
					t.Errorf("%d: error leaks host path: %v", i, c.err)
				}
			}
			if err := cases[0].err; !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("Stat(/missing) = %v, want ErrNotExist", err)
			}

			err := b.Rename("/missing", "/other")
			var le *os.LinkError
			if !errors.As(err, &le) || le.Op != "rename" || le.Old != "/missing" || le.New != "/other" || strings.Contains(err.Error(), e.parent) {
				t.Errorf("Rename = %#v", err)
			}
			if !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("Rename = %v, want ErrNotExist", err)
			}
			if err := b.Rename("/inside.txt", "/moved.txt"); err != nil {
				t.Errorf("Rename inside root = %v", err)
			}
		})
	}
}

func TestFileMethods(t *testing.T) {
	for _, e := range envs(t) {
		t.Run(e.name, func(t *testing.T) {
			b := e.symlinkFS(t)
			f, err := b.OpenFile("/sub/rw.txt", os.O_RDWR|os.O_CREATE, 0o644)
			if err != nil {
				t.Fatal(err)
			}
			if f.Name() != "/sub/rw.txt" {
				t.Errorf("Name = %q", f.Name())
			}
			if _, err := f.WriteString("hello "); err != nil {
				t.Fatal(err)
			}
			if _, err := f.Write([]byte("world")); err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteAt([]byte("H"), 0); err != nil {
				t.Fatal(err)
			}
			if err := f.Sync(); err != nil {
				t.Fatal(err)
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 11)
			if _, err := io.ReadFull(f, buf); err != nil || string(buf) != "Hello world" {
				t.Errorf("Read = %q, %v", buf, err)
			}
			if _, err := f.Read(buf); err != io.EOF {
				t.Errorf("Read at EOF = %v, want io.EOF", err)
			}
			if n, err := f.ReadAt(buf[:5], 6); err != nil || string(buf[:n]) != "world" {
				t.Errorf("ReadAt = %q, %v", buf[:n], err)
			}
			if err := f.Truncate(5); err != nil {
				t.Fatal(err)
			}
			info, err := f.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if info.Name() != "rw.txt" || info.Size() != 5 || info.IsDir() || !info.Mode().IsRegular() || info.ModTime().IsZero() {
				t.Errorf("Stat = %s %d %v %v %v", info.Name(), info.Size(), info.IsDir(), info.Mode(), info.ModTime())
			}
			_ = info.Sys()
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			// Errors on a closed file report the caller's name.
			for _, err := range []error{f.Close(), func() error { _, err := f.Write([]byte("x")); return err }()} {
				if err == nil {
					continue
				}
				var pe *os.PathError
				if strings.Contains(err.Error(), e.parent) || (errors.As(err, &pe) && pe.Path != "/sub/rw.txt") {
					t.Errorf("closed-file error = %#v", err)
				}
			}
			if _, err := f.Stat(); err != nil && strings.Contains(err.Error(), e.parent) {
				t.Errorf("closed-file Stat error leaks host path: %v", err)
			}

			d, err := b.Open("/sub")
			if err != nil {
				t.Fatal(err)
			}
			infos, err := d.Readdir(-1)
			if err != nil {
				t.Fatal(err)
			}
			names := []string{}
			for _, i := range infos {
				names = append(names, i.Name())
			}
			sort.Strings(names)
			if strings.Join(names, ",") != "deep,rw.txt" {
				t.Errorf("Readdir = %v", names)
			}
			d.Close()
			d, _ = b.Open("sub")
			if n, err := d.Readdirnames(-1); err != nil || len(n) != 2 {
				t.Errorf("Readdirnames = %v, %v", n, err)
			}
			d.Close()
			d, _ = b.Open("sub")
			if n, err := d.ReadDir(-1); err != nil || len(n) != 2 {
				t.Errorf("ReadDir = %v, %v", n, err)
			}
			d.Close()
		})
	}
}

// leakyFS returns errors whose messages embed host paths.
type leakyFS struct {
	absfs.SymlinkFileSystem
}

var errLeak = errors.New("leaky failure")

func (l *leakyFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return nil, fmt.Errorf("listing %s failed: %w", name, errLeak)
}

func (l *leakyFS) Remove(name string) error {
	return &os.PathError{Op: "remove", Path: name, Err: fmt.Errorf("nested %s: %w", name, errLeak)}
}

func (l *leakyFS) Rename(oldpath, newpath string) error {
	return fmt.Errorf("rename %s -> %s: %w", oldpath, newpath, errLeak)
}

func (l *leakyFS) Symlink(oldname, newname string) error {
	return &os.PathError{Op: "symlink", Path: newname, Err: errLeak}
}

func (l *leakyFS) Chmod(name string, mode os.FileMode) error {
	return &os.LinkError{Op: "chmod", Old: name, New: name, Err: errLeak}
}

func TestErrorScrubbing(t *testing.T) {
	e := memEnv(t)
	b, err := basefs.NewFS(&leakyFS{e.under}, e.root)
	if err != nil {
		t.Fatal(err)
	}
	errs := []error{
		func() error { _, err := b.ReadDir("/sub"); return err }(),
		b.Remove("/sub"),
		b.Rename("/a", "/b"),
		b.Symlink("/a", "/b"),
		b.Chmod("/a", 0),
	}
	for i, err := range errs {
		if !errors.Is(err, errLeak) {
			t.Errorf("%d: err = %v, want errLeak in chain", i, err)
		}
		if strings.Contains(err.Error(), e.root) {
			t.Errorf("%d: error leaks root: %v", i, err)
		}
	}
	if got := errs[0].Error(); got != "open /sub: listing /sub failed: leaky failure" {
		t.Errorf("scrubbed message = %q", got)
	}
	var le *os.LinkError
	if !errors.As(errs[3], &le) || le.Old != "/a" || le.New != "/b" {
		t.Errorf("Symlink err = %#v", errs[3])
	}
	var pe *os.PathError
	if !errors.As(errs[4], &pe) || pe.Path != "/a" {
		t.Errorf("Chmod err = %#v", errs[4])
	}
}

// TestSymlinkNamesNeverEscape places links that lead out of the root, both a
// link written on the host and a basefs link moved somewhere shallower, and
// shows that basefs still never names a path outside the root: escaping
// names are rejected whatever links exist, and the methods that act on a
// link itself (Lstat, Readlink, Lchown, Rename, Remove, RemoveAll) change the
// link, never what it points to.
func TestSymlinkNamesNeverEscape(t *testing.T) {
	for _, e := range envs(t) {
		t.Run(e.name, func(t *testing.T) {
			b := e.symlinkFS(t)
			before := e.outsideSnapshot(t)
			if err := e.under.Symlink(e.join(e.parent, "xfoo"), e.join(e.root, "out")); err != nil {
				t.Fatal(err)
			}
			if err := b.Mkdir("/a", 0o755); err != nil {
				t.Fatal(err)
			}
			if err := b.Symlink("../secret", "/a/up"); err != nil {
				t.Fatal(err)
			}
			if err := b.Rename("/a/up", "/up"); err != nil {
				t.Fatal(err)
			}

			for _, name := range []string{"out/../../secret", "/up/../../xfoo/secret", "../xfoo/secret", "/a/../../secret"} {
				if _, err := b.ReadFile(name); !errors.Is(err, basefs.ErrPathEscapes) {
					t.Errorf("ReadFile(%q) = %v, want ErrPathEscapes", name, err)
				}
				if err := b.Symlink("/inside.txt", name); !errors.Is(err, basefs.ErrPathEscapes) {
					t.Errorf("Symlink(_, %q) = %v, want ErrPathEscapes", name, err)
				}
			}
			for _, link := range []string{"/out", "/up"} {
				if info, err := b.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 || info.Name() != link[1:] {
					t.Errorf("Lstat(%q) = %v, %v", link, info, err)
				}
				if got, err := b.Readlink(link); err == nil && strings.Contains(got, e.parent) {
					t.Errorf("Readlink(%q) = %q leaks the host path", link, got)
				}
				if runtime.GOOS != "windows" {
					if err := b.Lchown(link, os.Getuid(), os.Getgid()); err != nil {
						t.Errorf("Lchown(%q) = %v", link, err)
					}
				}
			}
			if got, err := b.Readlink("/up"); err != nil || got != "../secret" {
				t.Errorf("Readlink(/up) = %q, %v", got, err)
			}
			if err := b.Rename("/out", "/a/out"); err != nil {
				t.Fatal(err)
			}
			if err := b.Remove("/up"); err != nil {
				t.Fatal(err)
			}
			if err := b.RemoveAll("/a"); err != nil {
				t.Fatal(err)
			}
			if after := e.outsideSnapshot(t); after != before {
				t.Errorf("outside of root changed:\nbefore\n%s\nafter\n%s", before, after)
			}
		})
	}
}

// TestSymlinkAbsoluteTargetThroughLink shows why an absolute target must be
// clean. The host applies ".." physically: with l a link to the root, the
// host would resolve "/l/../secret", stored as <root>/l/../secret, to
// <parent>/secret. Symlink refuses it, and the clean form reads inside.
func TestSymlinkAbsoluteTargetThroughLink(t *testing.T) {
	for _, e := range envs(t) {
		t.Run(e.name, func(t *testing.T) {
			b := e.symlinkFS(t)
			e.write(t, e.join(e.root, "secret"), "inside")
			if err := b.Symlink("/", "/l"); err != nil {
				t.Fatal(err)
			}
			err := b.Symlink("/l/../secret", "/evil")
			var le *os.LinkError
			if !errors.Is(err, basefs.ErrPathEscapes) || !errors.As(err, &le) || le.Old != "/l/../secret" || le.New != "/evil" {
				t.Errorf("Symlink(/l/../secret, /evil) = %v, want LinkError wrapping ErrPathEscapes", err)
			}
			if _, err := b.Lstat("/evil"); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("Symlink(/l/../secret, /evil) created a link: %v", err)
			}
			if err := b.Symlink("/l/secret", "/good"); err != nil {
				t.Fatal(err)
			}
			// memfs does not follow links in intermediate elements.
			if data, err := b.ReadFile("/good"); e.name == "osfs" && (err != nil || string(data) != "inside") {
				t.Errorf("ReadFile(/good) = %q, %v", data, err)
			}
			if got, err := b.Readlink("/good"); err != nil || got != "/l/secret" {
				t.Errorf("Readlink(/good) = %q, %v", got, err)
			}
		})
	}
}

// unconfined hides osfs's OpenRoot, so a basefs over it constrains names
// only, as it does over osfs before Go 1.25 and on Windows.
type unconfined struct{ absfs.SymlinkFileSystem }

// TestSymlinkHostFollow reproduces the links that lead the host out of the
// root although Symlink accepted each one. The target check in Symlink is
// lexical and made once, at creation; the host follows links physically,
// later. Over osfs with its OpenRoot hidden, the host follows each link out:
// basefs reads "PARENT" from outside the root, and a Chmod (which absnfs
// issues for an NFS SETATTR on the link's handle) changes the file there.
// Over osfs itself, where it can confine to the root (Go 1.25, Unix), every
// operation that would follow the link fails with ErrPathEscapes and nothing
// outside the root changes.
func TestSymlinkHostFollow(t *testing.T) {
	type step struct{ op, a, b string }
	cases := []struct {
		name    string
		steps   []step
		probe   string
		posixly bool // relies on ".." after a symlink resolving physically
	}{
		// A relative link is stored verbatim, so moving it, or a directory
		// holding it, somewhere shallower makes its ".." climb further.
		{"rename link shallower", []step{
			{"mkdir", "/a", ""}, {"symlink", "../secret", "/a/l"}, {"rename", "/a/l", "/l"},
		}, "/l", false},
		{"rename dir holding link", []step{
			{"mkdir", "/a/b", ""}, {"symlink", "../../secret", "/a/b/l"}, {"rename", "/a/b", "/b"},
		}, "/b/l", false},
		// ".." after an element that is itself a symlink, in a relative
		// target. (An absolute one is refused; see
		// TestSymlinkAbsoluteTargetThroughLink.)
		{"target through a link", []step{
			{"mkdir", "/a", ""}, {"symlink", "/", "/a/l"}, {"symlink", "l/../secret", "/a/evil"},
		}, "/a/evil", true},
		// A link created beneath a symlinked directory.
		{"link under a linked directory", []step{
			{"mkdir", "/a", ""}, {"symlink", "/", "/a/l"}, {"symlink", "../secret", "/a/l/evil"},
		}, "/evil", true},
	}
	for _, c := range cases {
		for _, confine := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/confined=%v", c.name, confine), func(t *testing.T) {
				if c.posixly && runtime.GOOS == "windows" {
					t.Skip("Windows resolves .. in a link target before following links")
				}
				e := osEnv(t)
				under := e.under
				if !confine {
					under = unconfined{e.under}
				}
				b, err := basefs.NewFS(under, e.root)
				if err != nil {
					t.Fatal(err)
				}
				if confine && !basefs.Confined(b) {
					t.Skip("osfs cannot confine itself with this Go release on this OS")
				}
				if !confine && basefs.Confined(b) {
					t.Fatal("Confined over osfs without OpenRoot")
				}
				for _, s := range c.steps {
					var err error
					switch s.op {
					case "mkdir":
						err = b.MkdirAll(s.a, 0o755)
					case "symlink":
						err = b.Symlink(s.a, s.b)
					case "rename":
						err = b.Rename(s.a, s.b)
					}
					if err != nil {
						t.Fatalf("%s(%q, %q) = %v", s.op, s.a, s.b, err)
					}
				}
				before := e.outsideSnapshot(t)

				if !confine {
					if data, err := b.ReadFile(c.probe); err != nil || string(data) != "PARENT" {
						t.Errorf("ReadFile(%q) = %q, %v; expected the host to follow the link out of the root", c.probe, data, err)
					}
					if err := b.Chmod(c.probe, 0o600); err != nil {
						t.Fatal(err)
					}
					if e.outsideSnapshot(t) == before {
						t.Errorf("Chmod(%q) changed nothing outside the root; expected the host to follow the link out", c.probe)
					}
					return
				}

				escapes := func(what string, err error) {
					t.Helper()
					if !errors.Is(err, basefs.ErrPathEscapes) {
						t.Errorf("%s(%q) = %v, want ErrPathEscapes", what, c.probe, err)
					}
				}
				_, err = b.ReadFile(c.probe)
				escapes("ReadFile", err)
				_, err = b.Open(c.probe)
				escapes("Open", err)
				_, err = b.Stat(c.probe)
				escapes("Stat", err)
				_, err = b.OpenFile(c.probe, os.O_WRONLY|os.O_APPEND, 0)
				escapes("OpenFile", err)
				_, err = b.Create(c.probe)
				escapes("Create", err)
				escapes("Truncate", b.Truncate(c.probe, 0))
				escapes("Chmod", b.Chmod(c.probe, 0o600))
				escapes("Chtimes", b.Chtimes(c.probe, time.Unix(1, 0), time.Unix(1, 0)))
				escapes("Chown", b.Chown(c.probe, os.Getuid(), os.Getgid()))
				if after := e.outsideSnapshot(t); after != before {
					t.Errorf("outside the root changed:\n%s\nwant:\n%s", after, before)
				}
				// The link itself is still there to read and remove.
				if _, err := b.Readlink(c.probe); err != nil {
					t.Errorf("Readlink(%q) = %v", c.probe, err)
				}
				if err := b.Remove(c.probe); err != nil {
					t.Errorf("Remove(%q) = %v", c.probe, err)
				}
			})
		}
	}
}
