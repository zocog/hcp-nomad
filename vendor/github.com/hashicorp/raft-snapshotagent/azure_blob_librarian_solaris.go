// +build solaris

package snapshotagent

import (
	"errors"
	"os"

	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/hashicorp/go-hclog"
)

var errAzureOnSolaris = errors.New("azure-blob snapshots: not implemented on Solaris")

// NewAzureBlobLibrarian returns an error on Solaris as the Azure blob library does not build on Solaris
// https://github.com/Azure/azure-storage-blob-go/issues/96
func NewAzureBlobLibrarian(config *AzureBlobLibrarianConfig, env azure.Environment, logger hclog.Logger) (*AzureBlobLibrarian, error) {
	return nil, errAzureOnSolaris
}

// See Librarian.
func (l *AzureBlobLibrarian) Create(id SnapshotID, snap *os.File) error {
	return nil
}

// See Librarian.
func (l *AzureBlobLibrarian) Delete(id SnapshotID) error {
	return nil
}

// See Librarian.
func (l *AzureBlobLibrarian) List() (SnapshotIDs, error) {
	var ids SnapshotIDs

	return ids, nil
}

// See Librarian.
func (l *AzureBlobLibrarian) RotationEnabled() bool {
	return true
}
