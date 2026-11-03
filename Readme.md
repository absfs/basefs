# basefs - Base File System

[![Go Reference](https://pkg.go.dev/badge/github.com/absfs/basefs.svg)](https://pkg.go.dev/github.com/absfs/basefs)
[![Go Report Card](https://goreportcard.com/badge/github.com/absfs/basefs)](https://goreportcard.com/report/github.com/absfs/basefs)
[![CI](https://github.com/absfs/basefs/actions/workflows/ci.yml/badge.svg)](https://github.com/absfs/basefs/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

The `basefs` package implements the `absfs.FileSystem` interface by constraining a `absfs.FileSystem` to a specific subdirectory. A basefs filesystem is re-rooted at that directory: its `/` is the directory, and no name can reach a path outside it. Every path passed to the underlying filesystem is an absolute path that basefs constructs to avoid ambiguity. basefs then calls the underlying filesystem's methods normally. Errors report the name the caller passed in, never the underlying host path.

The `basefs` package also provides `Prefix` and `Unwrap` functions for debugging purposes, and `Confined`, which reports whether the host is kept from following symlinks out of the root (see [Confinement over osfs](#confinement-over-osfs-osroot)). `Close` releases the root directory that a confined basefs holds open.

## Containment

Names are resolved lexically:

1. A relative name is joined to the working directory (see `Chdir`/`Getwd`). An empty name means the working directory.
2. The result is cleaned. A name whose `..` elements would climb above `/` is rejected with an `*os.PathError` (or `*os.LinkError` for `Rename` and `Symlink`) wrapping `basefs.ErrPathEscapes`. `"/.."` and `"../x"` at the root are rejected rather than clamped.
3. The cleaned name is joined to the root, and the result must be the root itself or lie below it on a path-separator boundary. A root of `/d/x` never reaches the sibling `/d/xfoo`.

An absolute host path is just a name inside the root: with a root of `/srv/data`, the name `/etc/passwd` means `/srv/data/etc/passwd`.

On Windows, `\` is a separator as well as `/`. Names carrying a volume (`C:\x`, `c:x`, `\\server\share`, `\\?\C:\x`), names containing a colon anywhere (such as the alternate data stream `file:stream`), and names with an element that is a reserved device name (`NUL`, `CON`, `COM1`, `CONIN$`, ... in any case, with or without trailing dots or spaces) are rejected with `ErrPathEscapes`. Native (`C:\data`) and osfs-style (`/c/data`) roots are both accepted.

### Symbolic links

basefs constrains **names**, not what the underlying filesystem does with them. When the underlying filesystem follows symlinks, as `osfs` does through the host OS, it follows them physically, and basefs cannot stop that. basefs does the following:

- `Readlink` returns exactly the text given to `Symlink`, as POSIX tools (`ln -s`, `readlink`, git, NFS clients) expect: `Symlink("../README.md", "/docs/up")` reads back as `../README.md`. (On Windows through `osfs`, targets read back with `/` separators: `..\x` reads back as `../x`.)
- A relative target is stored verbatim: the link on the host contains the target text. (On Windows, Go's `os.Symlink` stores it with `\` separators, which `osfs` turns back into `/` when reading.) At creation it is resolved lexically against the link's directory, without following any symlink, and refused with `ErrPathEscapes` if it would climb above the root. `../README.md` from `/docs` is accepted; `../../x` from `/docs` is not.
- An absolute target is a virtual path. It is checked the same way and must also be clean, as `path.Clean` leaves it: `/a/../b`, `/a/./b`, `//a` and `/a/` are refused with `ErrPathEscapes`. The host applies `..` physically, so with `l` a link to `/`, the target `/l/../secret` would lead to the root's parent; and Windows stores absolute targets cleaned, so they could not read back as written. An accepted target is stored as the root's host path followed by the target, so the host resolves it inside the root and `Readlink` strips the root off again. A program that reads the link directly on the host, bypassing basefs, sees that host path: with a root of `/srv/data`, `Symlink("/a/f", "/l")` stores `/srv/data/a/f`. Such a link breaks if the root directory itself is moved on the host.
- `Readlink` never reveals an absolute target outside the root: it returns `ErrPathEscapes` instead. It judges the target as written, not cleaned, so a target placed on the host that climbs out of the root and back in (`/srv/xfoo/../data/f` for a root of `/srv/data`) is refused too. On Windows a rooted target without a volume (`\Windows`) is absolute, as it is for `Symlink`.

The target check is made on names, once, when the link is created. A link that was inside the root then can lead out of it when the host follows it later, even if every link in the tree was made through basefs:

- A relative link moved by `Rename`, or carried by a `Rename` of a directory holding it, somewhere shallower: `../secret` made in `/a` and moved to `/` now climbs out of the root.
- A relative target whose `..` follows an element that is itself a symlink (`l/../f`), or a link created beneath a symlinked directory: the host resolves `..` from wherever the symlink leads, not lexically. (Absolute targets cannot contain `..`.)
- A link placed by another process, or replaced between a check and a use.

Any operation that follows a link can then act outside the root: reading, but also `Chmod`, `Chtimes`, `Truncate` and opening for writing. An NFS server such as absnfs issues those on a link's own path for a SETATTR on the link's handle.

### Confinement over osfs (os.Root)

When the underlying filesystem can confine itself to the root, basefs lets it, and the host can then never follow a link out of the root. `NewFS` and `NewFileSystem` look for an `OpenRoot(dir string) (absfs.SymlinkFileSystem, error)` method on the underlying filesystem. If it has one, they operate through what it returns. `osfs` has one: with Go 1.25 or later on a Unix system it returns an `osfs.RootFS`, which resolves every name through an [`os.Root`](https://pkg.go.dev/os#Root).

- Every case above fails with an error wrapping `ErrPathEscapes` instead of leaving the root. So does any link placed on the host that leads outside, and a directory swapped for such a link mid-operation.
- Links are judged on names, each step through `os.Root`, never by where the host would end up. A relative link that never leaves the root works, and so does an absolute link that starts with the root's path as given to `NewFS`, or its canonical path (`filepath.EvalSymlinks`). The absolute links basefs stores are of that kind: the root's host path followed by the target. A link that leaves the root fails even if it comes back in: `../x/f` for a root named `x`, or an absolute link that spells the root another way (`/srv/xfoo/../x/f`, or through a symlinked ancestor such as `/var/...` for a root given as `/private/var/...` on macOS). `Readlink` and `Symlink` are unchanged.
- Host links that are meant to lead outside the root (a shared directory linked as `../shared`, an absolute link into `/usr`) no longer work. Use `NewFSUnconfined` or `NewFileSystemUnconfined` to keep following them, knowingly: they never call `OpenRoot` and constrain names only.
- `Confined(fs)` reports whether a basefs is confined this way. `Unwrap` still returns the filesystem given to `NewFS`, which is not confined.
- Before Go 1.25, and on Windows, `osfs.OpenRoot` reports `errors.ErrUnsupported`, and basefs constrains names only, as described above. So it does when `OpenRoot` reports a permission error: `os.Root` must open the root for reading, so a root the process can search and write but not read (a drop box, mode `0333`) cannot be confined. Any other `OpenRoot` error fails `NewFS`.
- A confined basefs holds the root directory open. `Close` releases it; operations after `Close` fail. A basefs that is not closed releases it when it is garbage collected, so a program that creates many basefs filesystems, or one per request, should close them. An open root also keeps its volume busy and stops it being unmounted.
- A confined basefs is bound to the root directory, not its path. If the root is renamed on the host, the basefs keeps working in it at its new place, and a directory created at the old path is not seen. If the root is removed, every operation fails, even once a directory is created again at that path.
- `RemoveAll("/")` removes everything in the root but keeps the root directory itself, which `os.Root` cannot remove. Unconfined, it removes the root directory too.
- Errors report the same operation names as before (`open`, `stat`, `truncate`), not `os.Root`'s (`openat`, `statat`).

Where `Confined` is false and the host's own resolution of links must stay inside the root, use another OS-level mechanism: a chroot or a container.

### Absolute targets seen by clients

`Readlink` returns an absolute target as written (`/etc/passwd`), and a program that follows the link itself resolves it against its own root, not the basefs root. Over NFS, that means the client follows it. For a remote client, the target names a path on the client machine. For a client mounting from the same host (a loopback mount, or an agent running beside the server), it names the server's own files: an NFS client that creates `ln -s /srv/state/other/file l` in its workspace and then reads, writes, truncates or chmods `l` does so on that host path. The client does this itself, with its own credentials, so it gains no access it did not already have. But a client on the server's host is only confined to its workspace if its own process is confined too, by a sandbox, a chroot, a container or a mount namespace. basefs, and confinement on the server, cannot change where a client resolves a link.

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



