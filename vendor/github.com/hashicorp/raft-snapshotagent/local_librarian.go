package snapshotagent

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"

	"github.com/rboyer/safeio"
)

// LocalLibrarianConfig configures a local librarian.
type LocalLibrarianConfig struct {
	// Path is the local directory to use for snapshots.
	Path string `hcl:"path,optional"`
}

// LocalLibrarian stores snapshots in a local directory.
type LocalLibrarian struct {
	// config is the configuration of the librarian as given at create time.
	config *LocalLibrarianConfig

	filePrefix string
}

// NewLocalLibrarian returns a new local librarian for the given path.
func NewLocalLibrarian(config *LocalLibrarianConfig, filePrefix string) (*LocalLibrarian, error) {
	path := config.Path
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(path, 0700); err != nil {
			return nil, fmt.Errorf("failed to create %q: %v", path, err)
		}
	} else {
		if err != nil {
			return nil, fmt.Errorf("failed to stat path %q: %v", path, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("path %q is not a directory", path)
		}
	}

	return &LocalLibrarian{config, filePrefix}, nil
}

// makeFileName returns a file name for the given snapshot ID and suffix.
func (l *LocalLibrarian) makeFileName(id SnapshotID, suffix string) string {
	if suffix == "" {
		suffix = ".snap"
	}
	return fmt.Sprintf("%s-%d%s", l.filePrefix, id, suffix)
}

// See Librarian.
func (l *LocalLibrarian) Create(id SnapshotID, snap *os.File) error {
	// When using the local librarian we guarantee that the scratch snapshot is
	// written to the same directory as the local librarian outputs.
	//
	// Here we just enforce that contract.
	destDir, err := os.Stat(l.config.Path)
	if err != nil {
		return err
	}
	tmpSnapDir, err := os.Stat(filepath.Dir(snap.Name()))
	if err != nil {
		return err
	}

	if !os.SameFile(destDir, tmpSnapDir) {
		return fmt.Errorf("scratch snapshot required to be written to local librarian output directory")
	}

	snapName := snap.Name()

	// The scratch snaphost doesn't go through the same flow as
	// safeio.WriteToFile ordinarily since for the cloud-based librarians the
	// disk safety isn't required. Given that we need to do the last bits
	// ourselves.
	if err = snap.Chmod(0600); err != nil {
		return err
	}
	if err := snap.Sync(); err != nil {
		return err
	}
	if err := snap.Close(); err != nil {
		return err
	}

	// NOTE: We don't have to verify the snapshot again, since the code handing
	// us a io.Reader already does that, so we can just write it to the final
	// filename directly.

	// Move the file a name with a suffix that shows it's complete, which
	// should be atomic since it's in the same directory.
	final := filepath.Join(l.config.Path, l.makeFileName(id, ""))

	return safeio.Rename(snapName, final)
}

// See Librarian.
func (l *LocalLibrarian) Delete(id SnapshotID) error {
	final := filepath.Join(l.config.Path, l.makeFileName(id, ""))
	if err := os.Remove(final); err != nil {
		return fmt.Errorf("failed to remove snapshot %d: %v", id, err)
	}
	return nil
}

// See Librarian.
func (l *LocalLibrarian) List() (SnapshotIDs, error) {
	contents, err := ioutil.ReadDir(l.config.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read path %q: %v", l.config.Path, err)
	}
	// nameRegExp lets us filter the path's contents for snapshots.
	nameRegExp := regexp.MustCompile(fmt.Sprintf(`^%s-(\d+).snap$`, l.filePrefix))

	var ids SnapshotIDs
	for _, info := range contents {
		m := nameRegExp.FindStringSubmatch(info.Name())
		if m == nil {
			continue
		}

		var id int64
		if _, err := fmt.Sscanf(m[1], "%d", &id); err != nil {
			return nil, fmt.Errorf("bad snapshot file name %q: %v", info.Name(), err)
		}
		ids = append(ids, SnapshotID(id))
	}

	return ids, nil
}

// See Librarian.
func (l *LocalLibrarian) RotationEnabled() bool {
	return true
}

func (l *LocalLibrarian) Info() string {
	return fmt.Sprintf("Local -> Path: %q", l.config.Path)

}
