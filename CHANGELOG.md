# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security
- Root containment is now checked on path-separator boundaries. Previously a name with `..` could reach a sibling directory whose name starts with the root's name: with a root of `/d/x`, `../xfoo/secret` read `/d/xfoo/secret`. Names are now cleaned without clamping at the root, and any name that climbs above `/` is rejected. The joined host path must then equal the root or lie below it on a separator boundary. This includes roots of `/` and Windows volume roots.
- On Windows, backslashes in names are treated as separators. Names carrying a volume (`C:\x`, `c:x`, `\\server\share`, `\\?\...`) and reserved device names are rejected. Previously `..\` sequences and drive-letter names passed through to the host unchecked.
- `Symlink` rejects targets that resolve outside the root: relative targets are resolved from the link's directory, and Windows volume paths are rejected. Previously a relative or drive-letter target could plant a link that escapes the root.
- `Readlink` no longer reveals absolute host paths outside the root (it returns `ErrPathEscapes`). It no longer maps a sibling-prefixed target such as `/d/xfoo/secret` to a path inside the root.
- Errors no longer leak the host root. The old sanitiser called `strings.Replace` with a count of 0, which never replaced anything, and several methods did not sanitise at all. `*os.PathError` and `*os.LinkError` values now carry the caller's names, and other error messages have the root stripped.
- The README now documents what basefs guarantees: it constrains names, not what the host follows through symlinks placed by others.

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
- `FileSystem` and `SymlinkFileSystem` share a single implementation of every common method.
- Refactored internal implementation to reduce code duplication between FileSystem and SymlinkFileSystem
  - Introduced shared `baseFS` struct containing common fields and methods
  - Reduced codebase by ~6% while maintaining full backward compatibility
  - All public APIs remain unchanged

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
