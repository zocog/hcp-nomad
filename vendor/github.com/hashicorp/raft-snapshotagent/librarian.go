package snapshotagent

import (
	"fmt"
	"os"
	"regexp"
	"sort"
)

// keyRegExp lets us filter contents for snapshots.
func keyRegExp(prefix string) *regexp.Regexp {
	return regexp.MustCompile(`^` + prefix + `-(\d+).snap$`)
}

// SnapshotID is used to provide a very likely unique and monotonically
// increasing ID for snapshots, so they are easy to sort by when they were
// taken.
type SnapshotID int64

// See fmt.Stringer.
func (s SnapshotID) String() string {
	return fmt.Sprintf("%d", s)
}

// SnapshotIDs is a slice of snapshots.
type SnapshotIDs []SnapshotID

// See sort.Interface.
func (s SnapshotIDs) Len() int           { return len(s) }
func (s SnapshotIDs) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s SnapshotIDs) Less(i, j int) bool { return s[i] < s[j] }

// Librarian is an interface for a snapshot storage facility.
type Librarian interface {
	// Create takes the contents of the file and saves a new snapshot with the
	// given ID.
	Create(id SnapshotID, snap *os.File) error

	// Delete removes the given snapshot by ID.
	Delete(id SnapshotID) error

	// List returns a list of available snapshot IDs.
	List() (SnapshotIDs, error)

	// Enable snapshot rotation
	RotationEnabled() bool

	// Info returns info about the library to display to user
	Info() string
}

// Rotate is a generic algorithm for rotating snapshots. It'll sort available
// snapshots by ID and then retain up to the given number, and delete up to the
// limit if necessary. It returns the number of snapshots that were deleted.
func Rotate(l Librarian, retain int, limit int) (int, error) {
	ids, err := l.List()
	if err != nil {
		return 0, err
	}

	var deleted int
	sort.Sort(ids)
	for i := 0; (i < (len(ids) - retain)) && (i < limit); i++ {
		if err := l.Delete(ids[i]); err != nil {
			return deleted, err
		}
		deleted++
	}

	return deleted, nil
}
