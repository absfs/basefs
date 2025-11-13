# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
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
