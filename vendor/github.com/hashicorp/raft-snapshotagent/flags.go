package snapshotagent

import (
	"flag"
	"strings"

	"github.com/hashicorp/raft-snapshotagent/internal/flags"
)

type cmdFlags struct {
	LogLevel          flags.StringValue
	LogLogJSON        flags.BoolValue
	LogEnableSyslog   flags.BoolValue
	LogSyslogFacility flags.StringValue

	ConsulEnable flags.BoolValue

	// Snapshot agent flags
	SnapshotInterval         flags.StringValue
	SnapshotRetain           flags.IntValue
	SnapshotService          flags.StringValue
	SnapshotDeregisterAfter  flags.StringValue
	SnapshotLockKey          flags.StringValue
	SnapshotMaxFailures      flags.IntValue
	SnapshotLocalScratchPath flags.StringValue
	SnapshotNamePrefix       flags.StringValue

	// Local librarian flags
	LocalPath flags.StringValue

	// S3 librarian flags
	S3AccessKeyID          flags.StringValue
	S3SecretAccessKey      flags.StringValue
	S3Region               flags.StringValue
	S3Endpoint             flags.StringValue
	S3Bucket               flags.StringValue
	S3KeyPrefix            flags.StringValue
	S3ServerSideEncryption flags.BoolValue
	S3StaticSnapshotName   flags.StringValue
	S3EnableKMS            flags.BoolValue
	S3KMSKey               flags.StringValue

	// Azure Blob librarian flags
	AzureBlobAccountName   flags.StringValue
	AzureBlobAccountKey    flags.StringValue
	AzureBlobContainerName flags.StringValue
	AzureBlobEnvironment   flags.StringValue

	// Google Cloud Storage librarian flags
	GCSBucket flags.StringValue
}

func (c *Config) InstallFlags(flags *flag.FlagSet) {
	flags.Var(&c.cmdConfig.ConsulEnable, "consul-enable", "")

	// Logging flags.
	flags.Var(&c.cmdConfig.LogLevel, "log-level", "")
	flags.Var(&c.cmdConfig.LogLogJSON, "log-json", "")
	flags.Var(&c.cmdConfig.LogEnableSyslog, "syslog", "")
	flags.Var(&c.cmdConfig.LogSyslogFacility, "syslog-facility", "")

	// Snapshot agent flags.
	flags.Var(&c.cmdConfig.SnapshotInterval, "interval", "")
	flags.Var(&c.cmdConfig.SnapshotRetain, "retain", "")
	flags.Var(&c.cmdConfig.SnapshotService, "service", "")
	flags.Var(&c.cmdConfig.SnapshotDeregisterAfter, "deregister-after", "")
	flags.Var(&c.cmdConfig.SnapshotLockKey, "lock-key", "")
	flags.Var(&c.cmdConfig.SnapshotMaxFailures, "max-failures", "")
	flags.Var(&c.cmdConfig.SnapshotLocalScratchPath, "local-scratch-path", "")
	flags.Var(&c.cmdConfig.SnapshotNamePrefix, "name-prefix", "")

	// Local librarian flags.
	flags.Var(&c.cmdConfig.LocalPath, "local-path", "")

	// S3 librarian flags.
	flags.Var(&c.cmdConfig.S3AccessKeyID, "aws-access-key-id", "")
	flags.Var(&c.cmdConfig.S3SecretAccessKey, "aws-secret-access-key", "")
	flags.Var(&c.cmdConfig.S3Region, "aws-s3-region", "")
	flags.Var(&c.cmdConfig.S3Endpoint, "aws-s3-endpoint", "")
	flags.Var(&c.cmdConfig.S3Bucket, "aws-s3-bucket", "")
	flags.Var(&c.cmdConfig.S3KeyPrefix, "aws-s3-key-prefix", "")
	flags.Var(&c.cmdConfig.S3ServerSideEncryption, "aws-s3-server-side-encryption", "")
	flags.Var(&c.cmdConfig.S3StaticSnapshotName, "aws-s3-static-snapshot-name", "")
	flags.Var(&c.cmdConfig.S3EnableKMS, "aws-s3-enable-kms", "")
	flags.Var(&c.cmdConfig.S3KMSKey, "aws-s3-kms-key", "")

	// Azure Blob librarian flags
	flags.Var(&c.cmdConfig.AzureBlobAccountName, "azure-blob-account-name", "")
	flags.Var(&c.cmdConfig.AzureBlobAccountKey, "azure-blob-account-key", "")
	flags.Var(&c.cmdConfig.AzureBlobContainerName, "azure-blob-container-name", "")
	flags.Var(&c.cmdConfig.AzureBlobEnvironment, "azure-blob-environment", "Environment to use. Defaults to AZUREPUBLICCLOUD. Other valid environments are AZURECHINACLOUD, AZUREGERMANCLOUD and AZUREUSGOVERNMENTCLOUD.")

	// Google Cloud Storage librarian flags
	flags.Var(&c.cmdConfig.GCSBucket, "google-bucket", "")
	// deprecated, kept for compatibility
	flags.Var(&c.cmdConfig.GCSBucket, "gcs-bucket", "")
}

type isSetable interface {
	IsSet() bool
}

func (c *Config) populateWithFlags() {
	var consulEnable bool
	c.cmdConfig.ConsulEnable.Merge(&consulEnable)
	if consulEnable {
		if c.Consul == nil {
			c.Consul = &ConsulConfig{}
		}
		c.Consul.Enabled = &consulEnable

	}

	// Logging
	c.cmdConfig.LogLevel.Merge(&c.Log.Level)
	c.cmdConfig.LogLogJSON.Merge(&c.Log.LogJSON)
	c.cmdConfig.LogEnableSyslog.Merge(&c.Log.EnableSyslog)
	c.cmdConfig.LogSyslogFacility.Merge(&c.Log.SyslogFacility)

	// Snapshot agent flags.
	c.cmdConfig.SnapshotInterval.Merge(&c.Agent.IntervalStr)
	c.cmdConfig.SnapshotRetain.Merge(c.Agent.Retain)
	c.cmdConfig.SnapshotService.Merge(&c.Agent.Service)
	c.cmdConfig.SnapshotDeregisterAfter.Merge(&c.Agent.DeregisterAfterStr)
	c.cmdConfig.SnapshotLockKey.Merge(&c.Agent.LockKey)
	c.cmdConfig.SnapshotMaxFailures.Merge(c.Agent.MaxFailures)
	c.cmdConfig.SnapshotLocalScratchPath.Merge(&c.Agent.LocalScratchPath)
	c.cmdConfig.SnapshotNamePrefix.Merge(&c.Agent.NamePrefix)

	// Local librarian flags.
	anySet := func(fs ...isSetable) bool {
		for _, f := range fs {
			if f.IsSet() {
				return true
			}
		}
		return false
	}

	if anySet(&c.cmdConfig.LocalPath) {
		if c.Local == nil {
			c.Local = &LocalLibrarianConfig{}
		}

		c.cmdConfig.LocalPath.Merge(&c.Local.Path)
	}

	// S3 librarian flags.
	if anySet(&c.cmdConfig.S3AccessKeyID,
		&c.cmdConfig.S3SecretAccessKey,
		&c.cmdConfig.S3Region,
		&c.cmdConfig.S3Endpoint,
		&c.cmdConfig.S3Bucket,
		&c.cmdConfig.S3KeyPrefix,
		&c.cmdConfig.S3ServerSideEncryption,
		&c.cmdConfig.S3StaticSnapshotName,
		&c.cmdConfig.S3EnableKMS,
		&c.cmdConfig.S3KMSKey) {

		if c.S3 == nil {
			c.S3 = &S3LibrarianConfig{}
		}

		c.cmdConfig.S3AccessKeyID.Merge(&c.S3.AccessKeyID)
		c.cmdConfig.S3SecretAccessKey.Merge(&c.S3.SecretAccessKey)
		c.cmdConfig.S3Region.Merge(&c.S3.Region)
		c.cmdConfig.S3Endpoint.Merge(&c.S3.Endpoint)
		c.cmdConfig.S3Bucket.Merge(&c.S3.Bucket)
		c.cmdConfig.S3KeyPrefix.Merge(&c.S3.KeyPrefix)
		c.cmdConfig.S3ServerSideEncryption.Merge(&c.S3.ServerSideEncryption)
		c.cmdConfig.S3StaticSnapshotName.Merge(&c.S3.StaticSnapshotName)
		c.cmdConfig.S3EnableKMS.Merge(&c.S3.EnableKMS)
		c.cmdConfig.S3KMSKey.Merge(&c.S3.KMSKey)
	}

	// Azure Blob librarian flags
	if anySet(&c.cmdConfig.AzureBlobAccountName,
		&c.cmdConfig.AzureBlobAccountKey,
		&c.cmdConfig.AzureBlobContainerName,
		&c.cmdConfig.AzureBlobEnvironment) {

		if c.AzureBlob == nil {
			c.AzureBlob = &AzureBlobLibrarianConfig{}
		}

		c.cmdConfig.AzureBlobAccountName.Merge(&c.AzureBlob.AccountName)
		c.cmdConfig.AzureBlobAccountKey.Merge(&c.AzureBlob.AccountKey)
		c.cmdConfig.AzureBlobContainerName.Merge(&c.AzureBlob.ContainerName)
		c.cmdConfig.AzureBlobEnvironment.Merge(&c.AzureBlob.Environment)
	}

	// Google Cloud Storage librarian flags
	if anySet(&c.cmdConfig.GCSBucket) {
		if c.GCS == nil {
			c.GCS = &GCSLibrarianConfig{}
		}
		c.cmdConfig.GCSBucket.Merge(&c.GCS.Bucket)
	}
}

func FlagsHelp(product string) string {
	text := `Snapshot Options:
  -interval               Interval at which to perform snapshots as a time with
                          a unit suffix, which can be "s", "m", "h" for seconds,
                          minutes, or hours. If 0 is provided, the agent will
                          take a single snapshot and then exit, which is useful
                          for running snapshots via batch jobs. Defaults to "1h".
  -lock-key               A prefix in Consul's key-value store used to coordinate
                          between different instances of the snapshot agent in
                          order to only have one active instance at a time. For
                          highly available operation of the snapshot agent,
                          simply run multiple instances. All instances must be
                          configured with the same lock key in order to properly
                          coordinate. Defaults to "__PRODUCT__-snapshot/lock".
  -max-failures           Number of snapshot failures after which the snapshot
                          agent will give up leadership. In a highly available
                          operation with multiple snapshot agents available, this
                          gives another agent a chance to take over if an agent
                          is experiencing issues, such as running out of disk
                          space for snapshots. Defaults to 3.
  -retain                 Number of snapshots to retain. After each snapshot is
                          taken, the oldest snapshots will start to be deleted
                          in order to retain at most this many snapshots. If
                          this is set to 0, the agent will not perform this and
                          snapshots will accumulate forever. Defaults to 30.
Agent Options:
  -deregister-after       An interval, after which if the agent is unhealthy it
                          will be automatically deregistered from Consul service.
                          discovery. This is a time with a unit suffix, which
                          can be "s", "m", "h" for seconds, minutes, or hours.
                          If 0 is provided, this will be disabled. Defaults to "72h".
  -log-level              Controls verbosity of snapshot agent logs. Valid options
                          are "TRACE", "DEBUG", "INFO", "WARN", "ERR". Defaults
                          to "INFO".
  -log-json               Output logs in JSON format. Defaults to false.
  -service                The service name to used when registering the agent
                          with Consul. Registering helps monitor running agents
                          and the leader registers an additional health check to
                          monitor that snapshots are taking place. Defaults to
                          "__PRODUCT__-snapshot".
  -syslog                 This enables forwarding logs to syslog. Defaults to
                          false.
  -syslog-facility        Sets the facility to use for forwarding logs to syslog.
                          Defaults to "LOCAL0".
Local Storage Options:
  -local-path             Location to store snapshots locally. The default
                          behavior of the snapshot agent is to store snapshots
                          locally in this directory. Defaults to "." to use the
                          current working directory. If an alternate storage
                          option is configured, then local storage will be
                          disabled and this option will be ignored.
S3 Storage Options:
Note that despite the AWS references, any S3-compatible endpoint can be specified with '-aws-s3-endpoint'.
  -aws-access-key-id      These arguments supply authentication information for
  -aws-secret-access-key  connecting to S3. These may also be supplied using
                          the following alternative methods:
                          - AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment
                            variables
                          - A credentials file (~/.aws/credentials or the file at
                            the path specified by the AWS_SHARED_CREDENTIALS_FILE
                            environment variable)
                          - ECS task role metadata (container-specific)
                          - EC2 instance role metadata
  -aws-s3-bucket          S3 bucket to use. Required for S3 storage, and setting
                          this disables local storage.
  -aws-s3-key-prefix      Prefix to use for snapshot files in S3. Defaults to
                          "__PRODUCT__-snapshot".
  -aws-s3-region          S3 region to use. Required for S3 storage.
  -aws-s3-endpoint        Optional S3 endpoint to use. Can also be specified using the
                          AWS_S3_ENDPOINT environment variable.
  -aws-s3-server-side-encryption  Enables server side encryption with AES-256,
                                  when storing snapshots to S3. Defaults to false.
  -aws-s3-static-snapshot-name    Static file name to use for snapshot files. If
                                  this is set, snapshots are always saved with the
                                  same name, and are not versioned or rotated.
  -aws-s3-enable-kms              Enables using Amazon KMS for encrypting snapshots
  -aws-s3-kms-key                 Optional KMS key to use, if this is not set the default KMS key will be used.
Azure Blob Storage Options: (Note: Non-Solaris platforms only)
  -azure-blob-account-name       These arguments supply authentication information
  -azure-blob-account_key               for connecting to Azure Blob storage.
  -azure-blob-container-name     Container to use. Required for Azure blob storage, and setting this
                                        disables local storage.
  -azure-blob-environment        Environment to use. Defaults to AZUREPUBLICCLOUD. Other valid environments are 
                                        AZURECHINACLOUD, AZUREGERMANCLOUD and AZUREUSGOVERNMENTCLOUD.
Google Storage Options:
  -google-bucket           The bucket google to use
`
	return strings.ReplaceAll(text, "__PRODUCT__", product)
}
