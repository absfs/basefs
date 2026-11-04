# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security
- Over osfs, basefs now keeps the host from following a symlink out of the root. `NewFS` and `NewFileSystem` now use the underlying filesystem's `OpenRoot` method when it has one; osfs built with Go 1.25 or later on a Unix system returns an `os.Root`-backed `osfs.RootFS`. Before, links that `Symlink` accepted could still lead the host outside the root when it followed them later. Those were a relative link moved shallower by `Rename`, a relative target with `..` after a symlinked element, a link made beneath, or moved by `Rename` into, a symlinked directory, and links placed on the host. Read, `Chmod`, `Chtimes`, `Truncate` and write operations then acted on files outside, and an NFS SETATTR on a link's handle reaches those operations. Each now fails with `ErrPathEscapes`. Absolute links that start with the root's given or canonical path, as those basefs stores do, still work. With Go before 1.25 and on Windows, osfs reports `errors.ErrUnsupported`, and basefs constrains names only, as before.
- The README documents that NFS clients resolve absolute symlink targets on their own root. A client on the server's own host therefore needs its own sandbox to be confined to its workspace.
- Root containment is now checked on path-separator boundaries. Previously a name with `..` could reach a sibling directory whose name starts with the root's name: with a root of `/d/x`, `../xfoo/secret` read `/d/xfoo/secret`. Names are now cleaned without clamping at the root, and any name that climbs above `/` is rejected. The joined host path must then equal the root or lie below it on a separator boundary. This includes roots of `/` and Windows volume roots.
- On Windows, backslashes in names are treated as separators. Names carrying a volume (`C:\x`, `c:x`, `\\server\share`, `\\?\...`), names containing a colon (alternate data streams such as `file:stream`) and names with a reserved device name element (`NUL`, `sub\CON`, `CONIN$`, `aux.`) are rejected. Previously `..\` sequences and drive-letter names passed through to the host unchecked.
- `Symlink` rejects targets that resolve outside the root: relative targets are resolved lexically from the link's directory, and Windows volume paths are rejected. Previously a relative or drive-letter target could plant a link that escapes the root.
- `Symlink` rejects an absolute target that is not clean (`/a/../b`, `/a/./b`, `//a`, `/a/`). The host applies `..` physically, so `/l/../secret`, with `l` a link to `/`, would otherwise lead out of the root. Previously absolute targets were stored cleaned, which kept them inside but read back changed.
- `Rename` re-checks a relative symlink it moves, from the link's new directory, as `Symlink` does at creation, and refuses with an `*os.LinkError` wrapping `ErrPathEscapes` a move whose target would then climb above the root: after `Symlink("../../x", "/a/b/e")`, `Rename("/a/b/e", "/d")` now fails and leaves the link in place. Previously the move succeeded, and an unconfined host then followed the link out of the root. Moves that keep the target inside, deeper or sideways, still succeed, and absolute targets, stored under the root, are not checked. The check is made confined or not. It is made on names before the move, so it does not see where a symlinked destination directory leads (a link moved into one is resolved by the host from where it lands) nor a link swapped in between the check and the move. `Rename` does not walk the directories it moves, so a link carried by a directory `Rename` is still not checked. All of these rely on confinement (`Confined` true), and an unconfined host may follow them out.
- The `Symlink` check is lexical and made at creation (and, for a link moved by `Rename` itself, when it moves); basefs cannot stop the host OS from following a link physically. A relative link carried somewhere shallower by the `Rename` of a directory holding it, a relative target whose `..` follows an element that is itself a symlink (`l/../f`), a link created beneath, or moved by `Rename` into, a symlinked directory, and links placed by other processes can all lead the host outside the root, even when made through basefs alone. Where that matters, use `os.Root`, a chroot or a container. The README and package documentation describe this.
- `Readlink` no longer reveals absolute host paths outside the root (it returns `ErrPathEscapes`). It no longer maps a sibling-prefixed target such as `/d/xfoo/secret` to a path inside the root. It judges an absolute target as written, not cleaned, so a detour such as `/d/xfoo/../x/f` is refused, and on Windows it treats a rooted target such as `\Windows` as absolute, as `Symlink` does, rather than returning it as a relative target.
- Errors no longer leak the host root. The old sanitiser called `strings.Replace` with a count of 0, which never replaced anything, and several methods did not sanitise at all. `*os.PathError` and `*os.LinkError` values now carry the caller's names, and other error messages have the root stripped.
- The README now documents what basefs guarantees: it constrains names; it does not control what the host follows through a symlink.

### Added
- `Confined`, which reports whether a basefs keeps the host from following links out of its root.
- `NewFSUnconfined` and `NewFileSystemUnconfined`, which never call `OpenRoot` and constrain names only, as `NewFS` and `NewFileSystem` did before confinement.
- `Close` on `FileSystem` and `SymlinkFileSystem`, which releases the root directory a confined basefs holds open. It does nothing for an unconfined basefs.
- `ErrPathEscapes`, wrapped by the error returned for any name, symlink target or link content outside the root.

### Changed
- **Behaviour change:** over osfs with Go 1.25 or later on Unix, `NewFS` and `NewFileSystem` are confined by `os.Root` (see Security). Links are then judged lexically through `os.Root`: links placed on the host that lead outside the root (`../shared`, `/usr/...`) fail with `ErrPathEscapes`, and so do links that leave the root and come back in (`../x/f` for a root named `x`) or spell the root through another path, such as a symlinked ancestor. To keep following such links, call `NewFSUnconfined` or `NewFileSystemUnconfined` instead.
- `NewFS` and `NewFileSystem` now fail when the underlying filesystem's `OpenRoot` returns an error other than `errors.ErrUnsupported` or a permission error. A permission error (a root the process can search but not read, such as mode `0333`) leaves the basefs unconfined, as `Confined` reports.
- A confined basefs holds its root directory open until `Close` or garbage collection, and is bound to that directory rather than its path: it follows the root when it is renamed on the host and fails once it is removed, even if a directory is created again at the same path.
- Confined, `RemoveAll("/")` removes the root's contents but keeps the root directory, which `os.Root` cannot remove. Unconfined, it removes the root too, as before.
- basefs now imports `github.com/absfs/osfs` in library code, to map osfs's `ErrPathEscapes` to its own, and requires an osfs with `OpenRoot` and `RootFS`. That is currently a pseudo-version of absfs/osfs#6; basefs must require a tagged osfs release before it is released.
- Relative names now resolve against the working directory set by `Chdir`. Previously they were always resolved from `/`.
- `Chdir` now fails for a missing directory or a non-directory, as `os.Chdir` does.
- `Rename` and `Symlink` always return `*os.LinkError`. Errors from the underlying filesystem that are not path errors (for example a bare `syscall.Errno`) are wrapped in an `*os.PathError` for the caller's name.
- `Sub` returns an `fs.FS` that operates through the basefs instead of the underlying filesystem.
- `Lstat` returns a `FileInfo` named like `Stat` does (`/` for the root).
- `NewFS` and `NewFileSystem` clean the root, so `Prefix` returns the cleaned root (for example without a trailing separator). On Windows, Unix-style roots such as `/data` are accepted for virtual underlying filesystems.
- `Unwrap` and `Prefix` now also handle `*FileSystem`, not just `*SymlinkFileSystem`.
- `Symlink` stores a relative target verbatim and an absolute target as the root's host path followed by the target, so `Readlink` returns exactly the text given to `Symlink` (for example `../README.md` rather than `/README.md`). Programs reading an absolute link directly on the host see the host path under the root. On Windows through `osfs`, targets read back with `/` separators.
- `FileSystem` and `SymlinkFileSystem` share a single `baseFS` implementation of every common method.

### Fixed
- `Stat`, `Lstat` and `File.Stat` return the underlying `FileInfo` unchanged when it already carries the virtual base name, which it does for everything below the root, and wrap only the root to name it `/`. Since `Lstat` was made to rename its result, every one of these was a basefs wrapper, so `os.SameFile` was false for all of them, and `Lstat` returned a different type from the entries `ReadDir` and `Readdir` list. Layers that use `os.SameFile` to tell a case-only rename from a rename onto another file (unionfs, and absnfs and absnfs4 above it) then silently did nothing. The root's host name is still not revealed; on Unix its `Sys` still carries the host's device and inode.
- Fixed code formatting to comply with `gofmt` standards
- Corrected LICENSE link in README to point to basefs repository instead of osfs
- Removed commented-out debug code from basefile.go and basefs.go

## [0.0.0-20220705103527] - 2022-07-05

### Security
- Bumped golang.org/x/sys from 0.0.0-20200223170610-d5e6a3e2c0ae to 0.1.0

## Previous Releases

### Added
- Initial implementation of basefs filesystem wrapper
- Support for constraining filesystem access to a specific subdirectory
- Full absfs.FileSystem and absfs.SymlinkFileSystem interface implementation
- Symlink support (Lstat, Readlink, Symlink, Lchown)
- Walk and FastWalk directory traversal
- Utility functions (Unwrap, Prefix) for debugging
- Comprehensive test suite using absfs/fstesting framework
- Path security preventing directory traversal attacks
- Error message sanitization to hide internal paths
