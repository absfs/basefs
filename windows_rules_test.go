package basefs

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/absfs/absfs"
)

// withWindowsPaths runs fn with Windows path rules enabled, on any platform.
func withWindowsPaths(t *testing.T, on bool) {
	t.Helper()
	old := windowsPaths
	windowsPaths = on
	t.Cleanup(func() { windowsPaths = old })
}

func TestHasVolume(t *testing.T) {
	cases := map[string]bool{
		"":                 false,
		"C":                false,
		"C:":               true,
		`C:\x`:             true,
		"c:x":              true,
		`\\server\share`:   true,
		"//server/share":   true,
		`\/server`:         true,
		`\\?\C:\x`:         true,
		`\\.\pipe\x`:       true,
		"/c/x":             false,
		`\x`:               false,
		"1:x":              false,
		"relative/path":    false,
		"/single/absolute": false,
	}
	for p, want := range cases {
		if got := hasVolume(p); got != want {
			t.Errorf("hasVolume(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestVirtualWindowsRules(t *testing.T) {
	withWindowsPaths(t, true)
	b := &baseFS{cwd: "/sub", prefix: `C:\root`}
	ok := map[string]string{
		"":                `/sub`,
		`file`:            `/sub/file`,
		`..\file`:         `/file`,
		`\file`:           `/file`,
		`deep\..\file`:    `/sub/file`,
		`\sub\deep\`:      `/sub/deep`,
		`/mixed\sep/file`: `/mixed/sep/file`,
		`C`:               `/sub/C`,
		`CONSOLE`:         `/sub/CONSOLE`,
		`COM0`:            `/sub/COM0`,
		`LPTX`:            `/sub/LPTX`,
	}
	for name, want := range ok {
		got, valid := b.virtual(name)
		if !valid || got != want {
			t.Errorf("virtual(%q) = %q, %v; want %q", name, got, valid, want)
		}
	}
	escapes := []string{
		`..\..\x`, `\..\x`, `deep\..\..\..\x`,
		`C:\x`, `c:x`, `C:`, `D:/x`,
		`\\server\share\x`, `//server/share/x`, `\\?\C:\root\x`, `\\.\C:\root`,
		// Reserved device names, in any element, any case, with trailing
		// dots or spaces, and any name holding a colon (alternate data
		// streams).
		`NUL`, `nul`, `deep\CON`, `aux\f`, `PRN.`, `com1 `, `LPT9`,
		`CONIN$`, `conout$`, `file:stream`, `deep\f::$DATA`,
	}
	for _, name := range escapes {
		if got, valid := b.virtual(name); valid {
			t.Errorf("virtual(%q) = %q, want escape", name, got)
		}
		if _, _, err := b.path("open", name); !errors.Is(err, ErrPathEscapes) {
			t.Errorf("path(%q) err = %v", name, err)
		}
	}
}

func TestVirtualUnixRules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the OS itself treats backslash as a separator")
	}
	withWindowsPaths(t, false)
	b := &baseFS{cwd: "/", prefix: "/d/x"}
	// A backslash is an ordinary name byte; the result stays inside the root.
	for _, name := range []string{`..\xfoo`, `\..\xfoo`, `C:\x`, `\\server\share`} {
		v, valid := b.virtual(name)
		if !valid {
			t.Errorf("virtual(%q) rejected; backslash is not a separator here", name)
			continue
		}
		if _, inside := relTo(b.prefix, b.host(v)); !inside {
			t.Errorf("host(%q) = %q escapes", v, b.host(v))
		}
	}
}

func TestHostJoin(t *testing.T) {
	cases := []struct {
		windows bool
		prefix  string
		vpath   string
		want    string
	}{
		{true, `C:\root`, "/a/b", `C:\root\a\b`},
		{true, `C:\`, "/a", `C:\a`},
		{true, `C:\root`, "/", `C:\root`},
		{true, "/c/root", "/a/b", "/c/root/a/b"},
		{true, `\\srv\share\root`, "/a", `\\srv\share\root\a`},
		{false, "/", "/a", "/a"},
		{false, "/d/x", "/a/b", "/d/x/a/b"},
	}
	for _, c := range cases {
		withWindowsPaths(t, c.windows)
		b := &baseFS{prefix: c.prefix}
		if got := b.host(c.vpath); got != c.want {
			t.Errorf("windows=%v host(%q, %q) = %q, want %q", c.windows, c.prefix, c.vpath, got, c.want)
		}
	}
}

func TestRelTo(t *testing.T) {
	cases := []struct {
		windows    bool
		root, host string
		want       string
		inside     bool
	}{
		{false, "/d/x", "/d/x", "/", true},
		{false, "/d/x", "/d/x/", "/", true},
		{false, "/d/x", "/d/x/a/b", "/a/b", true},
		{false, "/d/x", "/d/xfoo", "", false},
		{false, "/d/x", "/d/xfoo/secret", "", false},
		{false, "/d/x", "/d/x/../xfoo", "", false},
		{false, "/d/x", "/d", "", false},
		{false, "/d/x", "/D/X/a", "", false},
		{false, "/d/x", `/d/x\..\xfoo`, "", false},
		{false, "/", "/", "/", true},
		{false, "/", "/a", "/a", true},
		{true, `C:\root`, `C:\root\a`, "/a", true},
		{true, `C:\root`, `c:\ROOT\a`, "/a", true},
		{true, `C:\root`, "/c/root/a", "/a", true},
		{true, "/c/root", `C:\root\a\b`, "/a/b", true},
		{true, `C:\root`, `C:\rootfoo\a`, "", false},
		{true, `C:\root`, `C:\root\..\rootfoo`, "", false},
		{true, `C:\root`, `D:\root\a`, "", false},
		{true, `C:\root`, `C:\`, "", false},
		{true, `C:\`, `C:\a`, "/a", true},
		{true, `C:\`, `C:\`, "/", true},
		{true, `\\srv\share\root`, "//srv/share/root/a", "/a", true},
		{true, `\\srv\share\root`, `\\srv\share\rootfoo`, "", false},
		{true, `\\srv\share\root`, "/srv/share/root/a", "", false},
	}
	for _, c := range cases {
		withWindowsPaths(t, c.windows)
		got, inside := relTo(c.root, c.host)
		if got != c.want || inside != c.inside {
			t.Errorf("windows=%v relTo(%q, %q) = %q, %v; want %q, %v", c.windows, c.root, c.host, got, inside, c.want, c.inside)
		}
	}
}

func TestRootFormsAndScrub(t *testing.T) {
	withWindowsPaths(t, true)
	for prefix, msgs := range map[string][]string{
		`C:\Users\x`: {`open C:\Users\x\a: denied`, "open C:/Users/x/a: denied", "open /c/Users/x/a: denied"},
		"/c/Users/x": {`open C:\Users\x\a: denied`, "open /c/Users/x/a: denied"},
	} {
		b := &baseFS{prefix: prefix}
		for _, msg := range msgs {
			err := b.scrub(errors.New(msg))
			if strings.Contains(strings.ToLower(err.Error()), "users") {
				t.Errorf("prefix %q: scrub(%q) = %q", prefix, msg, err)
			}
		}
	}

	withWindowsPaths(t, false)
	b := &baseFS{prefix: "/"}
	if forms := b.rootForms(); len(forms) != 0 {
		t.Errorf("rootForms for / = %q, want none", forms)
	}
	plain := errors.New("open /a: denied")
	if b.scrub(plain) != plain {
		t.Error("scrub changed an error with no root in it")
	}
}

// linkFS is a minimal SymlinkFileSystem that records Symlink calls and
// returns a fixed Readlink target.
type linkFS struct {
	absfs.SymlinkFileSystem
	target         string
	gotOld, gotNew string
}

func (l *linkFS) Readlink(string) (string, error) { return l.target, nil }

func (l *linkFS) Symlink(oldname, newname string) error {
	l.gotOld, l.gotNew = oldname, newname
	return nil
}

func TestSymlinkWindowsRules(t *testing.T) {
	withWindowsPaths(t, true)
	lfs := &linkFS{}
	f := &SymlinkFileSystem{baseFS: baseFS{cwd: "/", prefix: `C:\root`, fs: lfs}, sfs: lfs}

	readlink := map[string]string{
		`C:\root\a\b`:    "/a/b",
		"/c/root/a":      "/a",
		`c:\ROOT`:        "/",
		`C:\root\`:       "/",
		`C:\root\a\..\b`: "/a/../b",
		`c:root\a`:       "/a",
		`..\sibling`:     `..\sibling`,
		`relative\file`:  `relative\file`,
	}
	for target, want := range readlink {
		lfs.target = target
		if got, err := f.Readlink("/link"); err != nil || got != want {
			t.Errorf("Readlink -> %q = %q, %v; want %q", target, got, err, want)
		}
	}
	escapes := []string{
		`C:\rootfoo\x`, `D:\root\x`, `c:relative`, `\\srv\share\x`, "/c/other",
		// As written they climb above the root, though cleaning brings them back.
		`C:\root\..\root\a`, `C:\rootfoo\..\root\a`,
		// Rooted without a volume: absolute, as in Symlink, and not under the root.
		`\Windows\System32`, `\root\a`,
	}
	for _, target := range escapes {
		lfs.target = target
		if got, err := f.Readlink("/link"); !errors.Is(err, ErrPathEscapes) || got != "" {
			t.Errorf("Readlink -> %q = %q, %v; want ErrPathEscapes", target, got, err)
		}
	}

	if err := f.Symlink("/a", "/l"); err != nil || lfs.gotOld != `C:\root\a` || lfs.gotNew != `C:\root\l` {
		t.Errorf("Symlink(/a, /l) stored (%q, %q), %v", lfs.gotOld, lfs.gotNew, err)
	}
	// Relative targets are stored verbatim, backslashes and all.
	if err := f.Symlink(`..\x`, `sub\l`); err != nil || lfs.gotOld != `..\x` || lfs.gotNew != `C:\root\sub\l` {
		t.Errorf(`Symlink(..\x, sub\l) stored (%q, %q), %v`, lfs.gotOld, lfs.gotNew, err)
	}
	if err := f.Symlink(`d\..\x\`, `sub\l`); err != nil || lfs.gotOld != `d\..\x\` {
		t.Errorf(`Symlink(d\..\x\, sub\l) stored %q, %v`, lfs.gotOld, err)
	}
	// An absolute target is stored under the root as written and reads back
	// unchanged, with forward slashes.
	for _, target := range []string{"/a/b", "/", `\a\b`} {
		if err := f.Symlink(target, "/l"); err != nil || !strings.HasPrefix(lfs.gotOld, `C:\root`) {
			t.Errorf("Symlink(%q, /l) stored %q, %v", target, lfs.gotOld, err)
		}
		lfs.target = lfs.gotOld
		if got, err := f.Readlink("/l"); err != nil || got != strings.ReplaceAll(target, `\`, "/") {
			t.Errorf("Readlink after Symlink(%q) = %q, %v (stored %q)", target, got, err, lfs.gotOld)
		}
	}
	bad := [][2]string{
		{`C:\x`, "/l"}, {`\\srv\share`, "/l"}, {`..\..\x`, `sub\l`}, {"/a", `C:\l`},
		// An absolute target must be clean.
		{"/a/./b/", "/l"}, {"/a/../b", "/l"}, {`\a\..\b`, "/l"}, {`\a\`, "/l"},
	}
	for _, c := range bad {
		err := f.Symlink(c[0], c[1])
		var le *os.LinkError
		if !errors.Is(err, ErrPathEscapes) || !errors.As(err, &le) || le.Old != c[0] || le.New != c[1] {
			t.Errorf("Symlink(%q, %q) = %v, want LinkError wrapping ErrPathEscapes", c[0], c[1], err)
		}
	}
}

// renameFS is a linkFS on which every name is a symlink to target, and
// which records Rename calls.
type renameFS struct {
	linkFS
	renamed bool
}

// linkInfo describes a symlink; only Mode is called.
type linkInfo struct{ os.FileInfo }

func (linkInfo) Mode() os.FileMode { return os.ModeSymlink }

func (r *renameFS) Lstat(string) (os.FileInfo, error) { return linkInfo{}, nil }

func (r *renameFS) Rename(string, string) error {
	r.renamed = true
	return nil
}

func TestRenameLinkWindowsRules(t *testing.T) {
	withWindowsPaths(t, true)
	rfs := &renameFS{}
	b := &baseFS{cwd: "/", prefix: `C:\root`, fs: rfs}
	cases := []struct {
		target, newname string
		escapes         bool
	}{
		{`..\..\x`, `\a\b\l`, false},
		{`..\..\x`, `a\l`, true},
		{`..\..\x`, `\l`, true},
		// Absolute targets, with or without a volume, are stored under the
		// root and are not checked.
		{`C:\root\x`, `\l`, false},
		{`c:x`, `\l`, false},
		{`\\srv\share`, `\l`, false},
		{`\x`, `\l`, false},
		// As in Symlink, a relative target must also name an ordinary file.
		{`d\NUL`, `\l`, true},
	}
	for _, c := range cases {
		rfs.target, rfs.renamed = c.target, false
		err := b.Rename(`\a\b\l`, c.newname)
		if c.escapes {
			var le *os.LinkError
			if !errors.Is(err, ErrPathEscapes) || !errors.As(err, &le) || le.Op != "rename" || rfs.renamed {
				t.Errorf("Rename of a link to %q to %q = %v (renamed %v), want ErrPathEscapes", c.target, c.newname, err, rfs.renamed)
			}
			continue
		}
		if err != nil || !rfs.renamed {
			t.Errorf("Rename of a link to %q to %q = %v (renamed %v)", c.target, c.newname, err, rfs.renamed)
		}
	}
}
