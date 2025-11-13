# Contributing to basefs

Thank you for your interest in contributing to basefs! This document provides guidelines and instructions for contributing.

## Code of Conduct

This project follows the standard open source code of conduct. Be respectful and professional in all interactions.

## How to Contribute

### Reporting Issues

If you find a bug or have a feature request:

1. Check the [issue tracker](https://github.com/absfs/basefs/issues) to see if it's already reported
2. If not, create a new issue with:
   - Clear title and description
   - Steps to reproduce (for bugs)
   - Expected vs actual behavior
   - Go version and OS information
   - Code samples if applicable

### Submitting Changes

1. **Fork the repository** and create a new branch for your changes:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes** following the coding standards below

3. **Test your changes:**
   ```bash
   go test -v ./...
   ```

4. **Commit your changes** with clear, descriptive commit messages:
   ```bash
   git commit -m "Add feature: brief description"
   ```

5. **Push to your fork** and submit a pull request

## Coding Standards

### Code Quality

- **Formatting:** All code must pass `gofmt`. Run `gofmt -w .` before committing
- **Vetting:** Code must pass `go vet ./...` without warnings
- **Testing:** New features must include tests

### Testing Requirements

- All new code must have test coverage
- Tests should use the `absfs/fstesting` framework where applicable
- Run tests with race detection: `go test -race ./...`
- Ensure tests pass on Linux, macOS, and Windows

### Code Style

- Follow standard Go conventions and idioms
- Keep functions focused and reasonably sized
- Use meaningful variable and function names
- Add comments for exported functions and types
- Include package-level documentation

### Documentation

- Update README.md if adding new features
- Add godoc examples for new public APIs
- Update CHANGELOG.md following [Keep a Changelog](https://keepachangelog.com/) format
- Document any breaking changes clearly

## Development Workflow

### Setting Up Development Environment

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/basefs.git
cd basefs

# Add upstream remote
git remote add upstream https://github.com/absfs/basefs.git

# Install dependencies
go mod download

# Run tests
go test ./...
```

### Before Submitting PR

Run this checklist:

- [ ] Code is formatted with `gofmt -w .`
- [ ] All tests pass: `go test -v ./...`
- [ ] No `go vet` warnings: `go vet ./...`
- [ ] Added tests for new functionality
- [ ] Updated documentation (README, CHANGELOG, godoc)
- [ ] Commit messages are clear and descriptive
- [ ] Branch is up to date with master

### Pull Request Guidelines

- Keep PRs focused on a single feature or fix
- Write clear PR descriptions explaining what and why
- Reference related issues in PR description
- Respond to review feedback promptly
- Ensure CI checks pass

## Architecture Guidelines

### absfs Interface Compliance

basefs implements the `absfs.FileSystem` and `absfs.SymlinkFileSystem` interfaces. Any changes must maintain full compliance with these interfaces.

### Path Security

The core purpose of basefs is to constrain filesystem access to a subdirectory. All changes must maintain these security guarantees:

- Paths must not escape the base directory
- Path validation must prevent directory traversal attacks
- Error messages must not leak information about the real filesystem structure

### Code Organization

- `basefs.go` - Core filesystem implementations
- `basefile.go` - File wrapper implementation
- `utils.go` - Utility functions (Unwrap, Prefix)
- `basefs_test.go` - Test suite
- `example_test.go` - Godoc examples

## Questions?

If you have questions about contributing, feel free to:

- Open an issue for discussion
- Check the [absfs documentation](https://github.com/absfs/absfs)
- Review existing code and tests for patterns

Thank you for contributing to basefs!
