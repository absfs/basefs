# basefs - Base File System

[![Go Reference](https://pkg.go.dev/badge/github.com/absfs/basefs.svg)](https://pkg.go.dev/github.com/absfs/basefs)
[![Go Report Card](https://goreportcard.com/badge/github.com/absfs/basefs)](https://goreportcard.com/report/github.com/absfs/basefs)
[![CI](https://github.com/absfs/basefs/actions/workflows/ci.yml/badge.svg)](https://github.com/absfs/basefs/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

The `basefs` package implements the `absfs.FileSystem` interface by constraining a `absfs.FileSystem` to a specific subdirectory. A basefs filesystem is re-rooted at that directory: its `/` is the directory, and no name can reach a path outside it. Every path passed to the underlying filesystem is an absolute path that basefs constructs to avoid ambiguity. basefs then calls the underlying filesystem's methods normally. Errors report the name the caller passed in, never the underlying host path.

The `basefs` package also provides `Prefix` and `Unwrap` functions for debugging purposes.

## Containment

Names are resolved lexically:

1. A relative name is joined to the working directory (see `Chdir`/`Getwd`). An empty name means the working directory.
2. The result is cleaned. A name whose `..` elements would climb above `/` is rejected with an `*os.PathError` (or `*os.LinkError` for `Rename` and `Symlink`) wrapping `basefs.ErrPathEscapes`. `"/.."` and `"../x"` at the root are rejected rather than clamped.
3. The cleaned name is joined to the root, and the result must be the root itself or lie below it on a path-separator boundary. A root of `/d/x` never reaches the sibling `/d/xfoo`.

An absolute host path is just a name inside the root: with a root of `/srv/data`, the name `/etc/passwd` means `/srv/data/etc/passwd`.

On Windows, `\` is a separator as well as `/`. Names carrying a volume (`C:\x`, `c:x`, `\\server\share`, `\\?\C:\x`), names containing a colon anywhere (such as the alternate data stream `file:stream`), and names with an element that is a reserved device name (`NUL`, `CON`, `COM1`, `CONIN$`, ... in any case, with or without trailing dots or spaces) are rejected with `ErrPathEscapes`. Native (`C:\data`) and osfs-style (`/c/data`) roots are both accepted.

### Symbolic links

basefs constrains **names**, not what the underlying filesystem does with them. When the underlying filesystem follows symlinks, as `osfs` does through the host OS, it follows them physically, and basefs cannot stop that. basefs does the following:

- `Readlink` returns exactly the text given to `Symlink`, as POSIX tools (`ln -s`, `readlink`, git, NFS clients) expect: `Symlink("../README.md", "/docs/up")` reads back as `../README.md`.
- A relative target is stored verbatim: the link on the host contains the target text, byte for byte. At creation it is resolved lexically against the link's directory, without following any symlink, and refused with `ErrPathEscapes` if it would climb above the root. `../README.md` from `/docs` is accepted; `../../x` from `/docs` is not.
- An absolute target is a virtual path. It is checked the same way and stored as the root's host path followed by the target, so the host resolves it inside the root and `Readlink` strips the root off again. A program that reads the link directly on the host, bypassing basefs, sees that host path: with a root of `/srv/data`, `Symlink("/a/f", "/l")` stores `/srv/data/a/f`. Such a link breaks if the root directory itself is moved on the host. (On Windows it reads back with `/` separators, and cleaned, since Windows itself stores absolute link targets cleaned: `/a/../b` reads back as `/b`.)
- `Readlink` never reveals an absolute target outside the root: it returns `ErrPathEscapes` instead.

The target check is made on names, once, when the link is created. A link that was inside the root then can lead out of it when the host follows it later, even if every link in the tree was made through basefs:

- A relative link moved by `Rename`, or carried by a `Rename` of a directory holding it, somewhere shallower: `../secret` made in `/a` and moved to `/` now climbs out of the root.
- A target whose `..` follows an element that is itself a symlink (`l/../f`), or a link created beneath a symlinked directory: the host resolves `..` from wherever the symlink leads, not lexically.
- A link placed by another process, or replaced between a check and a use.

If the host's own resolution of links must stay inside the root, use an OS-level mechanism: `os.Root`, a chroot, or a container.

### Other methods

- `Chdir` requires an existing directory inside the root. `Getwd` returns a virtual path.
- `TempDir` returns the underlying temporary directory as a virtual path if it lies inside the root, and `/tmp` otherwise.
- `Sub` returns a read-only `fs.FS` that operates through the basefs, so it is confined as well.


## Install 

```bash
$ go get github.com/absfs/basefs
```

## Example Usage

```go
package main

import (
    "fmt"

    "github.com/absfs/basefs"
    "github.com/absfs/osfs"
)

func main() {
    ofs, _ := osfs.NewFS() // remember kids don't ignore errors
    bfs, _ := basefs.NewFS(ofs, "/tmp")
    f, _ := bfs.Create("/test.txt")
    defer bfs.Remove("/test.txt")
    bfsdata := []byte("base fs bound to `/tmp`\n")
    f.Write(bfsdata)
    f.Close()

    f, _ = ofs.Open("/tmp/test.txt")
    defer f.Close()
    ofsdata := make([]byte, 512)
    n, _ := f.Read(ofsdata)
    ofsdata = ofsdata[:n]

    if string(bfsdata) == string(ofsdata) {
        fmt.Println("it's the same file.")
    }
}
```

## absfs
Check out the [`absfs`](https://github.com/absfs/absfs) repo for more information about the abstract filesystem interface and features like filesystem composition 

## LICENSE

This project is governed by the MIT License. See [LICENSE](https://github.com/absfs/basefs/blob/master/LICENSE)



