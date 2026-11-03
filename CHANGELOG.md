# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security
- Root containment is now checked on path-separator boundaries. Previously a name with `..` could reach a sibling directory whose name starts with the root's name: with a root of `/d/x`, `../xfoo/secret` read `/d/xfoo/secret`. Names are now cleaned without clamping at the root, and any name that climbs above `/` is rejected. The joined host path must then equal the root or lie below it on a separator boundary. This includes roots of `/` and Windows volume roots.
- On Windows, backslashes in names are treated as separators. Names carrying a volume (`C:\x`, `c:x`, `\\server\share`, `\\?\...`), names containing a colon (alternate data streams such as `file:stream`) and names with a reserved device name element (`NUL`, `sub\CON`, `CONIN$`, `aux.`) are rejected. Previously `..\` sequences and drive-letter names passed through to the host unchecked.
- `Symlink` rejects targets that resolve outside the root: relative targets are resolved lexically from the link's directory, and Windows volume paths are rejected. Previously a relative or drive-letter target could plant a link that escapes the root.
- Links created by `Symlink` can no longer be made to escape afterwards. The stored target never contains `..`: a relative target that stays within the link's directory is stored cleaned, and an absolute or climbing (`../f`) target is stored as the absolute host path under the root. Previously, with basefs calls alone, a relative link that was safe where it was made escaped once `Rename` moved it, or a directory holding it, somewhere shallower; a target such as `l/../secret` escaped through an existing link `l` to the root; and a link created beneath a symlinked directory was checked against the wrong directory.
- `Readlink` no longer reveals absolute host paths outside the root (it returns `ErrPathEscapes`). It no longer maps a sibling-prefixed target such as `/d/xfoo/secret` to a path inside the root.
- Errors no longer leak the host root. The old sanitiser called `strings.Replace` with a count of 0, which never replaced anything, and several methods did not sanitise at all. `*os.PathError` and `*os.LinkError` values now carry the caller's names, and other error messages have the root stripped.
- The README now documents what basefs guarantees: it constrains names, and links it creates cannot point outside the root, but the host still follows links placed by other processes wherever they point.

### Added
- `ErrPathEscapes`, wrapped by the error returned for any name, symlink target or link content outside the root.

### Changed
- Relative names now resolve against the working directory set by `Chdir`. Previously they were always resolved from `/`.
- `Chdir` now fails for a missing directory or a non-directory, as `os.Chdir` does.
- `Rename` and `Symlink` always return `*os.LinkError`. Errors from the underlying filesystem that are not path errors (for example a bare `syscall.Errno`) are wrapped in an `*os.PathError` for the caller's name.
- `Sub` returns an `fs.FS` that operates through the basefs instead of the underlying filesystem.
- `Lstat` returns a `FileInfo` named like `Stat` does (`/` for the root).
- `NewFS` and `NewFileSystem` clean the root, so `Prefix` returns the cleaned root (for example without a trailing separator). On Windows, Unix-style roots such as `/data` are accepted for virtual underlying filesystems.
- `Unwrap` and `Prefix` now also handle `*FileSystem`, not just `*SymlinkFileSystem`.
- `Symlink` stores a climbing relative target (`../f`) as an absolute host path, so `Readlink` returns it as an absolute virtual path (`/f`) rather than the text passed in. Relative targets are stored cleaned (`./a/../f` becomes `f`). Links stored as absolute host paths break if the root is moved on the host.
- `FileSystem` and `SymlinkFileSystem` share a single `baseFS` implementation of every common method.

### Fixed
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
