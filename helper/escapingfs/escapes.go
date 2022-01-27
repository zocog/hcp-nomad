package escapingfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// PathEscapesAllocViaRelative returns if the given path escapes the allocation
// directory using relative paths.
//
// Does NOT handle the use of symlinks.
//
// The prefix is joined to the path (e.g. "task/local"), and this function
// checks if path escapes the alloc dir, NOT the prefix directory within the alloc dir.
// With prefix="task/local", it will return false for "../secret", but
// true for "../../../../../../root" path; only the latter escapes the alloc dir.
func PathEscapesAllocViaRelative(prefix, path string) (bool, error) {
	// Verify the destination doesn't escape the tasks directory
	alloc, err := filepath.Abs(filepath.Join("/", "alloc-dir/", "alloc-id/"))
	if err != nil {
		return false, err
	}
	abs, err := filepath.Abs(filepath.Join(alloc, prefix, path))
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(alloc, abs)
	if err != nil {
		return false, err
	}

	return strings.HasPrefix(rel, ".."), nil
}

// pathEscapesBaseViaSymlink returns if path escapes dir, taking into account evaluation
// of symlinks.
//
// The base directory must be an absolute path.
func pathEscapesBaseViaSymlink(base, full string) (bool, error) {
	resolveSym, err := filepath.EvalSymlinks(full)
	if err != nil {
		return false, err
	}

	// filepath.HasPrefix is deprecated for not supporting case-insensitive filesystems,
	// with no straightforward alternative. From a security perspective it should
	// be fine, as the behavior here would to erroneously block a cross-case path
	// comparison, which is annoying to macOS users but not a security hole.
	if !filepath.HasPrefix(resolveSym, base) {
		return true, nil
	}

	return false, nil
}

// PathEscapesAllocDir returns true if base/prefix/path escapes the given base directory.
//
// Escaping a directory can be done with relative paths (e.g. ../../ etc.) or by
// using symlinks. This checks both methods.
//
// The base directory must be an absolute path.
func PathEscapesAllocDir(base, prefix, path string) (bool, error) {
	full := filepath.Join(base, prefix, path)

	// If base is not an absolute path, the caller passed in the wrong thing.
	if !filepath.IsAbs(base) {
		return false, errors.New("alloc dir must be absolute")
	}

	// Check path does not escape the alloc dir using relative paths.
	if escapes, err := PathEscapesAllocViaRelative(prefix, path); err != nil {
		return false, err
	} else if escapes {
		return true, nil
	}

	// Check path does not escape the alloc dir using symlinks.
	if escapes, err := pathEscapesBaseViaSymlink(base, full); err != nil {
		if os.IsNotExist(err) {
			// Treat non-existent files as non-errors; perhaps not ideal but we
			// have existing features (log-follow) that depend on this. Still safe,
			// because we do the symlink check on every ReadAt call also.
			return false, nil
		}
		return false, err
	} else if escapes {
		return true, nil
	}

	return false, nil
}
