package snapshotagent

import (
	"fmt"
	"os"
	"path"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/defaults"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

// S3LibrarianConfig configures an S3 librarian.
type S3LibrarianConfig struct {
	// AccessKeyID and SecretAccessKey are used to supply authentication
	// information for AWS. If these aren't supplied we will also use:
	// - AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment
	// variables
	// - A credentials file (~/.aws/credentials) or the file at
	// the path specified by the AWS_SHARED_CREDENTIALS_FILE
	// environment variable
	// - ECS task role metadata (container-specific)
	// - EC2 instance role metadata
	AccessKeyID     string `hcl:"access_key_id,optional"`
	SecretAccessKey string `hcl:"secret_access_key,optional"`

	// Region is the AWS region where the bucket resides.
	Region string `hcl:"s3_region,optional"`

	// Endpoint is the AWS endpoint to connect to.
	Endpoint string `hcl:"s3_endpoint,optional"`

	// Bucket is the S3 bucket to use for snapshots.
	Bucket string `hcl:"s3_bucket,optional"`

	// KeyPrefix is the path in the bucket to use for snapshots.
	KeyPrefix string `hcl:"s3_key_prefix,optional"`

	// StaticSnapshotName is the name of the snapshot. Set this instead of KeyPrefix
	// if you don't want the snapshot agent to version snapshots by timestamp when saving them to s3
	// Note that this will overwrite the filename in s3, to get access to older versions
	// enable bucket versioning http://docs.aws.amazon.com/AmazonS3/latest/dev/UsingBucket.html#bucket-config-options-intro
	StaticSnapshotName string `hcl:"s3_static_snapshot_name,optional"`

	// When ServerSideEncryption is set, snapshots are saved to S3 with AES-256 encryption
	ServerSideEncryption bool `hcl:"s3_server_side_encryption,optional"`

	// EnableKMS turns on using Amazon KMS for server side encryption instead of the default
	EnableKMS bool `hcl:"s3_enable_kms,optional"`

	// When KMSKey is specified, it used as the encryption key. If this is empty and EnableKMS is set, the default KMS key is used.
	// http://docs.aws.amazon.com/AmazonS3/latest/dev/UsingKMSEncryption.html has more information on KMS encryption for S3.
	KMSKey string `hcl:"s3_kms_key,optional"`
}

// S3Librarian stores snapshots in Amazon's S3.
type S3Librarian struct {
	// config is the configuration of the librarian as given at create time.
	config *S3LibrarianConfig

	// proxy connects us to AWS.
	proxy liteS3

	// Prefix of objects
	snapPrefix string
}

// liteS3 is a tiny version of the S3 interface with just the bits we use here,
// and it abstracts over the fact that we have an uploader and a regular S3 client.
// This allows for much easier mocking for unit tests.
type liteS3 interface {
	Upload(*s3manager.UploadInput) (*s3manager.UploadOutput, error)
	DeleteObject(*s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error)
	ListObjectsPaginated(*s3.ListObjectsInput) ([]*s3.ListObjectsOutput, error)
}

// realS3 adds the s3Lite interface to the real AWS client and uploader.
type realS3 struct {
	// client is an S3 client used to list and delete snapshots.
	client s3iface.S3API

	// uploader is an upload manager used to create new snapshots.
	uploader *s3manager.Uploader
}

// See s3manager.Uploader.
func (s *realS3) Upload(input *s3manager.UploadInput) (*s3manager.UploadOutput, error) {
	return s.uploader.Upload(input)
}

// See s3iface.S3API.
func (s *realS3) DeleteObject(input *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
	return s.client.DeleteObject(input)
}

// See s3iface.S3API.
func (s *realS3) ListObjectsPaginated(input *s3.ListObjectsInput) ([]*s3.ListObjectsOutput, error) {
	var paginatedResponses []*s3.ListObjectsOutput
	err := s.client.ListObjectsPages(input,
		func(page *s3.ListObjectsOutput, lastPage bool) bool {
			paginatedResponses = append(paginatedResponses, page)
			return !lastPage
		})
	return paginatedResponses, err
}

// NewS3Librarian returns an S3 librarian with the given configuration. See
// S3LibrarianConfig for details on where the AWS authentication information can
// come from.
func NewS3Librarian(config *S3LibrarianConfig, snapPrefix string) (*S3Librarian, error) {
	awsConfig := generateAWSConfig(config)

	s, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, err
	}
	client := s3.New(s)
	uploader := s3manager.NewUploaderWithClient(client)
	proxy := &realS3{
		client:   client,
		uploader: uploader,
	}

	return &S3Librarian{
		config:     config,
		proxy:      proxy,
		snapPrefix: snapPrefix,
	}, nil
}

// generateAWSConfig returns an aws.Config for the given S3LibrarianConfig
func generateAWSConfig(config *S3LibrarianConfig) *aws.Config {
	return &aws.Config{
		Region:   &config.Region,
		Endpoint: &config.Endpoint,
		Credentials: credentials.NewChainCredentials(
			[]credentials.Provider{
				&credentials.StaticProvider{
					Value: credentials.Value{
						AccessKeyID:     config.AccessKeyID,
						SecretAccessKey: config.SecretAccessKey,
					},
				},
				&credentials.EnvProvider{},
				&credentials.SharedCredentialsProvider{},
				defaults.RemoteCredProvider(*(defaults.Config()), defaults.Handlers()),
			}),
	}
}

// makeKey returns a key name for the given snapshot ID.
func (l *S3Librarian) makeKey(id SnapshotID) *string {
	base := fmt.Sprintf("%v-%d.snap", l.snapPrefix, id)
	key := path.Join(l.config.KeyPrefix, base)
	return &key
}

var encodingType = "AES256"

// See Librarian.
func (l *S3Librarian) Create(id SnapshotID, snap *os.File) error {
	var encryption *string
	var kmsKey *string

	if l.config.EnableKMS {
		kms := "aws:kms"
		encryption = &kms
		if l.config.KMSKey != "" {
			kmsKey = &l.config.KMSKey
		}
	} else if l.config.ServerSideEncryption {
		aes256 := "AES256"
		encryption = &aes256
	}
	keyToSave := getS3Key(l, id)
	req := &s3manager.UploadInput{
		Bucket:               &l.config.Bucket,
		Key:                  keyToSave,
		Body:                 snap,
		ServerSideEncryption: encryption,
	}

	// TODO(rb): since we now have an io.ReadSeeker instead just an io.Reader
	// we can use (*s3.PutObjectInput).Body instead of
	// (*s3manager.UploadInput).Body which can retry uploads.

	if kmsKey != nil {
		req.SSEKMSKeyId = kmsKey
	}

	if _, err := l.proxy.Upload(req); err != nil {
		return err
	}

	return nil
}
func getS3Key(l *S3Librarian, id SnapshotID) *string {
	if l.config.StaticSnapshotName != "" {
		return &l.config.StaticSnapshotName
	} else {
		return l.makeKey(id)
	}
}

// See Librarian.
func (l *S3Librarian) Delete(id SnapshotID) error {
	req := &s3.DeleteObjectInput{
		Bucket: &l.config.Bucket,
		Key:    getS3Key(l, id),
	}
	if _, err := l.proxy.DeleteObject(req); err != nil {
		return err
	}

	return nil
}

// See Librarian.
func (l *S3Librarian) List() (SnapshotIDs, error) {
	if l.config.StaticSnapshotName != "" {
		//if static snapshots are enabled, we don't versionize them
		panic("List method not supported when static snapshots are enabled")
	}
	req := &s3.ListObjectsInput{
		Bucket: &l.config.Bucket,
		Prefix: &l.config.KeyPrefix,
	}
	paginatedResponse, err := l.proxy.ListObjectsPaginated(req)
	if err != nil {
		return nil, err
	}

	matcher := keyRegExp(l.snapPrefix)

	var ids SnapshotIDs
	for _, resp := range paginatedResponse {
		for _, obj := range resp.Contents {
			m := matcher.FindStringSubmatch(path.Base(*obj.Key))
			if m == nil {
				continue
			}

			var id int64
			if _, err := fmt.Sscanf(m[1], "%d", &id); err != nil {
				return nil, fmt.Errorf("bad snapshot key %q: %v", *obj.Key, err)
			}
			ids = append(ids, SnapshotID(id))
		}
	}

	return ids, nil
}

// See Librarian.
func (l *S3Librarian) RotationEnabled() bool {
	return l.config.StaticSnapshotName == ""
}

func (l *S3Librarian) Info() string {
	return fmt.Sprintf("Amazon S3 -> Region: %q Bucket: %q Key Prefix: %q Snapshot Name: %q",
		l.config.Region, l.config.Bucket, l.config.KeyPrefix, l.config.StaticSnapshotName)

}
