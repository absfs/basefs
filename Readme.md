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

basefs constrains **names**, not what the underlying filesystem does with them. When the underlying filesystem follows symlinks, as `osfs` does through the host OS, a link inside the root whose target lies outside it is followed out of the root. basefs does the following:

- `Symlink` resolves the target to a virtual path, joining a relative target to the link's directory and resolving `..` lexically (so `l/../f` is `f` beside `l`, wherever `l` points). A target that would climb above the root is rejected with `ErrPathEscapes`.
- The stored target never contains `..`. A relative target that stays within the link's directory (`f`, `sub/f`) is stored cleaned and relative. An absolute target, or a relative one that climbs (`../f`), is stored as the absolute host path under the root. A stored relative target only descends from wherever the link is, and a stored absolute one names a fixed place under the root, so neither `Rename` of the link or of a directory above it, nor creating it beneath a symlinked directory, can make a link created by basefs point outside the root.
- `Readlink` returns a relative target unchanged and maps an absolute target inside the root back to a virtual path. A climbing target such as `../f` therefore reads back as an absolute virtual path (`/f`), not as the text passed to `Symlink`. `Readlink` never reveals an absolute target outside the root: it returns `ErrPathEscapes` instead.
- Links stored as absolute host paths break if the root directory itself is moved on the host.
- A link placed by another process, or replaced between a check and a use, is still followed by the host. If the tree may contain links that basefs did not create, use an OS-level mechanism (`os.Root`, a chroot, or a container).

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



