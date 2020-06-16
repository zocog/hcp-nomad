// +build !solaris

package snapshotagent

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft-snapshotagent/internal/logging"
)

func makeConsulKey(l *AzureBlobLibrarian, id SnapshotID) *string {
	snapshotString := fmt.Sprintf("%v-%d.snap", l.outputNamePrefix, id)
	return &snapshotString
}

// NewAzureBlobLibrarian returns a new local librarian for the given path.
func NewAzureBlobLibrarian(config *AzureBlobLibrarianConfig, env azure.Environment, outputNamePrefix string, logger hclog.Logger) (*AzureBlobLibrarian, error) {
	return &AzureBlobLibrarian{
		config:           config,
		environment:      env,
		logger:           logger.Named(logging.Azure),
		outputNamePrefix: outputNamePrefix,
	}, nil
}

func getBlockBlobURL(storageEndpointSuffix, accountName, accountKey, containerName, fileName string) (*azblob.BlockBlobURL, error) {
	u, err := url.Parse(fmt.Sprintf("https://%s.%s/%s/%s", accountName, storageEndpointSuffix, containerName, fileName))
	if err != nil {
		return nil, err
	}
	credential, err := azblob.NewSharedKeyCredential(accountName, accountKey)
	if err != nil {
		return nil, err
	}
	blockBlobURL := azblob.NewBlockBlobURL(*u, azblob.NewPipeline(credential, azblob.PipelineOptions{}))
	return &blockBlobURL, nil
}

func getContainerURL(storageEndpointSuffix, accountName, accountKey, containerName string) (*azblob.ContainerURL, error) {
	url, err := url.Parse(fmt.Sprintf("https://%s.%s/%s", accountName, storageEndpointSuffix, containerName))
	if err != nil {
		return nil, err
	}
	credential, err := azblob.NewSharedKeyCredential(accountName, accountKey)
	if err != nil {
		return nil, err
	}
	p := azblob.NewPipeline(credential, azblob.PipelineOptions{})
	containerURL := azblob.NewContainerURL(*url, p)

	return &containerURL, nil
}

// See Librarian.
func (l *AzureBlobLibrarian) Create(id SnapshotID, snap *os.File) error {
	// Do a creation call to the bucket for SnapshotID

	ctx := context.Background()
	filename := makeConsulKey(l, id)

	blockBlobURL, err := getBlockBlobURL(l.environment.StorageEndpointSuffix, l.config.AccountName, l.config.AccountKey, l.config.ContainerName, *filename)

	if err != nil {
		return err
	}

	// TODO(rb): since we now have an io.ReadSeeker instead just an io.Reader
	// is there something simpler we can use here?

	// Perform UploadStreamToBlockBlob
	// These defaults are taken from Azure example code:
	// https://github.com/Azure/azure-storage-blob-go/blob/5152f14ace1c6db66bd9cb57840703a8358fa7bc/azblob/zt_examples_test.go#L1126
	// https://godoc.org/github.com/Azure/azure-storage-blob-go/azblob#UploadStreamToBlockBlob
	bufferSize := 2 * 1024 * 1024 // Configure the size of the rotating buffers that are used when uploading
	maxBuffers := 3               // Configure the number of rotating buffers that are used when uploading

	_, err = azblob.UploadStreamToBlockBlob(ctx, snap, *blockBlobURL,
		azblob.UploadStreamToBlockBlobOptions{BufferSize: bufferSize, MaxBuffers: maxBuffers})

	if err != nil {
		l.logger.Error("Attempt to upload to Azure Blob failed", "error", err)
		return err
	}

	l.logger.Info("Upload of file to Azure Blob Storage successful", "filename", *filename)

	return nil
}

// See Librarian.
func (l *AzureBlobLibrarian) Delete(id SnapshotID) error {
	ctx := context.Background()

	filename := makeConsulKey(l, id)

	blockBlobURL, err := getBlockBlobURL(l.environment.StorageEndpointSuffix, l.config.AccountName, l.config.AccountKey, l.config.ContainerName, *filename)

	if err != nil {
		return err
	}

	_, err = blockBlobURL.Delete(ctx, azblob.DeleteSnapshotsOptionInclude, azblob.BlobAccessConditions{})

	if err != nil {
		l.logger.Error("Attempt to delete Azure Blob failed", "error", err)
		return err
	}

	l.logger.Info("Deletion of older snapshot in Azure Blob storage successful", "filename", *filename)

	return nil
}

// See Librarian.
func (l *AzureBlobLibrarian) List() (SnapshotIDs, error) {
	// Do a fetch call to the bucket for SnapshotIDs

	var ids SnapshotIDs

	ctx := context.Background()

	containerURL, err := getContainerURL(l.environment.StorageEndpointSuffix, l.config.AccountName, l.config.AccountKey, l.config.ContainerName)

	if err != nil {
		return nil, err
	}

	matcher := keyRegExp(l.outputNamePrefix)

	for marker := (azblob.Marker{}); marker.NotDone(); {
		// Get a result segment starting with the blob indicated by the current Marker.
		listBlob, err := containerURL.ListBlobsFlatSegment(ctx, marker, azblob.ListBlobsSegmentOptions{})

		if err != nil {
			return nil, err
		}

		// ListBlobs returns the start of the next segment; you MUST use this to get
		// the next segment (after processing the current result segment).
		marker = listBlob.NextMarker

		// Process the blobs returned in this result segment (if the segment is empty, the loop body won't execute)
		for _, blobInfo := range listBlob.Segment.BlobItems {
			m := matcher.FindStringSubmatch(blobInfo.Name)
			if m == nil {
				continue
			}

			var id int64
			if id, err = strconv.ParseInt(m[1], 10, 64); err != nil {
				return nil, fmt.Errorf("bad snapshot key %q: %v", blobInfo.Name, err)
			}
			ids = append(ids, SnapshotID(id))
		}
	}

	return ids, nil
}

// See Librarian.
func (l *AzureBlobLibrarian) RotationEnabled() bool {
	return true
}

func (l *AzureBlobLibrarian) Info() string {
	return fmt.Sprintf("Azure Blob Storage -> Environment: %q Account Name: %q Container Name: %q",
		l.config.Environment, l.config.AccountName, l.config.ContainerName)
}
