package snapshotagent

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"

	"cloud.google.com/go/storage"
	"github.com/hashicorp/go-hclog"
	"google.golang.org/api/iterator"
)

type GCSLibrarianConfig struct {
	Bucket string `hcl:"bucket,optional"`
}

type GCSLibrarian struct {
	// config is the configuration of the librarian as given at create time.
	config     *GCSLibrarianConfig
	logger     hclog.Logger
	snapPrefix string
}

// NewGCSLibrarian returns an GCS librarian with the given configuration. See
// GCSLibrarianConfig for details on where the GCS authentication information can
// come from.
func NewGCSLibrarian(config *GCSLibrarianConfig, snapPrefix string, logger hclog.Logger) (*GCSLibrarian, error) {
	return &GCSLibrarian{
		config:     config,
		logger:     logger,
		snapPrefix: snapPrefix,
	}, nil
}

func (l *GCSLibrarian) getKey(id SnapshotID) string {
	return fmt.Sprintf("%v-%d.snap", l.snapPrefix, id)
}

// See Librarian.
func (l *GCSLibrarian) Create(id SnapshotID, snap *os.File) error {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}

	// TODO(rb): since we now have an io.ReadSeeker instead just an io.Reader
	// is there something simpler we can use here?

	wc := client.Bucket(l.config.Bucket).Object(l.getKey(id)).NewWriter(ctx)
	if _, err = io.Copy(wc, snap); err != nil {
		// return err, since this is the reason Create failed.
		wc.Close()
		return err
	}
	return wc.Close()
}

// See Librarian.
func (l *GCSLibrarian) Delete(id SnapshotID) error {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}

	o := client.Bucket(l.config.Bucket).Object(l.getKey(id))
	return o.Delete(ctx)
}

// See Librarian.
func (l *GCSLibrarian) List() (SnapshotIDs, error) {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	matcher := keyRegExp(l.snapPrefix)

	var ids SnapshotIDs
	objects := client.Bucket(l.config.Bucket).Objects(ctx, nil)
	for {
		attrs, err := objects.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		m := matcher.FindStringSubmatch(attrs.Name)
		if m == nil {
			continue
		}

		var id int64
		if id, err = strconv.ParseInt(m[1], 10, 64); err != nil {
			return nil, fmt.Errorf("bad snapshot key %q: %v", attrs.Name, err)
		}
		ids = append(ids, SnapshotID(id))
	}
	return ids, nil
}

// See Librarian.
func (l *GCSLibrarian) RotationEnabled() bool {
	return true
}

func (l *GCSLibrarian) Info() string {
	return fmt.Sprintf("Google Cloud Storage -> Bucket: %q", l.config.Bucket)
}
